package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/abskrj/velane/services/control-plane/internal/ids"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"time"
)

var ErrNotFound = errors.New("not found")

type CreateSandboxParams struct {
	TenantID, Name, RecipeVersionID, ProfileVersionID, VMRestoreDescriptorDigest, CreatedBy string
	DesiredState, ObservedState                                                             string
}

func (s *Store) CreateSandbox(ctx context.Context, p CreateSandboxParams) (*models.Sandbox, error) {
	x := &models.Sandbox{ID: ids.New(), TenantID: p.TenantID, Name: p.Name, RecipeVersionID: p.RecipeVersionID, ProfileVersionID: p.ProfileVersionID, VMRestoreDescriptorDigest: p.VMRestoreDescriptorDigest, DesiredState: p.DesiredState, ObservedState: p.ObservedState, Generation: 1}
	if x.DesiredState == "" {
		x.DesiredState = "running"
	}
	if x.ObservedState == "" {
		x.ObservedState = "pending"
	}
	err := s.pool.QueryRow(ctx, `INSERT INTO sandboxes(id,tenant_id,name,recipe_version_id,profile_version_id,vm_restore_descriptor_digest,desired_state,observed_state,generation,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING created_at,updated_at`, x.ID, x.TenantID, x.Name, x.RecipeVersionID, x.ProfileVersionID, x.VMRestoreDescriptorDigest, x.DesiredState, x.ObservedState, x.Generation, p.CreatedBy).Scan(&x.CreatedAt, &x.UpdatedAt)
	return x, err
}
func scanSandbox(row pgx.Row) (*models.Sandbox, error) {
	var x models.Sandbox
	err := row.Scan(&x.ID, &x.TenantID, &x.Name, &x.RecipeVersionID, &x.ProfileVersionID, &x.VMRestoreDescriptorDigest, &x.DesiredState, &x.ObservedState, &x.Generation, &x.ObservedGeneration, &x.FenceEpoch, &x.EverBooted, &x.LatestSnapshotID, &x.FailureCode, &x.FailureMessage, &x.CreatedAt, &x.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &x, err
}

const sandboxColumns = `id,tenant_id,name,recipe_version_id,profile_version_id,vm_restore_descriptor_digest,desired_state,observed_state,generation,observed_generation,fence_epoch,ever_booted,latest_snapshot_id,failure_code,failure_message,created_at,updated_at`

func (s *Store) GetSandbox(ctx context.Context, tenantID, id string) (*models.Sandbox, error) {
	return scanSandbox(s.pool.QueryRow(ctx, `SELECT `+sandboxColumns+` FROM sandboxes WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenantID, id))
}
func (s *Store) ListSandboxes(ctx context.Context, tenantID string, limit, offset int) ([]models.Sandbox, int, error) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM sandboxes WHERE tenant_id=$1 AND deleted_at IS NULL`, tenantID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.pool.Query(ctx, `SELECT `+sandboxColumns+` FROM sandboxes WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY created_at DESC LIMIT $2 OFFSET $3`, tenantID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []models.Sandbox
	for rows.Next() {
		x, err := scanSandbox(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *x)
	}
	return out, total, rows.Err()
}
func (s *Store) UpdateSandboxDesiredState(ctx context.Context, tenantID, id, desired string, generation int64) (*models.Sandbox, error) {
	row := s.pool.QueryRow(ctx, `UPDATE sandboxes SET desired_state=$3,generation=generation+1,updated_at=now() WHERE tenant_id=$1 AND id=$2 AND generation=$4 AND deleted_at IS NULL RETURNING `+sandboxColumns, tenantID, id, desired, generation)
	x, err := scanSandbox(row)
	return x, err
}

type CreateOperationParams struct {
	TenantID                                              string
	SandboxID                                             *string
	RecipeVersionID                                       *string
	SnapshotID                                            *string
	RetryOfOperationID                                    *string
	Kind, IdempotencyKey, RequestHash, ActorID, ActorType string
	RequestedGeneration                                   int64
	MaxAttempts                                           int
	DeadlineAt                                            *time.Time
}

func (s *Store) CreateSandboxOperation(ctx context.Context, p CreateOperationParams) (*models.SandboxOperation, error) {
	if p.MaxAttempts == 0 {
		p.MaxAttempts = 3
	}
	o := &models.SandboxOperation{ID: ids.New(), TenantID: p.TenantID, SandboxID: p.SandboxID, RecipeVersionID: p.RecipeVersionID, SnapshotID: p.SnapshotID, RetryOfOperationID: p.RetryOfOperationID, Kind: p.Kind, State: "queued", RequestedGeneration: p.RequestedGeneration, MaxAttempts: p.MaxAttempts, DeadlineAt: p.DeadlineAt}
	err := s.pool.QueryRow(ctx, `INSERT INTO sandbox_operations(id,tenant_id,sandbox_id,recipe_version_id,snapshot_id,retry_of_operation_id,kind,state,requested_generation,idempotency_key,request_hash,actor_id,actor_type,max_attempts,deadline_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING created_at,updated_at`, o.ID, o.TenantID, o.SandboxID, o.RecipeVersionID, o.SnapshotID, o.RetryOfOperationID, o.Kind, o.State, o.RequestedGeneration, p.IdempotencyKey, p.RequestHash, p.ActorID, p.ActorType, o.MaxAttempts, o.DeadlineAt).Scan(&o.CreatedAt, &o.UpdatedAt)
	return o, err
}
func (s *Store) FindOperationByIdempotency(ctx context.Context, tenantID, key string) (*models.SandboxOperation, string, error) {
	var o models.SandboxOperation
	var hash string
	err := s.pool.QueryRow(ctx, `SELECT id,tenant_id,sandbox_id,recipe_version_id,snapshot_id,retry_of_operation_id,kind,state,requested_generation,attempt,max_attempts,deadline_at,failure_code,failure_message,result,created_at,updated_at,request_hash FROM sandbox_operations WHERE tenant_id=$1 AND idempotency_key=$2`, tenantID, key).Scan(&o.ID, &o.TenantID, &o.SandboxID, &o.RecipeVersionID, &o.SnapshotID, &o.RetryOfOperationID, &o.Kind, &o.State, &o.RequestedGeneration, &o.Attempt, &o.MaxAttempts, &o.DeadlineAt, &o.FailureCode, &o.FailureMessage, &o.Result, &o.CreatedAt, &o.UpdatedAt, &hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	return &o, hash, err
}
func (s *Store) ClaimDueSandboxOperations(ctx context.Context, owner, leaseHash string, lease time.Duration, limit int) ([]models.SandboxOperation, error) {
	if limit < 1 {
		limit = 1
	}
	rows, err := s.pool.Query(ctx, `WITH due AS (SELECT id FROM sandbox_operations WHERE state IN('queued','waiting') AND next_attempt_at<=now() AND (lease_expires_at IS NULL OR lease_expires_at<now()) ORDER BY next_attempt_at FOR UPDATE SKIP LOCKED LIMIT $1) UPDATE sandbox_operations o SET state='claimed',owner_id=$2,lease_token_hash=$3,lease_expires_at=now()+$4::interval,updated_at=now() FROM due WHERE o.id=due.id RETURNING o.id,o.tenant_id,o.sandbox_id,o.recipe_version_id,o.snapshot_id,o.retry_of_operation_id,o.kind,o.state,o.requested_generation,o.attempt,o.max_attempts,o.deadline_at,o.failure_code,o.failure_message,o.result,o.created_at,o.updated_at`, limit, owner, leaseHash, lease.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []models.SandboxOperation
	for rows.Next() {
		var o models.SandboxOperation
		if err := rows.Scan(&o.ID, &o.TenantID, &o.SandboxID, &o.RecipeVersionID, &o.SnapshotID, &o.RetryOfOperationID, &o.Kind, &o.State, &o.RequestedGeneration, &o.Attempt, &o.MaxAttempts, &o.DeadlineAt, &o.FailureCode, &o.FailureMessage, &o.Result, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
func (s *Store) CompleteSandboxOperation(ctx context.Context, id, owner, leaseHash string, generation int64, state, failureCode, failureMessage string, result any) error {
	b, err := json.Marshal(result)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE sandbox_operations SET state=$5,failure_code=$6,failure_message=$7,result=$8,owner_id=NULL,lease_token_hash=NULL,lease_expires_at=NULL,updated_at=now() WHERE id=$1 AND owner_id=$2 AND lease_token_hash=$3 AND requested_generation=$4 AND state='claimed'`, id, owner, leaseHash, generation, state, failureCode, failureMessage, b)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return errors.New("stale fence")
	}
	return nil
}

type SandboxIntentResult struct {
	Sandbox      *models.Sandbox
	Operation    *models.SandboxOperation
	Replayed     bool
	ExistingHash string
}

func isSandboxIdempotencyRace(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "sandbox_operations_tenant_id_idempotency_key_key"
}

func (s *Store) replaySandboxIntent(ctx context.Context, tenant, key string) (*SandboxIntentResult, error) {
	operation, hash, err := s.FindOperationByIdempotency(ctx, tenant, key)
	if err != nil {
		return nil, err
	}
	return &SandboxIntentResult{Operation: operation, Replayed: true, ExistingHash: hash}, nil
}

// CreateSandboxIntent atomically arbitrates idempotency, locks the tenant quota,
// pins a ready recipe/profile artifact, and persists intent/event/outbox/audit.
func (s *Store) CreateSandboxIntent(ctx context.Context, p CreateSandboxParams, idempotencyKey, requestHash, actorID, actorType string) (*SandboxIntentResult, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var existing models.SandboxOperation
	var hash string
	err = tx.QueryRow(ctx, `SELECT id,tenant_id,sandbox_id,recipe_version_id,snapshot_id,retry_of_operation_id,kind,state,requested_generation,attempt,max_attempts,deadline_at,failure_code,failure_message,result,created_at,updated_at,request_hash FROM sandbox_operations WHERE tenant_id=$1 AND idempotency_key=$2 FOR UPDATE`, p.TenantID, idempotencyKey).Scan(&existing.ID, &existing.TenantID, &existing.SandboxID, &existing.RecipeVersionID, &existing.SnapshotID, &existing.RetryOfOperationID, &existing.Kind, &existing.State, &existing.RequestedGeneration, &existing.Attempt, &existing.MaxAttempts, &existing.DeadlineAt, &existing.FailureCode, &existing.FailureMessage, &existing.Result, &existing.CreatedAt, &existing.UpdatedAt, &hash)
	if err == nil {
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &SandboxIntentResult{Operation: &existing, Replayed: true, ExistingHash: hash}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO tenant_sandbox_quotas(tenant_id) VALUES($1) ON CONFLICT(tenant_id) DO NOTHING`, p.TenantID); err != nil {
		return nil, err
	}
	var max, count int
	// Acquire the quota row lock before counting. Combining the count subquery
	// with SELECT ... FOR UPDATE can evaluate it before a waiter obtains the
	// lock, allowing concurrent transactions to act on a stale count.
	if err = tx.QueryRow(ctx, `SELECT max_sandboxes FROM tenant_sandbox_quotas WHERE tenant_id=$1 FOR UPDATE`, p.TenantID).Scan(&max); err != nil {
		return nil, err
	}
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM sandboxes WHERE tenant_id=$1 AND deleted_at IS NULL`, p.TenantID).Scan(&count); err != nil {
		return nil, err
	}
	if count >= max {
		return nil, fmt.Errorf("quota exceeded")
	}
	var desc string
	if err = tx.QueryRow(ctx, `SELECT a.vm_restore_descriptor_digest FROM sandbox_image_recipe_profile_artifacts a JOIN sandbox_profile_versions p ON p.id=a.profile_version_id WHERE a.tenant_id=$1 AND a.recipe_version_id=$2 AND a.profile_version_id=$3 AND a.status='ready' AND p.status='active'`, p.TenantID, p.RecipeVersionID, p.ProfileVersionID).Scan(&desc); err != nil {
		return nil, err
	}
	sb := &models.Sandbox{ID: ids.New(), TenantID: p.TenantID, Name: p.Name, RecipeVersionID: p.RecipeVersionID, ProfileVersionID: p.ProfileVersionID, VMRestoreDescriptorDigest: desc, DesiredState: "running", ObservedState: "pending", Generation: 1}
	if err = tx.QueryRow(ctx, `INSERT INTO sandboxes(id,tenant_id,name,recipe_version_id,profile_version_id,vm_restore_descriptor_digest,desired_state,observed_state,generation,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING created_at,updated_at`, sb.ID, sb.TenantID, sb.Name, sb.RecipeVersionID, sb.ProfileVersionID, sb.VMRestoreDescriptorDigest, sb.DesiredState, sb.ObservedState, sb.Generation, p.CreatedBy).Scan(&sb.CreatedAt, &sb.UpdatedAt); err != nil {
		return nil, err
	}
	op := &models.SandboxOperation{ID: ids.New(), TenantID: p.TenantID, SandboxID: &sb.ID, Kind: "create", State: "queued", RequestedGeneration: sb.Generation, MaxAttempts: 3}
	if err = tx.QueryRow(ctx, `INSERT INTO sandbox_operations(id,tenant_id,sandbox_id,kind,state,requested_generation,idempotency_key,request_hash,actor_id,actor_type,max_attempts) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING created_at,updated_at`, op.ID, op.TenantID, op.SandboxID, op.Kind, op.State, op.RequestedGeneration, idempotencyKey, requestHash, actorID, actorType, op.MaxAttempts).Scan(&op.CreatedAt, &op.UpdatedAt); err != nil {
		if isSandboxIdempotencyRace(err) {
			_ = tx.Rollback(ctx)
			return s.replaySandboxIntent(ctx, p.TenantID, idempotencyKey)
		}
		return nil, err
	}
	if err = eventTx(ctx, tx, p.TenantID, sb.ID, op.ID, "sandbox.create_requested", "sandbox creation requested", sb.Generation); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(id,tenant_id,actor_id,actor_type,action,resource_id,metadata) VALUES($1,$2,$3,$4,'sandbox.create_requested',$5,$6)`, ids.New(), p.TenantID, actorID, actorType, sb.ID, `{"operation_id":"`+op.ID+`"}`); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &SandboxIntentResult{Sandbox: sb, Operation: op}, nil
}

// RequestSandboxOperation atomically changes desired state/generation for lifecycle intent and creates its operation.
func (s *Store) RequestSandboxOperation(ctx context.Context, tenant, sandbox, kind, key, hash, actor, actorType string, expected *int64, snapshotID *string, retryOf *string, body ...any) (*SandboxIntentResult, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var old models.SandboxOperation
	var oldHash string
	err = tx.QueryRow(ctx, `SELECT id,tenant_id,sandbox_id,recipe_version_id,snapshot_id,retry_of_operation_id,kind,state,requested_generation,attempt,max_attempts,deadline_at,failure_code,failure_message,result,created_at,updated_at,request_hash FROM sandbox_operations WHERE tenant_id=$1 AND idempotency_key=$2 FOR UPDATE`, tenant, key).Scan(&old.ID, &old.TenantID, &old.SandboxID, &old.RecipeVersionID, &old.SnapshotID, &old.RetryOfOperationID, &old.Kind, &old.State, &old.RequestedGeneration, &old.Attempt, &old.MaxAttempts, &old.DeadlineAt, &old.FailureCode, &old.FailureMessage, &old.Result, &old.CreatedAt, &old.UpdatedAt, &oldHash)
	if err == nil {
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &SandboxIntentResult{Operation: &old, Replayed: true, ExistingHash: oldHash}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	sb, err := scanSandbox(tx.QueryRow(ctx, `SELECT `+sandboxColumns+` FROM sandboxes WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, tenant, sandbox))
	if err != nil {
		return nil, err
	}
	if expected != nil && *expected != sb.Generation {
		return nil, fmt.Errorf("generation conflict")
	}
	// Do not accept work that this deployment cannot route safely. This is a
	// server-side admission gate; reconcilers still defend the same invariants.
	if kind == "snapshot" || kind == "stop" || kind == "restart" {
		if sb.ObservedState != "running" {
			return nil, fmt.Errorf("operation conflict: a full snapshot requires a running sandbox")
		}
	}
	if kind == "start" && (sb.ObservedState != "stopped" || !sb.EverBooted || sb.LatestSnapshotID == nil) {
		return nil, fmt.Errorf("operation conflict: start requires a stopped sandbox with a recovery snapshot")
	}
	if snapshotID != nil {
		var state string
		err = tx.QueryRow(ctx, `SELECT state FROM sandbox_snapshots WHERE tenant_id=$1 AND sandbox_id=$2 AND id=$3 AND state<>'deleted' FOR UPDATE`, tenant, sandbox, *snapshotID).Scan(&state)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		if kind == "restore" && (state != "ready" || sb.ObservedState != "stopped") {
			return nil, fmt.Errorf("restore requires a ready snapshot and stopped sandbox")
		}
		if kind == "snapshot_delete" && sb.LatestSnapshotID != nil && *sb.LatestSnapshotID == *snapshotID {
			return nil, fmt.Errorf("latest recovery snapshot cannot be deleted")
		}
	}
	if kind == "delete" {
		deleteSnapshots := false
		if len(body) > 0 {
			if request, ok := body[0].(map[string]any); ok {
				deleteSnapshots, _ = request["delete_snapshots"].(bool)
			}
		}
		var snapshots int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM sandbox_snapshots WHERE tenant_id=$1 AND sandbox_id=$2 AND state<>'deleted'`, tenant, sandbox).Scan(&snapshots); err != nil {
			return nil, err
		}
		if snapshots > 0 && !deleteSnapshots {
			return nil, fmt.Errorf("delete requires delete_snapshots when snapshots are retained")
		}
	}
	desired := sb.DesiredState
	if kind == "start" || kind == "restart" || kind == "restore" {
		desired = "running"
	}
	if kind == "stop" {
		desired = "stopped"
	}
	if kind == "delete" {
		desired = "deleted"
	}
	if kind == "snapshot" {
		desired = sb.DesiredState
	}
	if desired != sb.DesiredState {
		if err = tx.QueryRow(ctx, `UPDATE sandboxes SET desired_state=$3,generation=generation+1,updated_at=now() WHERE tenant_id=$1 AND id=$2 RETURNING `+sandboxColumns, tenant, sandbox, desired).Scan(&sb.ID, &sb.TenantID, &sb.Name, &sb.RecipeVersionID, &sb.ProfileVersionID, &sb.VMRestoreDescriptorDigest, &sb.DesiredState, &sb.ObservedState, &sb.Generation, &sb.ObservedGeneration, &sb.FenceEpoch, &sb.EverBooted, &sb.LatestSnapshotID, &sb.FailureCode, &sb.FailureMessage, &sb.CreatedAt, &sb.UpdatedAt); err != nil {
			return nil, err
		}
	}
	op := &models.SandboxOperation{ID: ids.New(), TenantID: tenant, SandboxID: &sandbox, SnapshotID: snapshotID, RetryOfOperationID: retryOf, Kind: kind, State: "queued", RequestedGeneration: sb.Generation, MaxAttempts: 3}
	if err = tx.QueryRow(ctx, `INSERT INTO sandbox_operations(id,tenant_id,sandbox_id,snapshot_id,retry_of_operation_id,kind,state,requested_generation,idempotency_key,request_hash,actor_id,actor_type,max_attempts) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING created_at,updated_at`, op.ID, tenant, sandbox, snapshotID, retryOf, kind, "queued", sb.Generation, key, hash, actor, actorType, 3).Scan(&op.CreatedAt, &op.UpdatedAt); err != nil {
		if isSandboxIdempotencyRace(err) {
			_ = tx.Rollback(ctx)
			return s.replaySandboxIntent(ctx, tenant, key)
		}
		return nil, err
	}
	if err = eventTx(ctx, tx, tenant, sandbox, op.ID, "operation.state_changed", "sandbox operation requested", sb.Generation); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(id,tenant_id,actor_id,actor_type,action,resource_id,metadata) VALUES($1,$2,$3,$4,$5,$6,$7)`, ids.New(), tenant, actor, actorType, "sandbox."+kind+"_requested", sandbox, `{"operation_id":"`+op.ID+`"}`); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &SandboxIntentResult{Sandbox: sb, Operation: op}, nil
}

// RetrySandboxOperation creates a new attempt of a failed sandbox operation.
// It deliberately preserves the original kind and snapshot target.
func (s *Store) RetrySandboxOperation(ctx context.Context, tenant, sandbox, parentID, key, hash, actor, actorType string, expected *int64) (*SandboxIntentResult, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var existing models.SandboxOperation
	var existingHash string
	err = tx.QueryRow(ctx, `SELECT id,tenant_id,sandbox_id,recipe_version_id,snapshot_id,retry_of_operation_id,kind,state,requested_generation,attempt,max_attempts,deadline_at,failure_code,failure_message,result,created_at,updated_at,request_hash FROM sandbox_operations WHERE tenant_id=$1 AND idempotency_key=$2 FOR UPDATE`, tenant, key).Scan(&existing.ID, &existing.TenantID, &existing.SandboxID, &existing.RecipeVersionID, &existing.SnapshotID, &existing.RetryOfOperationID, &existing.Kind, &existing.State, &existing.RequestedGeneration, &existing.Attempt, &existing.MaxAttempts, &existing.DeadlineAt, &existing.FailureCode, &existing.FailureMessage, &existing.Result, &existing.CreatedAt, &existing.UpdatedAt, &existingHash)
	if err == nil {
		if err = tx.Commit(ctx); err != nil {
			return nil, err
		}
		return &SandboxIntentResult{Operation: &existing, Replayed: true, ExistingHash: existingHash}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	sb, err := scanSandbox(tx.QueryRow(ctx, `SELECT `+sandboxColumns+` FROM sandboxes WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR UPDATE`, tenant, sandbox))
	if err != nil {
		return nil, err
	}
	if expected != nil && *expected != sb.Generation {
		return nil, fmt.Errorf("generation conflict")
	}
	var parent models.SandboxOperation
	err = tx.QueryRow(ctx, `SELECT id,tenant_id,sandbox_id,recipe_version_id,snapshot_id,retry_of_operation_id,kind,state,requested_generation,attempt,max_attempts,deadline_at,failure_code,failure_message,result,created_at,updated_at FROM sandbox_operations WHERE tenant_id=$1 AND sandbox_id=$2 AND id=$3 FOR UPDATE`, tenant, sandbox, parentID).Scan(&parent.ID, &parent.TenantID, &parent.SandboxID, &parent.RecipeVersionID, &parent.SnapshotID, &parent.RetryOfOperationID, &parent.Kind, &parent.State, &parent.RequestedGeneration, &parent.Attempt, &parent.MaxAttempts, &parent.DeadlineAt, &parent.FailureCode, &parent.FailureMessage, &parent.Result, &parent.CreatedAt, &parent.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if parent.State != "failed" || parent.Kind == "recipe_build" {
		return nil, fmt.Errorf("operation conflict")
	}
	var active int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM sandbox_operations WHERE tenant_id=$1 AND sandbox_id=$2 AND state IN('queued','claimed','dispatched','waiting')`, tenant, sandbox).Scan(&active); err != nil {
		return nil, err
	}
	if active > 0 {
		return nil, fmt.Errorf("operation conflict")
	}
	op := &models.SandboxOperation{ID: ids.New(), TenantID: tenant, SandboxID: &sandbox, SnapshotID: parent.SnapshotID, RetryOfOperationID: &parent.ID, Kind: parent.Kind, State: "queued", RequestedGeneration: sb.Generation, MaxAttempts: parent.MaxAttempts}
	if err = tx.QueryRow(ctx, `INSERT INTO sandbox_operations(id,tenant_id,sandbox_id,snapshot_id,retry_of_operation_id,kind,state,requested_generation,idempotency_key,request_hash,actor_id,actor_type,max_attempts) VALUES($1,$2,$3,$4,$5,$6,'queued',$7,$8,$9,$10,$11,$12) RETURNING created_at,updated_at`, op.ID, tenant, sandbox, op.SnapshotID, op.RetryOfOperationID, op.Kind, op.RequestedGeneration, key, hash, actor, actorType, op.MaxAttempts).Scan(&op.CreatedAt, &op.UpdatedAt); err != nil {
		return nil, err
	}
	if err = eventTx(ctx, tx, tenant, sandbox, op.ID, "operation.state_changed", "sandbox operation retry requested", sb.Generation); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(id,tenant_id,actor_id,actor_type,action,resource_id,metadata) VALUES($1,$2,$3,$4,'sandbox.operation_retry_requested',$5,$6)`, ids.New(), tenant, actor, actorType, sandbox, fmt.Sprintf(`{"parent_operation_id":%q,"operation_id":%q,"kind":%q}`, parent.ID, op.ID, parent.Kind)); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &SandboxIntentResult{Sandbox: sb, Operation: op}, nil
}
