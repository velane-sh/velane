package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/abskrj/velane/services/sandbox-host-agent/internal/controlplane"
	"github.com/abskrj/velane/services/sandbox-host-agent/internal/reconcile"
)

type ControlClient interface {
	EnrollmentChallenge(context.Context, string, string, string) (controlplane.EnrollmentChallenge, error)
	Enroll(context.Context, controlplane.RegisterRequest) (controlplane.RegisterResponse, error)
	Register(context.Context, controlplane.RegisterRequest) (controlplane.RegisterResponse, error)
	Renew(context.Context, string, string) (controlplane.RegisterResponse, error)
	Heartbeat(context.Context, string, controlplane.HeartbeatRequest) (controlplane.HeartbeatResponse, error)
	Poll(context.Context, string, uint64) ([]controlplane.Command, error)
	Ack(context.Context, string, string) error
	Result(context.Context, string, string, controlplane.CommandResult) error
}
type IdentityInstaller interface{ InstallIdentity(string) error }
type Loop struct {
	Client                                       ControlClient
	Agent                                        Agent
	HostID, PoolID, BootID, HostCompatibilityKey string
	HostIncarnation                              uint64
	HeartbeatInterval                            time.Duration
	WatchdogHealthy                              func() bool
	CSR                                          string
	CertificateExpiresAt                         time.Time
	ProviderEvidence                             func(context.Context, string) (identityDocument, identitySignature, stsSignedRequest string, err error)
	Inventory                                    controlplane.HostInventory
	Watchdog                                     interface {
		Deliver(context.Context, json.RawMessage) error
	}
	allocations map[string]controlplane.AllocationLeaseRenewal
}

