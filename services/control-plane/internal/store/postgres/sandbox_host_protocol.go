package postgres

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/ids"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/abskrj/velane/services/control-plane/internal/objectstore"
	"github.com/abskrj/velane/services/control-plane/internal/sandboxcontrol/domain"
	"github.com/abskrj/velane/services/control-plane/internal/sandboxcontrol/hostidentity"
	"github.com/jackc/pgx/v5"
)

const (
	hostLeaseDuration       = 45 * time.Second
	operationRetryBackoff   = 10 * time.Second
	commandDeliveryBatchMax = 32
)

type SandboxHostIdentity struct {
	PoolID      string
	HostID      string
	Incarnation int64
}

type SandboxHostRegistration struct {
	PoolID, BootID, HostCompatibilityKey string
	VMRestoreCapabilities                map[string]bool
	TotalVCPU, TotalMemoryMB             int
	TotalDiskBytes, TotalStagingBytes    int64
	AgentVersion, ProtocolVersion        string
}

type SandboxHostCommand struct {
	ID, Kind, AllocationID string
	HostIncarnation        int64
	FenceEpoch, Sequence   int64
	Payload                json.RawMessage
	LeaseToken             string          `json:"lease_token"`
	WatchdogGrant          json.RawMessage `json:"watchdog_grant"`
}

type WatchdogGrant struct {
	SandboxID          string `json:"sandbox_id"`
	AllocationID       string `json:"allocation_id"`
	HostID             string `json:"host_id"`
	HostIncarnation    int64  `json:"host_incarnation"`
	FenceEpoch         int64  `json:"fence_epoch"`
	IssuedAtUnixMilli  int64  `json:"issued_at_unix_milli"`
	ExpiresAtUnixMilli int64  `json:"expires_at_unix_milli"`
	Signature          []byte `json:"signature"`
}

func (g WatchdogGrant) signedPayload() []byte {
	b, _ := json.Marshal(struct {
		SandboxID       string `json:"sandbox_id"`
		AllocationID    string `json:"allocation_id"`
		HostID          string `json:"host_id"`
		HostIncarnation int64  `json:"host_incarnation"`
		FenceEpoch      int64  `json:"fence_epoch"`
		IssuedAt        int64  `json:"issued_at_unix_milli"`
		ExpiresAt       int64  `json:"expires_at_unix_milli"`
	}{g.SandboxID, g.AllocationID, g.HostID, g.HostIncarnation, g.FenceEpoch, g.IssuedAtUnixMilli, g.ExpiresAtUnixMilli})
	return b
}

func randomLeaseToken() (string, string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	return token, fmt.Sprintf("%x", sha256.Sum256([]byte(token))), nil
}

func (s *Store) watchdogGrant(sandboxID, allocationID, hostID string, incarnation, fence int64, expiry time.Time) (json.RawMessage, error) {
	if len(s.watchdogSigner) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("watchdog lease signer is unavailable")
	}
	now := time.Now().UTC()
	g := WatchdogGrant{SandboxID: sandboxID, AllocationID: allocationID, HostID: hostID, HostIncarnation: incarnation, FenceEpoch: fence, IssuedAtUnixMilli: now.UnixMilli(), ExpiresAtUnixMilli: expiry.UnixMilli()}
	g.Signature = ed25519.Sign(s.watchdogSigner, g.signedPayload())
	return json.Marshal(g)
}

type SandboxHostCommandResult struct {
	AllocationID, CommandID     string
	HostIncarnation, FenceEpoch int64
	Succeeded                   bool
	FailureCode, FailureMessage string
	Result                      json.RawMessage
}

type AllocationLeaseRenewal struct {
	AllocationID, LeaseToken    string
	HostIncarnation, FenceEpoch int64
}

type SandboxHostEnrollment struct {
	HostID            string
	Incarnation       int64
	CertificateSerial string
}