func (l *Loop) Run(ctx context.Context) error {
	if l.Client == nil {
		return fmt.Errorf("host-control client is required")
	}
	if l.Agent.Journal == nil || l.Agent.Executor == nil {
		return fmt.Errorf("command journal and executor are required")
	}
	if l.HeartbeatInterval <= 0 {
		l.HeartbeatInterval = 10 * time.Second
	}
	if l.HostID == "" {
		if l.CSR == "" || l.ProviderEvidence == nil {
			return fmt.Errorf("first-boot enrollment CSR and AWS evidence are required")
		}
		challenge, err := l.Client.EnrollmentChallenge(ctx, l.PoolID, "aws", l.CSR)
		if err != nil {
			return fmt.Errorf("obtain enrollment challenge: %w", err)
		}
		document, signature, stsProof, err := l.ProviderEvidence(ctx, challenge.Nonce)
		if err != nil {
			return fmt.Errorf("collect AWS enrollment evidence: %w", err)
		}
		registered, err := l.Client.Enroll(ctx, controlplane.RegisterRequest{PoolID: l.PoolID, BootID: l.BootID, HostCompatibilityKey: l.HostCompatibilityKey, Provider: "aws", CSR: l.CSR, ChallengeID: challenge.ChallengeID, Nonce: challenge.Nonce, IdentityDocument: document, IdentitySignature: signature, STSSignedRequest: stsProof})
		if err != nil {
			return fmt.Errorf("register host: %w", err)
		}
		installer, ok := l.Client.(IdentityInstaller)
		if !ok {
			return fmt.Errorf("host-control client cannot persist enrolled mTLS identity")
		}
		if registered.CertificatePEM != "" {
			if err := installer.InstallIdentity(registered.CertificatePEM); err != nil {
				return fmt.Errorf("persist enrolled mTLS identity: %w", err)
			}
		}
		l.HostID = registered.HostID
		l.HostIncarnation = registered.Incarnation
		l.CertificateExpiresAt = registered.CertificateExpiresAt
	}
	if !l.CertificateExpiresAt.IsZero() && !l.CertificateExpiresAt.After(time.Now().Add(2*time.Hour)) {
		if err := l.renewIdentity(ctx); err != nil {
			return err
		}
	}
	if err := l.Inventory.Validate(); err != nil {
		return fmt.Errorf("host inventory: %w", err)
	}
	registered, err := l.Client.Register(ctx, controlplane.RegisterRequest{PoolID: l.PoolID, BootID: l.BootID, HostCompatibilityKey: l.HostCompatibilityKey, CommandCapabilities: l.Inventory.CommandCapabilities, TotalVCPU: l.Inventory.TotalVCPU, TotalMemoryMB: l.Inventory.TotalMemoryMB, TotalDiskBytes: l.Inventory.TotalDiskBytes, TotalStagingBytes: l.Inventory.TotalStagingBytes, ProtocolVersion: l.Inventory.ProtocolVersion, AgentVersion: l.Inventory.AgentVersion})
	if err != nil {
		return fmt.Errorf("register authenticated host inventory: %w", err)
	}
	if registered.HostID != l.HostID || registered.Incarnation != l.HostIncarnation {
		return fmt.Errorf("authenticated host registration identity mismatch")
	}
	ticker := time.NewTicker(l.HeartbeatInterval)
	defer ticker.Stop()
	var sequence uint64
	if l.allocations == nil {
		l.allocations = map[string]controlplane.AllocationLeaseRenewal{}
	}
	for {
		if !l.CertificateExpiresAt.IsZero() && !l.CertificateExpiresAt.After(time.Now().Add(2*time.Hour)) {
			if err := l.renewIdentity(ctx); err != nil {
				return err
			}
		}
		healthy := true
		if l.WatchdogHealthy != nil {
			healthy = l.WatchdogHealthy()
		}
		renewals := make([]controlplane.AllocationLeaseRenewal, 0, len(l.allocations))
		for _, allocation := range l.allocations {
			renewals = append(renewals, allocation)
		}
		heartbeat, err := l.Client.Heartbeat(ctx, l.HostID, controlplane.HeartbeatRequest{Sequence: sequence, WatchdogHealthy: healthy, SentAt: time.Now().UTC(), Allocations: renewals})
		if err != nil {
			return fmt.Errorf("heartbeat: %w", err)
		}
		if len(heartbeat.WatchdogGrants) > 0 && l.Watchdog == nil {
			return fmt.Errorf("watchdog lease delivery is required")
		}
		for _, grant := range heartbeat.WatchdogGrants {
			if err := l.Watchdog.Deliver(ctx, grant); err != nil {
				return fmt.Errorf("deliver renewed watchdog lease: %w", err)
			}
		}
		commands, err := l.Client.Poll(ctx, l.HostID, sequence)
		if err != nil {
			return fmt.Errorf("poll commands: %w", err)
		}
		for _, command := range commands {
			if err := l.handle(ctx, command); err != nil {
				return err
			}
			if command.Sequence > sequence {
				sequence = command.Sequence
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (l *Loop) renewIdentity(ctx context.Context) error {
	if l.CSR == "" {
		return fmt.Errorf("certificate renewal CSR is required")
	}
	renewed, err := l.Client.Renew(ctx, l.HostID, l.CSR)
	if err != nil {
		return fmt.Errorf("renew host certificate: %w", err)
	}
	installer, ok := l.Client.(IdentityInstaller)
	if !ok {
		return fmt.Errorf("host-control client cannot persist renewed mTLS identity")
	}
	if renewed.CertificatePEM == "" || renewed.CertificateExpiresAt.IsZero() {
		return fmt.Errorf("certificate renewal returned incomplete identity")
	}
	if err := installer.InstallIdentity(renewed.CertificatePEM); err != nil {
		return fmt.Errorf("persist renewed mTLS identity: %w", err)
	}
	l.CertificateExpiresAt = renewed.CertificateExpiresAt
	return nil
}
func (l *Loop) handle(ctx context.Context, c controlplane.Command) error {
	fence := reconcile.CommandFence{SandboxID: sandboxID(c.Payload), AllocationID: c.AllocationID, Incarnation: c.HostIncarnation, FenceEpoch: c.FenceEpoch, Sequence: c.Sequence, CommandID: c.ID}
	if fence.SandboxID == "" {
		return fmt.Errorf("command %s lacks sandbox_id", c.ID)
	}
	if err := l.Agent.Journal.Accept(fence); err != nil {
		return l.Client.Result(ctx, l.HostID, c.ID, controlplane.CommandResult{AllocationID: c.AllocationID, HostIncarnation: c.HostIncarnation, FenceEpoch: c.FenceEpoch, CommandID: c.ID, Succeeded: false, FailureCode: "STALE_FENCE", FailureMessage: err.Error()})
	}
	if err := l.Client.Ack(ctx, l.HostID, c.ID); err != nil {
		return fmt.Errorf("ack command %s: %w", c.ID, err)
	}
	if c.LeaseToken == "" || len(c.WatchdogGrant) == 0 {
		return fmt.Errorf("command %s lacks allocation lease authority", c.ID)
	}
	if l.allocations == nil {
		l.allocations = map[string]controlplane.AllocationLeaseRenewal{}
	}
	l.allocations[c.AllocationID] = controlplane.AllocationLeaseRenewal{AllocationID: c.AllocationID, LeaseToken: c.LeaseToken, HostIncarnation: c.HostIncarnation, FenceEpoch: c.FenceEpoch}
	payload, err := payloadWithWatchdogGrant(c.Payload, c.WatchdogGrant)
	if err != nil {
		return fmt.Errorf("attach watchdog grant: %w", err)
	}
	err = l.Agent.Executor.Execute(ctx, controlplane.Command{ID: c.ID, Kind: c.Kind, AllocationID: c.AllocationID, HostIncarnation: c.HostIncarnation, FenceEpoch: c.FenceEpoch, Sequence: c.Sequence, Payload: payload, LeaseToken: c.LeaseToken, WatchdogGrant: c.WatchdogGrant})
	r := controlplane.CommandResult{AllocationID: c.AllocationID, HostIncarnation: c.HostIncarnation, FenceEpoch: c.FenceEpoch, CommandID: c.ID, Succeeded: err == nil}
	if err != nil {
		r.FailureCode = "HOST_COMMAND_FAILED"
		r.FailureMessage = err.Error()
	}
	if resultErr := l.Client.Result(ctx, l.HostID, c.ID, r); resultErr != nil {
		return resultErr
	}
	if err == nil && c.Kind == string(controlplane.CommandDestroy) {
		delete(l.allocations, c.AllocationID)
	}
	return nil
}

func payloadWithWatchdogGrant(payload, grant json.RawMessage) (json.RawMessage, error) {
	var value map[string]any
	if err := json.Unmarshal(payload, &value); err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(grant, &decoded); err != nil {
		return nil, err
	}
	value["signed_watchdog_grant"] = decoded
	return json.Marshal(value)
}
func sandboxID(payload json.RawMessage) string {
	var v struct {
		SandboxID string `json:"sandbox_id"`
	}
	_ = json.Unmarshal(payload, &v)
	return v.SandboxID
}