// CreateSandboxHostEnrollment consumes a persisted one-use challenge and
// creates or reincarnates only the provider-attested instance in its pool.
// It must be called after the provider verifier has validated cloud evidence.
func (s *Store) CreateSandboxHostEnrollment(ctx context.Context, challengeID, nonceHash, csrHash, bootID, agentVersion, protocolVersion string, verified hostidentity.VerifiedHost, certificateSerial string, certificateExpiresAt time.Time) (SandboxHostEnrollment, error) {
	if challengeID == "" || nonceHash == "" || csrHash == "" || bootID == "" || verified.PoolID == "" || verified.ProviderInstanceID == "" || !certificateExpiresAt.After(time.Now()) {
		return SandboxHostEnrollment{}, fmt.Errorf("invalid host enrollment")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return SandboxHostEnrollment{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var poolProvider, poolLineage, poolKey, status string
	err = tx.QueryRow(ctx, `SELECT provider,lineage_id,host_compatibility_key,status FROM sandbox_host_pools WHERE id=$1 FOR UPDATE`, verified.PoolID).Scan(&poolProvider, &poolLineage, &poolKey, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		return SandboxHostEnrollment{}, ErrNotFound
	}
	if err != nil {
		return SandboxHostEnrollment{}, err
	}
	if poolProvider != "aws" || status != "active" || verified.LineageID != poolLineage || verified.HostCompatibilityKey != poolKey {
		return SandboxHostEnrollment{}, fmt.Errorf("provider attestation does not match active pool")
	}
	tag, err := tx.Exec(ctx, `UPDATE sandbox_host_enrollment_challenges SET consumed_at=now() WHERE id=$1 AND nonce_hash=$2 AND csr_sha256=$3 AND pool_id=$4 AND provider=$5 AND consumed_at IS NULL AND expires_at>now()`, challengeID, nonceHash, csrHash, verified.PoolID, poolProvider)
	if err != nil {
		return SandboxHostEnrollment{}, err
	}
	if tag.RowsAffected() != 1 {
		return SandboxHostEnrollment{}, fmt.Errorf("enrollment challenge is expired, invalid, or already used")
	}
	var hostID, oldBootID string
	var incarnation int64
	err = tx.QueryRow(ctx, `SELECT id,incarnation,boot_id FROM sandbox_hosts WHERE pool_id=$1 AND provider_instance_id=$2 FOR UPDATE`, verified.PoolID, verified.ProviderInstanceID).Scan(&hostID, &incarnation, &oldBootID)
	if errors.Is(err, pgx.ErrNoRows) {
		hostID, incarnation = ids.New(), 1
		_, err = tx.Exec(ctx, `INSERT INTO sandbox_hosts(id,pool_id,provider_instance_id,incarnation,state,lineage_id,host_compatibility_key,host_compatibility_descriptor,total_vcpu,total_memory_mb,total_disk_bytes,total_staging_bytes,boot_id,agent_version,protocol_version,certificate_serial,certificate_expires_at) VALUES($1,$2,$3,$4,'registering',$5,$6,'{}',0,0,0,0,$7,$8,$9,$10,$11)`, hostID, verified.PoolID, verified.ProviderInstanceID, incarnation, poolLineage, poolKey, bootID, agentVersion, protocolVersion, certificateSerial, certificateExpiresAt)
	} else if err == nil {
		if oldBootID != bootID {
			incarnation++
		}
		_, err = tx.Exec(ctx, `UPDATE sandbox_hosts SET incarnation=$2,state='registering',lineage_id=$3,host_compatibility_key=$4,boot_id=$5,agent_version=$6,protocol_version=$7,certificate_serial=$8,certificate_expires_at=$9,certificate_revoked_at=NULL,heartbeat_sequence=0,last_heartbeat_at=NULL,lease_expires_at=NULL,failure_code='',failure_message='',updated_at=now() WHERE id=$1`, hostID, incarnation, poolLineage, poolKey, bootID, agentVersion, protocolVersion, certificateSerial, certificateExpiresAt)
	}
	if err != nil {
		return SandboxHostEnrollment{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return SandboxHostEnrollment{}, err
	}
	return SandboxHostEnrollment{HostID: hostID, Incarnation: incarnation, CertificateSerial: certificateSerial}, nil
}

func (s *Store) RenewSandboxHostCertificate(ctx context.Context, identity SandboxHostIdentity, certificateSerial string, certificateExpiresAt time.Time) error {
	if identity.PoolID == "" || identity.HostID == "" || identity.Incarnation < 1 || certificateSerial == "" || !certificateExpiresAt.After(time.Now()) {
		return fmt.Errorf("invalid host certificate renewal")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE sandbox_hosts SET certificate_serial=$4,certificate_expires_at=$5,certificate_revoked_at=NULL,updated_at=now() WHERE id=$1 AND pool_id=$2 AND incarnation=$3 AND state IN('registering','ready','draining') AND certificate_revoked_at IS NULL`, identity.HostID, identity.PoolID, identity.Incarnation, certificateSerial, certificateExpiresAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("stale, revoked, or expired host identity")
	}
	return nil
}

func (s *Store) HostCertificateActive(ctx context.Context, identity SandboxHostIdentity, serial string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE sandbox_hosts SET updated_at=updated_at WHERE id=$1 AND pool_id=$2 AND incarnation=$3 AND certificate_serial=$4 AND certificate_revoked_at IS NULL AND certificate_expires_at>now()`, identity.HostID, identity.PoolID, identity.Incarnation, serial)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("stale, revoked, or expired host certificate")
	}
	return nil
}

func (s *Store) CreateSandboxHostEnrollmentChallenge(ctx context.Context, id, nonceHash, poolID, csrHash, provider string, expiresAt time.Time) error {
	if id == "" || nonceHash == "" || poolID == "" || csrHash == "" || provider == "" || !expiresAt.After(time.Now()) {
		return fmt.Errorf("invalid enrollment challenge")
	}
	tag, err := s.pool.Exec(ctx, `INSERT INTO sandbox_host_enrollment_challenges(id,nonce_hash,pool_id,csr_sha256,provider,expires_at) SELECT $1,$2,p.id,$4,$5,$6 FROM sandbox_host_pools p WHERE p.id=$3 AND p.provider=$5 AND p.status='active'`, id, nonceHash, poolID, csrHash, provider, expiresAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("active sandbox host pool is unavailable")
	}
	return nil
}

func (s *Store) RegisterSandboxHost(ctx context.Context, identity SandboxHostIdentity, request SandboxHostRegistration) (int64, error) {
	if identity.PoolID == "" || identity.HostID == "" || identity.Incarnation < 1 || request.PoolID != identity.PoolID || request.BootID == "" || request.HostCompatibilityKey == "" || request.AgentVersion == "" || request.ProtocolVersion == "" || request.TotalVCPU <= 0 || request.TotalMemoryMB <= 0 || request.TotalDiskBytes <= 0 || request.TotalStagingBytes <= 0 || !supportsRequiredCommands(request.VMRestoreCapabilities) {
		return 0, fmt.Errorf("invalid host registration")
	}
	capabilities, err := json.Marshal(request.VMRestoreCapabilities)
	if err != nil {
		return 0, err
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var incarnation int64
	var poolKey, poolStatus, bootID string
	err = tx.QueryRow(ctx, `SELECT h.incarnation,p.host_compatibility_key,p.status,h.boot_id
		FROM sandbox_hosts h JOIN sandbox_host_pools p ON p.id=h.pool_id
		WHERE h.id=$1 AND h.pool_id=$2 FOR UPDATE`, identity.HostID, identity.PoolID).Scan(&incarnation, &poolKey, &poolStatus, &bootID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if incarnation != identity.Incarnation || poolStatus != "active" || poolKey != request.HostCompatibilityKey {
		return 0, fmt.Errorf("host identity, pool, or compatibility key is not valid")
	}
	// A new boot must receive a newly issued certificate bound to its new
	// incarnation. Never increment it under an old certificate.
	if bootID != request.BootID {
		return 0, fmt.Errorf("host boot changed; certificate renewal is required")
	}
	_, err = tx.Exec(ctx, `UPDATE sandbox_hosts SET state='ready',vm_restore_capabilities=$2,total_vcpu=$3,total_memory_mb=$4,total_disk_bytes=$5,total_staging_bytes=$6,agent_version=$7,protocol_version=$8,last_heartbeat_at=now(),lease_expires_at=now()+$9::interval,failure_code='',failure_message='',updated_at=now() WHERE id=$1`, identity.HostID, capabilities, request.TotalVCPU, request.TotalMemoryMB, request.TotalDiskBytes, request.TotalStagingBytes, request.AgentVersion, request.ProtocolVersion, hostLeaseDuration.String())
	if err != nil {
		return 0, err
	}
	return incarnation, tx.Commit(ctx)
}

func supportsRequiredCommands(capabilities map[string]bool) bool {
	// Registration needs only safe teardown. Create, snapshot, and restore are
	// checked independently at mutation admission and placement time.
	return capabilities != nil && capabilities["Destroy"]
}

// SandboxHostCommandReadiness reports support from live, leased hosts. It is
// intentionally global: per-sandbox compatibility and capacity are still
// rechecked transactionally during placement and dispatch.
func (s *Store) SandboxHostCommandReadiness(ctx context.Context) (map[string]bool, error) {
	commands := make(map[string]bool)
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT capability.key
		FROM sandbox_hosts h
		CROSS JOIN LATERAL jsonb_each_text(h.vm_restore_capabilities) capability
		WHERE h.state='ready' AND h.lease_expires_at>now() AND capability.value='true'
		AND capability.key = ANY($1)`, []string{"CreateSandbox", "Destroy", "CreateFullSnapshot", "RestoreFullSnapshot"})
	if err != nil {
		return commands, err
	}
	defer rows.Close()
	for rows.Next() {
		var command string
		if err := rows.Scan(&command); err != nil {
			return nil, err
		}
		commands[command] = true
	}
	return commands, rows.Err()
}

func (s *Store) HeartbeatSandboxHost(ctx context.Context, identity SandboxHostIdentity, sequence int64, watchdogHealthy bool) error {
	if sequence < 0 {
		return fmt.Errorf("invalid heartbeat sequence")
	}
	state := "ready"
	failureCode := ""
	failureMessage := ""
	if !watchdogHealthy {
		state, failureCode, failureMessage = "draining", "WATCHDOG_UNHEALTHY", "sandbox watchdog is unhealthy"
	}
	tag, err := s.pool.Exec(ctx, `UPDATE sandbox_hosts SET state=$4,heartbeat_sequence=$3,last_heartbeat_at=now(),lease_expires_at=now()+$5::interval,failure_code=$6,failure_message=$7,updated_at=now()
		WHERE id=$1 AND pool_id=$2 AND incarnation=$8 AND state IN('ready','draining') AND heartbeat_sequence <= $3 AND total_vcpu>0 AND total_memory_mb>0 AND total_disk_bytes>0 AND total_staging_bytes>0 AND vm_restore_capabilities ?& ARRAY['CreateSandbox','Destroy']`, identity.HostID, identity.PoolID, sequence, state, hostLeaseDuration.String(), failureCode, failureMessage, identity.Incarnation)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("stale host identity or heartbeat")
	}
	return nil
}

// RenewSandboxAllocationLease compares every fencing value and the opaque lease
// token. The server receipt time defines the new deadline; hosts cannot extend
// a disconnected allocation locally.
func (s *Store) RenewSandboxAllocationLease(ctx context.Context, identity SandboxHostIdentity, renewal AllocationLeaseRenewal) (time.Time, error) {
	if renewal.AllocationID == "" || renewal.LeaseToken == "" || renewal.HostIncarnation != identity.Incarnation || renewal.FenceEpoch < 1 {
		return time.Time{}, fmt.Errorf("invalid allocation lease renewal")
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(renewal.LeaseToken)))
	var expiry time.Time
	err := s.pool.QueryRow(ctx, `UPDATE sandbox_allocations a SET lease_expires_at=now()+interval '60 seconds',updated_at=now()
		FROM sandboxes s,sandbox_hosts h WHERE a.id=$1 AND a.host_id=$2 AND a.host_incarnation=$3 AND a.fence_epoch=$4 AND a.lease_token_hash=$5 AND a.state='active' AND s.id=a.sandbox_id AND s.tenant_id=a.tenant_id AND s.current_allocation_id=a.id AND h.id=a.host_id AND h.incarnation=a.host_incarnation AND h.lease_expires_at>now()
		RETURNING a.lease_expires_at`, renewal.AllocationID, identity.HostID, identity.Incarnation, renewal.FenceEpoch, hash).Scan(&expiry)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, fmt.Errorf("stale fence")
	}
	return expiry, err
}

// WatchdogLeaseGrant creates a freshly signed grant only after re-reading the
// live allocation fence. It is safe to call after a successful renewal and is
// also used when a command is delivered.
func (s *Store) WatchdogLeaseGrant(ctx context.Context, identity SandboxHostIdentity, allocationID string, fence int64, expiry time.Time) (json.RawMessage, error) {
	var sandboxID string
	var actualFence, incarnation int64
	err := s.pool.QueryRow(ctx, `SELECT a.sandbox_id,a.fence_epoch,a.host_incarnation FROM sandbox_allocations a JOIN sandboxes s ON s.current_allocation_id=a.id WHERE a.id=$1 AND a.host_id=$2 AND a.host_incarnation=$3 AND a.fence_epoch=$4 AND a.state='active' AND a.lease_expires_at>= $5`, allocationID, identity.HostID, identity.Incarnation, fence, expiry.Add(-time.Second)).Scan(&sandboxID, &actualFence, &incarnation)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("stale fence")
	}
	if err != nil {
		return nil, err
	}
	return s.watchdogGrant(sandboxID, allocationID, identity.HostID, incarnation, actualFence, expiry)
}

// PollSandboxHostCommands redelivers every unacknowledged command. The host
// journal makes these intentional at-least-once deliveries safe after a crash.
func (s *Store) PollSandboxHostCommands(ctx context.Context, identity SandboxHostIdentity, after int64) ([]SandboxHostCommand, error) {
	rows, err := s.pool.Query(ctx, `WITH selected AS (
		SELECT c.id FROM sandbox_commands c
		JOIN sandbox_allocations a ON a.id=c.allocation_id
		JOIN sandboxes s ON s.id=c.sandbox_id AND s.tenant_id=c.tenant_id
		JOIN sandbox_hosts h ON h.id=c.host_id
		WHERE c.host_id=$1 AND c.host_incarnation=$2 AND c.state IN('pending','delivered')
		AND (c.sequence>$3 OR c.state='delivered') AND a.state='active' AND a.lease_expires_at>now()
		AND a.host_incarnation=c.host_incarnation AND a.fence_epoch=c.fence_epoch AND s.current_allocation_id=a.id
		AND h.state='ready' AND h.incarnation=c.host_incarnation AND h.lease_expires_at>now()
		ORDER BY c.sequence FOR UPDATE OF c SKIP LOCKED LIMIT $4
	) UPDATE sandbox_commands c SET state='delivered',attempt=attempt+1,updated_at=now()
	FROM selected WHERE c.id=selected.id
	RETURNING c.id,c.kind,c.allocation_id,c.host_incarnation,c.fence_epoch,c.sequence,c.payload,
	(SELECT lease_token_envelope FROM sandbox_allocations a WHERE a.id=c.allocation_id),
	(SELECT lease_expires_at FROM sandbox_allocations a WHERE a.id=c.allocation_id),c.sandbox_id`, identity.HostID, identity.Incarnation, after, commandDeliveryBatchMax)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	commands := make([]SandboxHostCommand, 0)
	for rows.Next() {
		var command SandboxHostCommand
		var expiry time.Time
		var sandboxID string
		if err := rows.Scan(&command.ID, &command.Kind, &command.AllocationID, &command.HostIncarnation, &command.FenceEpoch, &command.Sequence, &command.Payload, &command.LeaseToken, &expiry, &sandboxID); err != nil {
			return nil, err
		}
		grant, err := s.watchdogGrant(sandboxID, command.AllocationID, identity.HostID, command.HostIncarnation, command.FenceEpoch, expiry)
		if err != nil {
			return nil, err
		}
		command.WatchdogGrant = grant
		commands = append(commands, command)
	}
	return commands, rows.Err()
}

func (s *Store) AckSandboxHostCommand(ctx context.Context, identity SandboxHostIdentity, commandID string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE sandbox_commands SET state='acknowledged',updated_at=now()
		WHERE id=$1 AND host_id=$2 AND host_incarnation=$3 AND state IN('pending','delivered','acknowledged')`, commandID, identity.HostID, identity.Incarnation)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CompleteSandboxHostCommand(ctx context.Context, identity SandboxHostIdentity, input SandboxHostCommandResult) error {
	if input.CommandID == "" || input.AllocationID == "" || input.HostIncarnation != identity.Incarnation {
		return fmt.Errorf("invalid command result")
	}
	result := input.Result
	if len(result) == 0 {
		result = json.RawMessage(`{}`)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var operationID, tenantID, sandboxID, kind, commandKind, state, allocationID string
	var hostIncarnation, fenceEpoch, generation int64
	err = tx.QueryRow(ctx, `SELECT c.operation_id,c.tenant_id,c.sandbox_id,c.allocation_id,c.host_incarnation,c.fence_epoch,c.state,o.kind,c.kind,o.requested_generation
		FROM sandbox_commands c JOIN sandbox_operations o ON o.id=c.operation_id
		WHERE c.id=$1 AND c.host_id=$2 AND c.host_incarnation=$3 FOR UPDATE`, input.CommandID, identity.HostID, identity.Incarnation).Scan(&operationID, &tenantID, &sandboxID, &allocationID, &hostIncarnation, &fenceEpoch, &state, &kind, &commandKind, &generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if allocationID != input.AllocationID || hostIncarnation != input.HostIncarnation || fenceEpoch != input.FenceEpoch {
		return fmt.Errorf("stale fence")
	}
	// Lock the current ownership records as well as the command. A command is
	// evidence of past ownership; only the current, unexpired allocation may
	// advance sandbox or operation state.
	var currentAllocationID, allocationState string
	var allocationFence, allocationIncarnation int64
	err = tx.QueryRow(ctx, `SELECT s.current_allocation_id,a.state,a.fence_epoch,a.host_incarnation
		FROM sandboxes s JOIN sandbox_allocations a ON a.id=s.current_allocation_id
		WHERE s.id=$1 AND s.tenant_id=$2 AND s.generation=$3 AND a.id=$4 AND a.host_id=$5 AND a.lease_expires_at>now() FOR UPDATE`, sandboxID, tenantID, generation, allocationID, identity.HostID).Scan(&currentAllocationID, &allocationState, &allocationFence, &allocationIncarnation)
	if errors.Is(err, pgx.ErrNoRows) || currentAllocationID != allocationID || allocationState != "active" || allocationFence != fenceEpoch || allocationIncarnation != identity.Incarnation {
		return fmt.Errorf("stale fence")
	}
	if err != nil {
		return err
	}
	terminalState := "succeeded"
	if !input.Succeeded {
		terminalState = "failed"
	}
	if state == terminalState {
		return tx.Commit(ctx) // idempotent result replay
	}
	if state == "succeeded" || state == "failed" || state == "superseded" {
		return fmt.Errorf("command is already terminal")
	}
	if commandKind == "CreateFullSnapshot" && input.Succeeded {
		// Snapshot upload/verification is an explicit second durable step. A
		// successful Firecracker call alone never completes the operation.
		if _, err = tx.Exec(ctx, `UPDATE sandbox_commands SET state='succeeded',result=$2,updated_at=now() WHERE id=$1 AND state IN('delivered','acknowledged')`, input.CommandID, result); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE sandbox_operations SET state='waiting',owner_id=NULL,lease_token_hash=NULL,lease_expires_at=NULL,next_attempt_at=now()+interval '1 hour',result=$2,updated_at=now() WHERE id=$1 AND state='dispatched'`, operationID, result)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if commandKind == "Destroy" && input.Succeeded {
		if err := releaseAllocationTx(ctx, tx, allocationID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE sandboxes SET current_allocation_id=NULL WHERE id=$1 AND tenant_id=$2 AND generation=$3 AND current_allocation_id=$4`, sandboxID, tenantID, generation, allocationID); err != nil {
			return err
		}
	}
	if commandKind == "Destroy" && kind == "restart" && input.Succeeded {
		tag, updateErr := tx.Exec(ctx, `UPDATE sandbox_operations SET state='queued',owner_id=NULL,lease_token_hash=NULL,lease_expires_at=NULL,next_attempt_at=now(),updated_at=now() WHERE id=$1 AND state='dispatched' AND requested_generation=$2`, operationID, generation)
		if updateErr != nil {
			return updateErr
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("stale fence")
		}
		tag, updateErr = tx.Exec(ctx, `UPDATE sandboxes SET observed_state='stopped',current_allocation_id=NULL,updated_at=now() WHERE id=$1 AND tenant_id=$2 AND generation=$3 AND current_allocation_id=$4`, sandboxID, tenantID, generation, allocationID)
		if updateErr != nil {
			return updateErr
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("stale fence")
		}
		return tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `UPDATE sandbox_commands SET state=$2,result=$3,failure_code=$4,failure_message=$5,updated_at=now() WHERE id=$1`, input.CommandID, terminalState, result, input.FailureCode, input.FailureMessage); err != nil {
		return err
	}
	if input.Succeeded && commandKind == "RestoreFullSnapshot" && kind == "restore" {
		var selectedSnapshotID string
		if err = tx.QueryRow(ctx, `SELECT snapshot_id FROM sandbox_operations WHERE id=$1 AND snapshot_id IS NOT NULL`, operationID).Scan(&selectedSnapshotID); err != nil {
			return err
		}
		tag, updateErr := tx.Exec(ctx, `UPDATE sandboxes SET latest_snapshot_id=$4,updated_at=now() WHERE id=$1 AND tenant_id=$2 AND generation=$3 AND current_allocation_id=$5`, sandboxID, tenantID, generation, selectedSnapshotID, allocationID)
		if updateErr != nil {
			return updateErr
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("stale fence")
		}
	}
	if !input.Succeeded {
		_, err = tx.Exec(ctx, `UPDATE sandbox_operations SET state='failed',failure_code=$2,failure_message=$3,result=$4,updated_at=now() WHERE id=$1 AND state='dispatched'`, operationID, input.FailureCode, input.FailureMessage, result)
		return finishSandboxCommandTx(ctx, tx, tenantID, sandboxID, generation, "failed", input.FailureCode, input.FailureMessage, err)
	}
	observed := successfulObservedState(kind)
	if observed == "deleted" {
		_, err = tx.Exec(ctx, `UPDATE sandboxes SET observed_state='deleting',observed_generation=$3,deleted_at=now(),updated_at=now() WHERE tenant_id=$1 AND id=$2 AND generation=$3`, tenantID, sandboxID, generation)
	} else {
		_, err = tx.Exec(ctx, `UPDATE sandboxes SET observed_state=$4,observed_generation=$3,ever_booted=ever_booted OR $4='running',failure_code='',failure_message='',updated_at=now() WHERE tenant_id=$1 AND id=$2 AND generation=$3`, tenantID, sandboxID, generation, observed)
	}
	if err != nil {
		return err
	}
	tag, updateErr := tx.Exec(ctx, `UPDATE sandbox_operations SET state='succeeded',result=$2,updated_at=now() WHERE id=$1 AND state='dispatched' AND requested_generation=$3`, operationID, result, generation)
	if updateErr != nil {
		return updateErr
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("stale fence")
	}
	return tx.Commit(ctx)
}

func finishSandboxCommandTx(ctx context.Context, tx pgx.Tx, tenantID, sandboxID string, generation int64, state, code, message string, prior error) error {
	if prior != nil {
		return prior
	}
	if state == "failed" {
		tag, err := tx.Exec(ctx, `UPDATE sandboxes SET observed_state='failed',failure_code=$4,failure_message=$5,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND generation=$3`, tenantID, sandboxID, generation, code, message)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("stale fence")
		}
	}
	return nil
}

type SnapshotUploadPlan struct {
	SnapshotID string                       `json:"snapshot_id"`
	State      string                       `json:"state"`
	Artifacts  []SnapshotUploadPlanArtifact `json:"artifacts"`
}
type SnapshotUploadPlanArtifact struct {
	Type    string                    `json:"type"`
	DriveID string                    `json:"drive_id,omitempty"`
	Chunks  []SnapshotUploadPlanChunk `json:"chunks"`
}
type SnapshotUploadPlanChunk struct {
	Index          int    `json:"index"`
	ObjectRef      string `json:"object_ref"`
	UploadID       string `json:"upload_id"`
	PartNumber     int    `json:"part_number"`
	URL            string `json:"url"`
	ChecksumSHA256 string `json:"checksum_sha256"`
}
type SnapshotCompletedUpload struct {
	ObjectRef      string `json:"object_ref"`
	UploadID       string `json:"upload_id"`
	PartNumber     int    `json:"part_number"`
	ETag           string `json:"etag"`
	ChecksumSHA256 string `json:"checksum_sha256"`
}

const snapshotUploadURLTTL = 10 * time.Minute

// SandboxSnapshotUploadPlan fences planning to the active allocation and pins
// every encrypted chunk before a presigned URL is minted. It never returns an
// empty or synthetic plan.
func (s *Store) SandboxSnapshotUploadPlan(ctx context.Context, identity SandboxHostIdentity, snapshotID, allocationID string, incarnation, fence int64, rawManifest json.RawMessage) (SnapshotUploadPlan, error) {
	if s.snapshotObjects == nil {
		return SnapshotUploadPlan{}, fmt.Errorf("snapshot multipart object storage is unavailable")
	}
	var manifest domain.SnapshotManifestV1
	if snapshotID == "" || allocationID == "" || incarnation != identity.Incarnation || fence < 1 || json.Unmarshal(rawManifest, &manifest) != nil {
		return SnapshotUploadPlan{}, fmt.Errorf("invalid snapshot upload plan request")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return SnapshotUploadPlan{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var sandboxID, lineage, hostKey, descriptor, state string
	var generation, allocationFence, allocationIncarnation int64
	err = tx.QueryRow(ctx, `SELECT sn.sandbox_id,sn.generation,sn.lineage_id,sn.source_host_compatibility_key,sn.vm_restore_descriptor_digest,sn.state,a.fence_epoch,a.host_incarnation
		FROM sandbox_snapshots sn JOIN sandbox_allocations a ON a.id=sn.allocation_id
		WHERE sn.id=$1 AND sn.allocation_id=$2 AND a.host_id=$3 AND a.host_incarnation=$4 AND a.state='active' AND a.lease_expires_at>now() FOR UPDATE`, snapshotID, allocationID, identity.HostID, identity.Incarnation).Scan(&sandboxID, &generation, &lineage, &hostKey, &descriptor, &state, &allocationFence, &allocationIncarnation)
	if errors.Is(err, pgx.ErrNoRows) {
		return SnapshotUploadPlan{}, fmt.Errorf("stale fence")
	}
	if err != nil {
		return SnapshotUploadPlan{}, err
	}
	if allocationFence != fence || allocationIncarnation != incarnation || manifest.SandboxID != sandboxID || int64(manifest.Generation) != generation || manifest.LineageID != lineage || manifest.SourceHostCompatibilityKey != hostKey || manifest.VMRestoreDescriptorDigest != descriptor {
		return SnapshotUploadPlan{}, fmt.Errorf("stale fence")
	}
	var drivesJSON []byte
	if err = tx.QueryRow(ctx, `SELECT p.mutable_disk_layout FROM sandbox_profile_versions p JOIN sandboxes s ON s.profile_version_id=p.id WHERE s.id=$1`, sandboxID).Scan(&drivesJSON); err != nil {
		return SnapshotUploadPlan{}, err
	}
	var drives []domain.DriveDescriptor
	if err = json.Unmarshal(drivesJSON, &drives); err != nil {
		return SnapshotUploadPlan{}, err
	}
	if err = validateSnapshotExpectations(manifest, drives); err != nil {
		return SnapshotUploadPlan{}, err
	}
	if state != "requested" && state != "uploading" {
		return SnapshotUploadPlan{}, fmt.Errorf("snapshot is not awaiting upload")
	}
	// Reissuing a plan only creates descriptors for the original immutable rows.
	var existing int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM sandbox_snapshot_expected_artifacts WHERE snapshot_id=$1`, snapshotID).Scan(&existing); err != nil {
		return SnapshotUploadPlan{}, err
	}
	if existing != 0 {
		return SnapshotUploadPlan{}, fmt.Errorf("snapshot upload plan has expired; abort and retry the snapshot")
	}
	plan := SnapshotUploadPlan{SnapshotID: snapshotID, State: "uploading", Artifacts: make([]SnapshotUploadPlanArtifact, 0, len(manifest.Artifacts))}
	for _, artifact := range manifest.Artifacts {
		artifactID := ids.New()
		if _, err = tx.Exec(ctx, `INSERT INTO sandbox_snapshot_expected_artifacts(id,snapshot_id,artifact_type,drive_id,logical_size,logical_sha256) VALUES($1,$2,$3,$4,$5,$6)`, artifactID, snapshotID, artifact.Type, artifact.DriveID, artifact.LogicalSize, artifact.SHA256); err != nil {
			return SnapshotUploadPlan{}, err
		}
		out := SnapshotUploadPlanArtifact{Type: artifact.Type, DriveID: artifact.DriveID, Chunks: make([]SnapshotUploadPlanChunk, 0, len(artifact.Chunks))}
		for _, chunk := range artifact.Chunks {
			checksum, err := hexDigestToBase64(chunk.CiphertextSHA256)
			if err != nil {
				return SnapshotUploadPlan{}, err
			}
			objectRef := fmt.Sprintf("snapshots/%s/%s/%s/%06d", snapshotID, artifact.Type, artifact.DriveID, chunk.Index)
			upload, err := s.snapshotObjects.BeginSnapshotMultipart(ctx, objectRef, []objectstore.SnapshotPartExpectation{{Number: 1, Size: chunk.CiphertextSize, ChecksumSHA256: checksum}}, snapshotUploadURLTTL)
			if err != nil {
				return SnapshotUploadPlan{}, err
			}
			chunkID, uploadID := ids.New(), ids.New()
			if _, err = tx.Exec(ctx, `INSERT INTO sandbox_snapshot_artifact_chunks(id,expected_artifact_id,chunk_index,plaintext_size,ciphertext_size,plaintext_sha256,ciphertext_sha256,nonce,object_ref,state) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'uploading')`, chunkID, artifactID, chunk.Index, chunk.PlaintextSize, chunk.CiphertextSize, chunk.PlaintextSHA256, chunk.CiphertextSHA256, chunk.Nonce, objectRef); err != nil {
				_ = s.snapshotObjects.AbortSnapshotMultipart(ctx, upload)
				return SnapshotUploadPlan{}, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO sandbox_snapshot_multipart_uploads(id,chunk_id,provider_upload_id,expires_at,state) VALUES($1,$2,$3,$4,'active')`, uploadID, chunkID, upload.UploadID, upload.Parts[0].ExpiresAt); err != nil {
				_ = s.snapshotObjects.AbortSnapshotMultipart(ctx, upload)
				return SnapshotUploadPlan{}, err
			}
			if _, err = tx.Exec(ctx, `INSERT INTO sandbox_snapshot_multipart_parts(upload_id,part_number,expected_size,expected_checksum_sha256) VALUES($1,1,$2,$3)`, uploadID, chunk.CiphertextSize, checksum); err != nil {
				_ = s.snapshotObjects.AbortSnapshotMultipart(ctx, upload)
				return SnapshotUploadPlan{}, err
			}
			out.Chunks = append(out.Chunks, SnapshotUploadPlanChunk{Index: chunk.Index, ObjectRef: objectRef, UploadID: upload.UploadID, PartNumber: 1, URL: upload.Parts[0].URL, ChecksumSHA256: checksum})
		}
		plan.Artifacts = append(plan.Artifacts, out)
	}
	// This is an immutable upload expectation, not a completed manifest. It is
	// retained only while uploading and becomes the source for the canonical
	// manifest after control-plane object verification.
	if _, err = tx.Exec(ctx, `UPDATE sandbox_snapshots SET state='uploading',manifest=$2 WHERE id=$1 AND state='requested'`, snapshotID, rawManifest); err != nil {
		return SnapshotUploadPlan{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return SnapshotUploadPlan{}, err
	}
	return plan, nil
}

func hexDigestToBase64(v string) (string, error) {
	b, err := hex.DecodeString(v)
	if err != nil || len(b) != sha256.Size {
		return "", fmt.Errorf("invalid SHA-256 checksum")
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func validateSnapshotExpectations(m domain.SnapshotManifestV1, configured []domain.DriveDescriptor) error {
	// Object identities are control-plane generated after this request; validate
	// every other canonical full-manifest field before pinning it.
	for ai := range m.Artifacts {
		for ci := range m.Artifacts[ai].Chunks {
			m.Artifacts[ai].Chunks[ci].ObjectRef = "planned"
			m.Artifacts[ai].Chunks[ci].ObjectVersion = "planned"
		}
	}
	m.Checksum = "" // this pre-plan manifest cannot yet have canonical refs.
	if m.SchemaVersion != "v1" || m.SnapshotMode != "full" || m.FirecrackerSnapshotType != "Full" || m.ManifestID == "" || m.SandboxID == "" || m.Generation < 1 || m.LineageID == "" || m.SourceHostCompatibilityKey == "" || m.VMRestoreDescriptorDigest == "" || m.SnapshotCompatibilityKey == "" || m.MachineTopologyDigest == "" || m.DeviceTopologyDigest == "" || m.GuestImageDigest == "" || m.WrappedDataKey == "" || m.EncryptionContextDigest == "" {
		return fmt.Errorf("invalid full snapshot expectations")
	}
	compatibility, err := domain.SnapshotCompatibilityDigest(domain.SnapshotCompatibilityDescriptor{SchemaVersion: m.SchemaVersion, LineageID: m.LineageID, SourceHostCompatibilityKey: m.SourceHostCompatibilityKey, VMRestoreDescriptorDigest: m.VMRestoreDescriptorDigest})
	if err != nil || compatibility != m.SnapshotCompatibilityKey {
		return fmt.Errorf("invalid snapshot compatibility key")
	}
	if len(m.Drives) != len(configured) {
		return fmt.Errorf("drive inventory differs")
	}
	for i := range configured {
		if m.Drives[i] != configured[i] {
			return fmt.Errorf("drive inventory differs")
		}
	}
	seen := map[string]bool{}
	for _, a := range m.Artifacts {
		key := a.Type + "/" + a.DriveID
		if seen[key] || a.LogicalSize <= 0 || !validSnapshotHex(a.SHA256, sha256.Size) || len(a.Chunks) == 0 {
			return fmt.Errorf("invalid snapshot artifact expectations")
		}
		seen[key] = true
		var totalPlaintext int64
		for i, c := range a.Chunks {
			if c.Index != i || c.PlaintextSize <= 0 || c.CiphertextSize <= 0 || !validSnapshotHex(c.PlaintextSHA256, sha256.Size) || !validSnapshotHex(c.CiphertextSHA256, sha256.Size) || !validSnapshotHex(c.Nonce, 12) || c.ObjectRef != "planned" || c.ObjectVersion != "planned" {
				return fmt.Errorf("invalid snapshot chunk expectations")
			}
			if c.CiphertextSize != c.PlaintextSize+16 {
				return fmt.Errorf("invalid snapshot chunk size")
			}
			totalPlaintext += c.PlaintextSize
		}
		if totalPlaintext != a.LogicalSize {
			return fmt.Errorf("snapshot artifact size differs from its chunks")
		}
	}
	if !seen["memory.full/"] || !seen["vmstate.full/"] {
		return fmt.Errorf("memory and VM state are required")
	}
	for _, d := range configured {
		if d.Mutable != seen["drive.full/"+d.ID] {
			return fmt.Errorf("full snapshot does not cover every mutable drive")
		}
	}
	return nil
}

func validSnapshotHex(value string, size int) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == size
}

func (s *Store) CompleteSandboxSnapshot(ctx context.Context, identity SandboxHostIdentity, snapshotID, allocationID string, incarnation, fence int64, rawManifest json.RawMessage, completed []SnapshotCompletedUpload) error {
	// The planned manifest was fenced and persisted before presigns were issued;
	// completion data is only transport metadata and must not replace it.
	_ = rawManifest
	if s.snapshotObjects == nil {
		return fmt.Errorf("snapshot multipart object storage is unavailable")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var tenantID, sandboxID, operationID, lineage, hostKey, descriptor, state, operationKind string
	var plannedManifest json.RawMessage
	var generation, allocationFence, allocationIncarnation int64
	err = tx.QueryRow(ctx, `SELECT sn.tenant_id,sn.sandbox_id,sn.operation_id,sn.generation,sn.lineage_id,sn.source_host_compatibility_key,sn.vm_restore_descriptor_digest,sn.state,a.fence_epoch,a.host_incarnation,o.kind
		FROM sandbox_snapshots sn JOIN sandbox_allocations a ON a.id=sn.allocation_id JOIN sandbox_operations o ON o.id=sn.operation_id
		WHERE sn.id=$1 AND sn.allocation_id=$2 AND a.host_id=$3 AND a.host_incarnation=$4 AND a.state='active' AND a.lease_expires_at>now() FOR UPDATE`, snapshotID, allocationID, identity.HostID, identity.Incarnation).Scan(&tenantID, &sandboxID, &operationID, &generation, &lineage, &hostKey, &descriptor, &state, &allocationFence, &allocationIncarnation, &operationKind)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("stale fence")
	}
	if err != nil {
		return err
	}
	if incarnation != identity.Incarnation || allocationIncarnation != incarnation || allocationFence != fence {
		return fmt.Errorf("stale fence")
	}
	if err = tx.QueryRow(ctx, `SELECT manifest FROM sandbox_snapshots WHERE id=$1 FOR UPDATE`, snapshotID).Scan(&plannedManifest); err != nil {
		return err
	}
	var manifest domain.SnapshotManifestV1
	if err = json.Unmarshal(plannedManifest, &manifest); err != nil {
		return fmt.Errorf("invalid planned snapshot manifest")
	}
	if manifest.SandboxID != sandboxID || int64(manifest.Generation) != generation || manifest.LineageID != lineage || manifest.SourceHostCompatibilityKey != hostKey || manifest.VMRestoreDescriptorDigest != descriptor {
		return fmt.Errorf("stale fence")
	}
	var drivesJSON []byte
	if err = tx.QueryRow(ctx, `SELECT mutable_disk_layout FROM sandbox_profile_versions p JOIN sandboxes s ON s.profile_version_id=p.id WHERE s.id=$1`, sandboxID).Scan(&drivesJSON); err != nil {
		return err
	}
	var configured []domain.DriveDescriptor
	if err = json.Unmarshal(drivesJSON, &configured); err != nil {
		return err
	}
	if err = validateSnapshotExpectations(manifest, configured); err != nil {
		return fmt.Errorf("invalid snapshot expectations: %w", err)
	}
	if len(completed) == 0 {
		return fmt.Errorf("completed multipart uploads are required")
	}
	completedByRef := make(map[string]SnapshotCompletedUpload, len(completed))
	for _, item := range completed {
		if item.ObjectRef == "" || item.UploadID == "" || item.PartNumber != 1 || item.ETag == "" || item.ChecksumSHA256 == "" {
			return fmt.Errorf("invalid completed multipart upload")
		}
		if _, duplicate := completedByRef[item.ObjectRef]; duplicate {
			return fmt.Errorf("duplicate completed multipart upload")
		}
		completedByRef[item.ObjectRef] = item
	}
	// Completion trusts neither host-supplied object references nor checksums:
	// complete each exact persisted multipart session, then HEAD its resulting
	// version before allowing the manifest to become durable recovery state.
	for _, artifact := range manifest.Artifacts {
		for _, chunk := range artifact.Chunks {
			provided, ok := completedByRef[chunk.ObjectRef]
			if !ok {
				return fmt.Errorf("snapshot chunk completion is missing")
			}
			var chunkID, providerUploadID, expectedChecksum string
			var expectedSize int64
			err = tx.QueryRow(ctx, `SELECT c.id,u.provider_upload_id,p.expected_checksum_sha256,p.expected_size
				FROM sandbox_snapshot_artifact_chunks c JOIN sandbox_snapshot_expected_artifacts a ON a.id=c.expected_artifact_id
				JOIN sandbox_snapshot_multipart_uploads u ON u.chunk_id=c.id JOIN sandbox_snapshot_multipart_parts p ON p.upload_id=u.id
				WHERE a.snapshot_id=$1 AND a.artifact_type=$2 AND a.drive_id=$3 AND c.object_ref=$4 AND c.chunk_index=$5 AND c.plaintext_size=$6 AND c.ciphertext_size=$7 AND c.plaintext_sha256=$8 AND c.ciphertext_sha256=$9 AND c.nonce=$10 AND u.state='active' AND u.expires_at>now() FOR UPDATE`, snapshotID, artifact.Type, artifact.DriveID, chunk.ObjectRef, chunk.Index, chunk.PlaintextSize, chunk.CiphertextSize, chunk.PlaintextSHA256, chunk.CiphertextSHA256, chunk.Nonce).Scan(&chunkID, &providerUploadID, &expectedChecksum, &expectedSize)
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("snapshot chunk was not pinned by its upload plan")
			}
			if err != nil {
				return err
			}
			if provided.UploadID != providerUploadID || provided.ChecksumSHA256 != expectedChecksum {
				return fmt.Errorf("multipart completion does not match its upload plan")
			}
			object, completeErr := s.snapshotObjects.CompleteSnapshotMultipart(ctx, objectstore.SnapshotMultipartUpload{ObjectRef: chunk.ObjectRef, UploadID: providerUploadID, Parts: []objectstore.SnapshotPartURL{{Number: 1, ChecksumSHA256: expectedChecksum}}}, []objectstore.SnapshotCompletedPart{{Number: 1, ETag: provided.ETag, ChecksumSHA256: provided.ChecksumSHA256}})
			if completeErr != nil {
				return completeErr
			}
			if object.ObjectRef != chunk.ObjectRef || object.Size != expectedSize || object.ChecksumSHA256 != expectedChecksum || object.Version == "" {
				return fmt.Errorf("object storage verification failed")
			}
			idx := artifactIndex(manifest.Artifacts, artifact.Type, artifact.DriveID)
			if idx < 0 {
				return fmt.Errorf("planned snapshot artifact is missing")
			}
			manifest.Artifacts[idx].Chunks[chunk.Index].ObjectRef = object.ObjectRef
			manifest.Artifacts[idx].Chunks[chunk.Index].ObjectVersion = object.Version
			if _, err = tx.Exec(ctx, `UPDATE sandbox_snapshot_artifact_chunks SET object_version=$2,object_etag=$3,object_checksum_sha256=$4,state='verified' WHERE id=$1`, chunkID, object.Version, object.ETag, object.ChecksumSHA256); err != nil {
				return err
			}
			if _, err = tx.Exec(ctx, `UPDATE sandbox_snapshot_multipart_uploads SET state='completed',updated_at=now() WHERE chunk_id=$1 AND state='active'`, chunkID); err != nil {
				return err
			}
		}
	}
	manifest.Checksum = ""
	checksum, err := manifest.CanonicalChecksum()
	if err != nil {
		return err
	}
	manifest.Checksum = checksum
	if err = domain.ValidateFullSnapshotManifest(manifest, configured); err != nil {
		return fmt.Errorf("canonical verified manifest is invalid: %w", err)
	}
	if state != "uploading" && state != "verifying" {
		return fmt.Errorf("snapshot is not uploading")
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `UPDATE sandbox_snapshots SET state='ready',manifest=$2,manifest_checksum=$3,wrapped_data_key=$4,encryption_context_digest=$5,total_bytes=(SELECT coalesce(sum(logical_size),0) FROM jsonb_to_recordset($2->'artifacts') AS a(logical_size bigint)),ready_at=now() WHERE id=$1 AND state IN('uploading','verifying')`, snapshotID, manifestBytes, manifest.Checksum, manifest.WrappedDataKey, manifest.EncryptionContextDigest)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("stale fence")
	}
	tag, err = tx.Exec(ctx, `UPDATE sandboxes SET latest_snapshot_id=$4,observed_state=CASE WHEN $5 IN('stop','restart') THEN 'stopping' ELSE 'running' END,updated_at=now() WHERE id=$1 AND tenant_id=$2 AND generation=$3 AND current_allocation_id=$6`, sandboxID, tenantID, generation, snapshotID, operationKind, allocationID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("stale fence")
	}
	if operationKind == "snapshot" {
		tag, err = tx.Exec(ctx, `UPDATE sandbox_operations SET state='succeeded',result=jsonb_build_object('snapshot_id',$2),updated_at=now() WHERE id=$1 AND state IN('waiting','dispatched') AND requested_generation=$3`, operationID, snapshotID, generation)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("stale fence")
		}
		return tx.Commit(ctx)
	}
	// Stop/restart continue with a distinct destroy step. The operation stays
	// waiting until that command result completes it.
	tag, err = tx.Exec(ctx, `UPDATE sandbox_operations SET state='queued',next_attempt_at=now(),updated_at=now() WHERE id=$1 AND state IN('waiting','dispatched')`, operationID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("stale fence")
	}
	return tx.Commit(ctx)
}

func artifactIndex(artifacts []domain.SnapshotArtifact, typ, driveID string) int {
	for i, artifact := range artifacts {
		if artifact.Type == typ && artifact.DriveID == driveID {
			return i
		}
	}
	return -1
}

func successfulObservedState(kind string) string {
	switch kind {
	case "stop":
		return "stopped"
	case "snapshot":
		return "running"
	case "delete":
		return "deleted"
	default:
		return "running"
	}
}

// DispatchClaimedSandboxOperation persists a command before marking the
// operation dispatched. Without a live fenced allocation it releases the claim
// into durable waiting/backoff state instead of stranding it.
func (s *Store) DispatchClaimedSandboxOperation(ctx context.Context, operation models.SandboxOperation, owner, leaseHash string) error {
	if operation.SandboxID == nil {
		return s.failUnsupportedRecipeBuild(ctx, operation, owner, leaseHash)
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var sandboxID, tenantID, observed, descriptor string
	var operationSnapshotID *string
	var generation int64
	err = tx.QueryRow(ctx, `SELECT s.id,s.tenant_id,s.generation,s.observed_state,s.vm_restore_descriptor_digest,o.snapshot_id FROM sandboxes s JOIN sandbox_operations o ON o.sandbox_id=s.id WHERE o.id=$1 AND o.state='claimed' AND o.owner_id=$2 AND o.lease_token_hash=$3 AND o.requested_generation=s.generation FOR UPDATE`, operation.ID, owner, leaseHash).Scan(&sandboxID, &tenantID, &generation, &observed, &descriptor, &operationSnapshotID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("stale fence")
	}
	if err != nil {
		return err
	}
	operation.SnapshotID = operationSnapshotID
	commandKind := commandKindForOperation(operation.Kind)
	if operation.Kind == "stop" && operation.SnapshotID != nil && observed == "stopping" {
		commandKind = "Destroy"
	}
	if operation.Kind == "restart" && operation.SnapshotID != nil {
		if observed == "stopping" {
			commandKind = "Destroy"
		}
		if observed == "stopped" {
			commandKind = "RestoreFullSnapshot"
		}
	}
	var allocationID, hostID string
	var incarnation, fence, sequence int64
	err = tx.QueryRow(ctx, `SELECT a.id,a.host_id,a.host_incarnation,a.fence_epoch,COALESCE((SELECT max(sequence) FROM sandbox_commands WHERE host_id=a.host_id AND host_incarnation=a.host_incarnation),0)+1
		FROM sandbox_allocations a JOIN sandbox_hosts h ON h.id=a.host_id
		WHERE a.sandbox_id=$1 AND a.tenant_id=$2 AND a.state IN('reserved','active') AND a.lease_expires_at>now() AND h.state='ready' AND h.incarnation=a.host_incarnation AND h.lease_expires_at>now()
		AND COALESCE((h.vm_restore_capabilities->>$3)::boolean,false)
		ORDER BY a.created_at DESC LIMIT 1 FOR UPDATE`, sandboxID, tenantID, commandKind).Scan(&allocationID, &hostID, &incarnation, &fence, &sequence)
	if errors.Is(err, pgx.ErrNoRows) {
		allocationID, hostID, incarnation, fence, err = placeSandboxTx(ctx, tx, tenantID, sandboxID, generation, descriptor, operation.Kind, commandKind, operation.SnapshotID)
		if errors.Is(err, pgx.ErrNoRows) {
			if _, err = tx.Exec(ctx, `UPDATE sandbox_operations SET state='waiting',owner_id=NULL,lease_token_hash=NULL,lease_expires_at=NULL,next_attempt_at=now()+$2::interval,updated_at=now() WHERE id=$1`, operation.ID, operationRetryBackoff.String()); err != nil {
				return err
			}
			if observed == "pending" || observed == "stopped" || observed == "recovering" {
				_, err = tx.Exec(ctx, `UPDATE sandboxes SET observed_state='awaiting_capacity',updated_at=now() WHERE id=$1 AND generation=$2`, sandboxID, generation)
			}
			if err != nil {
				return err
			}
			return tx.Commit(ctx)
		}
	}
	if err != nil {
		return err
	}
	snapshotID := operation.SnapshotID
	if (operation.Kind == "snapshot" || operation.Kind == "stop" || operation.Kind == "restart") && operation.SnapshotID == nil {
		snapshotID, err = createOperationSnapshotTx(ctx, tx, operation, tenantID, sandboxID, generation, allocationID, hostID, incarnation, fence, descriptor)
		if err != nil {
			return err
		}
	}
	var payload json.RawMessage
	if commandKind == "Destroy" {
		payload, err = json.Marshal(map[string]any{"sandbox_id": sandboxID, "operation_id": operation.ID, "generation": generation})
	} else {
		payload, err = s.buildLifecyclePayloadTx(ctx, tx, lifecyclePayloadInput{
			Command: commandKind, OperationKind: operation.Kind, OperationID: operation.ID, SandboxID: sandboxID, TenantID: tenantID,
			Generation: generation, AllocationID: allocationID, HostID: hostID, Incarnation: incarnation,
			Fence: fence, SnapshotID: snapshotID,
		})
	}
	if err != nil {
		return deferClaimedSandboxOperationTx(ctx, tx, operation.ID, owner, leaseHash, "LIFECYCLE_PAYLOAD_UNAVAILABLE", err.Error())
	}
	commandID := ids.New()
	_, err = tx.Exec(ctx, `INSERT INTO sandbox_commands(id,operation_id,tenant_id,sandbox_id,allocation_id,host_id,host_incarnation,fence_epoch,sequence,kind,payload,state) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'pending')`, commandID, operation.ID, tenantID, sandboxID, allocationID, hostID, incarnation, fence, sequence, commandKind, payload)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE sandbox_allocations SET state='active',lease_expires_at=now()+interval '60 seconds',updated_at=now() WHERE id=$1 AND state IN('reserved','active')`, allocationID); err != nil {
		return err
	}
	observedTarget := "provisioning"
	if operation.Kind == "start" || operation.Kind == "restore" || operation.Kind == "recover" || (operation.Kind == "restart" && commandKind == "RestoreFullSnapshot") {
		observedTarget = "restoring"
	}
	if commandKind == "CreateFullSnapshot" {
		observedTarget = "snapshotting"
	}
	if commandKind == "Destroy" {
		observedTarget = "stopping"
	}
	if _, err = tx.Exec(ctx, `UPDATE sandboxes SET observed_state=$3,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND generation=$4`, tenantID, sandboxID, observedTarget, generation); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE sandbox_operations SET state='dispatched',owner_id=NULL,lease_token_hash=NULL,lease_expires_at=NULL,attempt=attempt+1,updated_at=now() WHERE id=$1 AND state='claimed'`, operation.ID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) deferClaimedSandboxOperation(ctx context.Context, operationID, owner, leaseHash, code string) error {
	_, err := s.pool.Exec(ctx, `UPDATE sandbox_operations SET state='waiting',owner_id=NULL,lease_token_hash=NULL,lease_expires_at=NULL,next_attempt_at=now()+$4::interval,failure_code=$5,updated_at=now() WHERE id=$1 AND state='claimed' AND owner_id=$2 AND lease_token_hash=$3`, operationID, owner, leaseHash, operationRetryBackoff.String(), code)
	return err
}

func deferClaimedSandboxOperationTx(ctx context.Context, tx pgx.Tx, operationID, owner, leaseHash, code, message string) error {
	tag, err := tx.Exec(ctx, `UPDATE sandbox_operations SET state='waiting',owner_id=NULL,lease_token_hash=NULL,lease_expires_at=NULL,next_attempt_at=now()+$4::interval,failure_code=$5,failure_message=$6,updated_at=now() WHERE id=$1 AND state='claimed' AND owner_id=$2 AND lease_token_hash=$3`, operationID, owner, leaseHash, operationRetryBackoff.String(), code, message)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("stale fence")
	}
	return tx.Commit(ctx)
}

type lifecyclePayloadInput struct {
	Command, OperationID, SandboxID, TenantID, AllocationID, HostID string
	OperationKind                                                   string
	Generation, Incarnation, Fence                                  int64
	SnapshotID                                                      *string
}

// buildLifecyclePayloadTx resolves every command input from the immutable
// profile/recipe artifact and fenced allocation. It intentionally rejects
// incomplete pinned data instead of inventing a host path or runtime default.
func (s *Store) buildLifecyclePayloadTx(ctx context.Context, tx pgx.Tx, in lifecyclePayloadInput) (json.RawMessage, error) {
	if in.Command != "CreateSandbox" && in.Command != "CreateFullSnapshot" && in.Command != "RestoreFullSnapshot" {
		return nil, fmt.Errorf("unsupported lifecycle payload command %q", in.Command)
	}
	var artifactDigest, descriptor string
	var runtimeJSON, machineConfig, topology, drivesJSON, artifactManifest []byte
	var vcpu, memoryMB int
	err := tx.QueryRow(ctx, `SELECT p.vcpu,p.memory_mb,p.runtime_overhead,p.machine_config,p.device_topology,p.mutable_disk_layout,a.artifact_manifest,a.artifact_digest,a.vm_restore_descriptor_digest
		FROM sandboxes s JOIN sandbox_profile_versions p ON p.id=s.profile_version_id
		JOIN sandbox_image_recipe_profile_artifacts a ON a.tenant_id=s.tenant_id AND a.recipe_version_id=s.recipe_version_id AND a.profile_version_id=s.profile_version_id
		WHERE s.id=$1 AND s.tenant_id=$2 AND s.vm_restore_descriptor_digest=a.vm_restore_descriptor_digest
		AND p.status='active' AND a.status='ready'`, in.SandboxID, in.TenantID).Scan(&vcpu, &memoryMB, &runtimeJSON, &machineConfig, &topology, &drivesJSON, &artifactManifest, &artifactDigest, &descriptor)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("pinned profile or recipe artifact is unavailable")
	}
	if err != nil {
		return nil, err
	}
	runtime, err := domain.DecodeProfileRuntimeV1(runtimeJSON)
	if err != nil {
		return nil, fmt.Errorf("pinned profile runtime is invalid: %w", err)
	}
	manifest, err := domain.DecodeRecipeArtifactManifestV1(artifactManifest)
	if err != nil {
		return nil, fmt.Errorf("pinned recipe artifact manifest is invalid: %w", err)
	}
	if artifactDigest == "" || descriptor == "" {
		return nil, fmt.Errorf("pinned recipe artifact digest or restore descriptor is unavailable")
	}
	var mutable []domain.LifecycleDriveV1
	if err := json.Unmarshal(drivesJSON, &mutable); err != nil {
		return nil, fmt.Errorf("pinned mutable drive layout is invalid: %w", err)
	}
	for i := range mutable {
		mutable[i].Mutable = true
		mutable[i].Order = len(manifest.ImmutableDrives) + i
		mutable[i].Artifact = nil
	}
	drives := append(append([]domain.LifecycleDriveV1(nil), manifest.ImmutableDrives...), mutable...)
	machineDigest, err := domain.CanonicalDigest(machineConfig)
	if err != nil {
		return nil, err
	}
	topologyDigest, err := domain.CanonicalDigest(topology)
	if err != nil {
		return nil, err
	}
	var hostLineage, hostKey string
	if err := tx.QueryRow(ctx, `SELECT lineage_id,host_compatibility_key FROM sandbox_hosts WHERE id=$1 AND incarnation=$2`, in.HostID, in.Incarnation).Scan(&hostLineage, &hostKey); err != nil {
		return nil, fmt.Errorf("snapshot source host identity is unavailable: %w", err)
	}
	payload := domain.LifecyclePayloadV1{
		SchemaVersion: "v1", Command: in.Command, SandboxID: in.SandboxID, OperationID: in.OperationID, OperationKind: in.OperationKind, Generation: in.Generation,
		Allocation: domain.AllocationPayloadV1{ID: in.AllocationID, HostID: in.HostID, HostIncarnation: in.Incarnation, FenceEpoch: in.Fence},
		Resources:  runtime.Limits, Machine: domain.MachinePayloadV1{
			SMT: runtime.SMT, MachineConfig: machineConfig, DeviceTopology: topology,
			MachineTopologyDigest: machineDigest, DeviceTopologyDigest: topologyDigest,
		},
		Guest: manifest.Guest, GuestImageDigest: manifest.GuestImageDigest, Drives: drives, Network: runtime.Network, Vsock: runtime.Vsock, Jailer: runtime.Jailer,
		Lineage: domain.RestoreLineageV1{LineageID: hostLineage, SourceHostCompatibilityKey: hostKey, VMRestoreDescriptorDigest: descriptor},
	}
	payload.Resources.VCPU, payload.Resources.MemoryMB = vcpu, memoryMB
	if in.Command == "CreateFullSnapshot" {
		if in.SnapshotID == nil {
			return nil, fmt.Errorf("snapshot ID is unavailable")
		}
		payload.SnapshotID = *in.SnapshotID
	}
	if in.Command == "RestoreFullSnapshot" {
		if in.SnapshotID == nil {
			return nil, fmt.Errorf("restore snapshot is unavailable")
		}
		var snapshotLineage, sourceKey, snapshotDescriptor string
		var rawManifest []byte
		err = tx.QueryRow(ctx, `SELECT lineage_id,source_host_compatibility_key,vm_restore_descriptor_digest,manifest FROM sandbox_snapshots WHERE id=$1 AND tenant_id=$2 AND sandbox_id=$3 AND state='ready'`, *in.SnapshotID, in.TenantID, in.SandboxID).Scan(&snapshotLineage, &sourceKey, &snapshotDescriptor, &rawManifest)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("selected full snapshot is unavailable")
		}
		if err != nil {
			return nil, err
		}
		if snapshotDescriptor != descriptor {
			return nil, fmt.Errorf("selected snapshot VM restore descriptor is incompatible")
		}
		payload.Lineage.LineageID, payload.Lineage.SourceHostCompatibilityKey = snapshotLineage, sourceKey
		payload.Restore = &domain.RestorePayloadV1{SnapshotID: *in.SnapshotID, Manifest: rawManifest}
	}
	var expiry time.Time
	if err := tx.QueryRow(ctx, `SELECT lease_expires_at FROM sandbox_allocations WHERE id=$1 AND state IN('reserved','active')`, in.AllocationID).Scan(&expiry); err != nil {
		return nil, fmt.Errorf("allocation watchdog lease is unavailable")
	}
	grant, err := s.watchdogGrant(in.SandboxID, in.AllocationID, in.HostID, in.Incarnation, in.Fence, expiry)
	if err != nil {
		return nil, err
	}
	payload.WatchdogGrant = grant
	if err := payload.Validate(in.Command); err != nil {
		return nil, fmt.Errorf("invalid canonical lifecycle payload: %w", err)
	}
	return json.Marshal(payload)
}

func commandKindForOperation(kind string) string {
	switch kind {
	case "snapshot", "stop", "restart":
		return "CreateFullSnapshot"
	case "start", "restore", "recover":
		return "RestoreFullSnapshot"
	case "delete":
		return "Destroy"
	case "create":
		return "CreateSandbox"
	default:
		return kind
	}
}

func releaseAllocationTx(ctx context.Context, tx pgx.Tx, allocationID string) error {
	var hostID string
	var vcpu, memoryMB, diskBytes, stagingBytes int64
	err := tx.QueryRow(ctx, `SELECT host_id,reserved_vcpu,reserved_memory_mb,reserved_disk_bytes,reserved_staging_bytes FROM sandbox_allocations WHERE id=$1 FOR UPDATE`, allocationID).Scan(&hostID, &vcpu, &memoryMB, &diskBytes, &stagingBytes)
	if err != nil {
		return err
	}
	if tag, err := tx.Exec(ctx, `UPDATE sandbox_allocations SET state='released',lease_expires_at=now(),updated_at=now() WHERE id=$1 AND state IN('active','releasing','reserved')`, allocationID); err != nil || tag.RowsAffected() != 1 {
		if err != nil {
			return err
		}
		return fmt.Errorf("stale fence")
	}
	if _, err = tx.Exec(ctx, `UPDATE sandbox_hosts SET reserved_vcpu=GREATEST(0,reserved_vcpu-$2),reserved_memory_mb=GREATEST(0,reserved_memory_mb-$3),reserved_disk_bytes=GREATEST(0,reserved_disk_bytes-$4),reserved_staging_bytes=GREATEST(0,reserved_staging_bytes-$5),updated_at=now() WHERE id=$1`, hostID, vcpu, memoryMB, diskBytes, stagingBytes); err != nil {
		return err
	}
	return nil
}

// placeSandboxTx is the provider-neutral minimum placement path. It selects a
// registered, leased ready host with actual free profile resources and creates
// the allocation/fence in this same transaction. Restore candidates additionally
// require exact snapshot lineage/key/descriptor equality and restore capability.
func placeSandboxTx(ctx context.Context, tx pgx.Tx, tenantID, sandboxID string, generation int64, descriptor, kind, commandKind string, selectedSnapshotID *string) (string, string, int64, int64, error) {
	var everBooted bool
	var latestSnapshotID *string
	if err := tx.QueryRow(ctx, `SELECT ever_booted,latest_snapshot_id FROM sandboxes WHERE id=$1 AND tenant_id=$2 AND generation=$3 FOR UPDATE`, sandboxID, tenantID, generation).Scan(&everBooted, &latestSnapshotID); err != nil {
		return "", "", 0, 0, err
	}
	if kind == "create" && everBooted {
		return "", "", 0, 0, fmt.Errorf("initial boot is forbidden after a sandbox has booted")
	}
	restoring := kind == "start" || kind == "restore" || kind == "recover"
	snapshotID := latestSnapshotID
	if selectedSnapshotID != nil {
		snapshotID = selectedSnapshotID
	}
	if restoring && snapshotID == nil {
		return "", "", 0, 0, fmt.Errorf("NO_RECOVERABLE_SNAPSHOT")
	}
	var hostID string
	var incarnation, vcpu, memoryMB, diskBytes, stagingBytes int64
	query := `SELECT h.id,h.incarnation,p.vcpu,p.memory_mb,
	 COALESCE((SELECT sum((x->>'size_bytes')::bigint) FROM jsonb_array_elements(p.mutable_disk_layout) x),0),p.staging_overhead_bytes
	 FROM sandbox_hosts h JOIN sandbox_profile_versions p ON p.id=(SELECT profile_version_id FROM sandboxes WHERE id=$1)
	 WHERE h.state='ready' AND h.lease_expires_at>now() AND h.total_vcpu-h.reserved_vcpu>=p.vcpu AND h.total_memory_mb-h.reserved_memory_mb>=p.memory_mb
	 AND h.total_disk_bytes-h.reserved_disk_bytes>=COALESCE((SELECT sum((x->>'size_bytes')::bigint) FROM jsonb_array_elements(p.mutable_disk_layout) x),0)
	 AND h.total_staging_bytes-h.reserved_staging_bytes>=p.staging_overhead_bytes
	 AND COALESCE((h.vm_restore_capabilities->>$2)::boolean,false)`
	args := []any{sandboxID, commandKind}
	if restoring {
		query += ` AND EXISTS(SELECT 1 FROM sandbox_snapshots sn WHERE sn.id=$3 AND sn.tenant_id=$4 AND sn.sandbox_id=$1 AND sn.state='ready' AND sn.lineage_id=h.lineage_id AND sn.source_host_compatibility_key=h.host_compatibility_key AND sn.vm_restore_descriptor_digest=$5)`
		args = append(args, *snapshotID, tenantID, descriptor)
	}
	query += ` ORDER BY (h.total_memory_mb-h.reserved_memory_mb) ASC,h.id FOR UPDATE SKIP LOCKED LIMIT 1`
	if err := tx.QueryRow(ctx, query, args...).Scan(&hostID, &incarnation, &vcpu, &memoryMB, &diskBytes, &stagingBytes); err != nil {
		return "", "", 0, 0, err
	}
	var fence int64
	if err := tx.QueryRow(ctx, `UPDATE sandboxes SET fence_epoch=fence_epoch+1,updated_at=now() WHERE id=$1 AND tenant_id=$2 AND generation=$3 RETURNING fence_epoch`, sandboxID, tenantID, generation).Scan(&fence); err != nil {
		return "", "", 0, 0, err
	}
	if tag, err := tx.Exec(ctx, `UPDATE sandbox_hosts SET reserved_vcpu=reserved_vcpu+$2,reserved_memory_mb=reserved_memory_mb+$3,reserved_disk_bytes=reserved_disk_bytes+$4,reserved_staging_bytes=reserved_staging_bytes+$5,updated_at=now() WHERE id=$1 AND incarnation=$6 AND state='ready'`, hostID, vcpu, memoryMB, diskBytes, stagingBytes, incarnation); err != nil || tag.RowsAffected() != 1 {
		if err != nil {
			return "", "", 0, 0, err
		}
		return "", "", 0, 0, fmt.Errorf("stale host")
	}
	allocationID := ids.New()
	leaseToken, leaseHash, err := randomLeaseToken()
	if err != nil {
		return "", "", 0, 0, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO sandbox_allocations(id,tenant_id,sandbox_id,host_id,host_incarnation,state,fence_epoch,lease_token_hash,lease_token_envelope,lease_expires_at,reserved_vcpu,reserved_memory_mb,reserved_disk_bytes,reserved_staging_bytes) VALUES($1,$2,$3,$4,$5,'reserved',$6,$7,$8,now()+interval '60 seconds',$9,$10,$11,$12)`, allocationID, tenantID, sandboxID, hostID, incarnation, fence, leaseHash, leaseToken, vcpu, memoryMB, diskBytes, stagingBytes); err != nil {
		return "", "", 0, 0, err
	}
	if tag, err := tx.Exec(ctx, `UPDATE sandboxes SET current_allocation_id=$4 WHERE id=$1 AND tenant_id=$2 AND generation=$3`, sandboxID, tenantID, generation, allocationID); err != nil || tag.RowsAffected() != 1 {
		if err != nil {
			return "", "", 0, 0, err
		}
		return "", "", 0, 0, fmt.Errorf("stale fence")
	}
	return allocationID, hostID, incarnation, fence, nil
}

func createOperationSnapshotTx(ctx context.Context, tx pgx.Tx, operation models.SandboxOperation, tenantID, sandboxID string, generation int64, allocationID, hostID string, incarnation, fence int64, descriptor string) (*string, error) {
	if operation.SnapshotID != nil {
		return operation.SnapshotID, nil
	}
	var lineage, hostKey string
	if err := tx.QueryRow(ctx, `SELECT lineage_id,host_compatibility_key FROM sandbox_hosts WHERE id=$1 AND incarnation=$2`, hostID, incarnation).Scan(&lineage, &hostKey); err != nil {
		return nil, err
	}
	compatibilityKey, err := domain.SnapshotCompatibilityDigest(domain.SnapshotCompatibilityDescriptor{SchemaVersion: "v1", LineageID: lineage, SourceHostCompatibilityKey: hostKey, VMRestoreDescriptorDigest: descriptor})
	if err != nil {
		return nil, err
	}
	id := ids.New()
	kind := "manual"
	if operation.Kind == "stop" {
		kind = "stop"
	}
	if operation.Kind == "restart" {
		kind = "recovery"
	}
	if _, err = tx.Exec(ctx, `INSERT INTO sandbox_snapshots(id,tenant_id,sandbox_id,allocation_id,operation_id,generation,kind,state,lineage_id,source_host_compatibility_key,vm_restore_descriptor_digest,snapshot_compatibility_key) VALUES($1,$2,$3,$4,$5,$6,$7,'requested',$8,$9,$10,$11)`, id, tenantID, sandboxID, allocationID, operation.ID, generation, kind, lineage, hostKey, descriptor, compatibilityKey); err != nil {
		return nil, err
	}
	if tag, err := tx.Exec(ctx, `UPDATE sandbox_operations SET snapshot_id=$2 WHERE id=$1 AND state='claimed'`, operation.ID, id); err != nil || tag.RowsAffected() != 1 {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("stale fence")
	}
	return &id, nil
}

func (s *Store) failUnsupportedRecipeBuild(ctx context.Context, operation models.SandboxOperation, owner, leaseHash string) error {
	if operation.RecipeVersionID == nil {
		return fmt.Errorf("operation lacks sandbox or recipe target")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if tag, err := tx.Exec(ctx, `UPDATE sandbox_operations SET state='failed',failure_code='BUILDER_UNAVAILABLE',failure_message='no registered host supports recipe builds',owner_id=NULL,lease_token_hash=NULL,lease_expires_at=NULL,updated_at=now() WHERE id=$1 AND kind='recipe_build' AND state='claimed' AND owner_id=$2 AND lease_token_hash=$3`, operation.ID, owner, leaseHash); err != nil || tag.RowsAffected() != 1 {
		if err != nil {
			return err
		}
		return fmt.Errorf("stale fence")
	}
	if _, err = tx.Exec(ctx, `UPDATE sandbox_image_recipe_versions SET status='failed',failure_code='BUILDER_UNAVAILABLE',failure_message='no registered host supports recipe builds',updated_at=now() WHERE id=$1 AND status IN('queued','building')`, *operation.RecipeVersionID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ExpireSandboxHostLeases fences lost allocations and returns their sandboxes
// to strict recovery/capacity selection. It never moves a running sandbox to a
// replacement host itself.
func (s *Store) ExpireSandboxHostLeases(ctx context.Context) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `UPDATE sandbox_hosts SET state='unreachable',failure_code='HOST_LEASE_LOST',failure_message='host heartbeat lease expired',updated_at=now() WHERE state IN('ready','draining') AND lease_expires_at<now()`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE sandbox_allocations SET state='lost',updated_at=now() WHERE state IN('reserved','active','releasing') AND lease_expires_at<now()`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE sandbox_commands c SET state='superseded',failure_code='HOST_LEASE_LOST',failure_message='allocation lease expired',updated_at=now()
		FROM sandbox_allocations a WHERE c.allocation_id=a.id AND a.state='lost' AND c.state IN('pending','delivered','acknowledged')`); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE sandbox_operations o SET state='failed',failure_code='HOST_LEASE_LOST',failure_message='allocation lease expired',owner_id=NULL,lease_token_hash=NULL,lease_expires_at=NULL,updated_at=now()
		WHERE o.state IN('claimed','dispatched','waiting') AND EXISTS(SELECT 1 FROM sandbox_commands c JOIN sandbox_allocations a ON a.id=c.allocation_id WHERE c.operation_id=o.id AND a.state='lost')`); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE sandboxes s SET observed_state='recovering',failure_code='HOST_LEASE_LOST',failure_message='host allocation lease expired',updated_at=now() FROM sandbox_allocations a WHERE a.sandbox_id=s.id AND a.tenant_id=s.tenant_id AND a.state='lost' AND s.observed_state IN('running','snapshotting','stopping','provisioning','bootstrapping')`)
	if err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT s.id,s.tenant_id,s.generation FROM sandboxes s JOIN sandbox_allocations a ON a.sandbox_id=s.id AND a.tenant_id=s.tenant_id WHERE a.state='lost' AND s.observed_state='recovering' AND s.latest_snapshot_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM sandbox_operations o WHERE o.sandbox_id=s.id AND o.state IN('queued','claimed','dispatched','waiting')) FOR UPDATE`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var sandboxID, tenantID string
		var generation int64
		if err := rows.Scan(&sandboxID, &tenantID, &generation); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO sandbox_operations(id,tenant_id,sandbox_id,kind,state,requested_generation,idempotency_key,request_hash,actor_id,actor_type,max_attempts) VALUES($1,$2,$3,'recover','queued',$4,$5,$6,'system','system',3)`, ids.New(), tenantID, sandboxID, generation, "recover-"+sandboxID+"-"+fmt.Sprint(generation), "host-lease-lost"); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CreateSandboxAllocation reserves a ready host and increments the sandbox
// fence in the same transaction. Placement callers must use this rather than
// creating allocation rows directly so an old host can never retain authority.
func (s *Store) CreateSandboxAllocation(ctx context.Context, tenantID, sandboxID, hostID string, vcpu, memoryMB int, diskBytes, stagingBytes int64) (string, int64, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var observed string
	var nextFence, incarnation int64
	err = tx.QueryRow(ctx, `UPDATE sandboxes SET fence_epoch=fence_epoch+1,current_allocation_id=NULL,updated_at=now()
		WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL
		RETURNING observed_state,fence_epoch`, tenantID, sandboxID).Scan(&observed, &nextFence)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, ErrNotFound
	}
	if err != nil {
		return "", 0, err
	}
	if observed != "pending" && observed != "stopped" && observed != "recovering" && observed != "awaiting_capacity" {
		return "", 0, fmt.Errorf("sandbox is not placeable: %s", observed)
	}
	err = tx.QueryRow(ctx, `UPDATE sandbox_hosts SET reserved_vcpu=reserved_vcpu+$2,reserved_memory_mb=reserved_memory_mb+$3,reserved_disk_bytes=reserved_disk_bytes+$4,reserved_staging_bytes=reserved_staging_bytes+$5,updated_at=now()
		WHERE id=$1 AND state='ready' AND lease_expires_at>now() AND total_vcpu-reserved_vcpu >= $2 AND total_memory_mb-reserved_memory_mb >= $3 AND total_disk_bytes-reserved_disk_bytes >= $4 AND total_staging_bytes-reserved_staging_bytes >= $5
		RETURNING incarnation`, hostID, vcpu, memoryMB, diskBytes, stagingBytes).Scan(&incarnation)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", 0, fmt.Errorf("no compatible capacity")
	}
	if err != nil {
		return "", 0, err
	}
	allocationID := ids.New()
	leaseToken, leaseTokenHash, tokenErr := randomLeaseToken()
	if tokenErr != nil {
		return "", 0, tokenErr
	}
	if _, err = tx.Exec(ctx, `INSERT INTO sandbox_allocations(id,tenant_id,sandbox_id,host_id,host_incarnation,state,fence_epoch,lease_token_hash,lease_token_envelope,lease_expires_at,reserved_vcpu,reserved_memory_mb,reserved_disk_bytes,reserved_staging_bytes) VALUES($1,$2,$3,$4,$5,'reserved',$6,$7,$8,now()+interval '60 seconds',$9,$10,$11,$12)`, allocationID, tenantID, sandboxID, hostID, incarnation, nextFence, leaseTokenHash, leaseToken, vcpu, memoryMB, diskBytes, stagingBytes); err != nil {
		return "", 0, err
	}
	if _, err = tx.Exec(ctx, `UPDATE sandboxes SET current_allocation_id=$3 WHERE tenant_id=$1 AND id=$2`, tenantID, sandboxID, allocationID); err != nil {
		return "", 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", 0, err
	}
	return allocationID, nextFence, nil
}
