package agent

import (
	"context"
	"encoding/json"
	"github.com/abskrj/velane/services/sandbox-host-agent/internal/controlplane"
	"github.com/abskrj/velane/services/sandbox-host-agent/internal/reconcile"
	"path/filepath"
	"testing"
	"time"
)

type fakeControl struct {
	commands       []controlplane.Command
	acked, results int
}

func (f *fakeControl) InstallIdentity(string) error { return nil }

func (f *fakeControl) EnrollmentChallenge(context.Context, string, string, string) (controlplane.EnrollmentChallenge, error) {
	return controlplane.EnrollmentChallenge{ChallengeID: "challenge", Nonce: "n"}, nil
}
func (f *fakeControl) Register(context.Context, controlplane.RegisterRequest) (controlplane.RegisterResponse, error) {
	return controlplane.RegisterResponse{HostID: "h", Incarnation: 1, CertificatePEM: "issued"}, nil
}
func (f *fakeControl) Enroll(ctx context.Context, request controlplane.RegisterRequest) (controlplane.RegisterResponse, error) {
	return f.Register(ctx, request)
}
func (f *fakeControl) Renew(context.Context, string, string) (controlplane.RegisterResponse, error) {
	return controlplane.RegisterResponse{CertificatePEM: "renewed", CertificateExpiresAt: time.Now().Add(24 * time.Hour)}, nil
}
func (f *fakeControl) Heartbeat(context.Context, string, controlplane.HeartbeatRequest) (controlplane.HeartbeatResponse, error) {
	return controlplane.HeartbeatResponse{}, nil
}
func (f *fakeControl) Poll(context.Context, string, uint64) ([]controlplane.Command, error) {
	c := f.commands
	f.commands = nil
	return c, nil
}
func (f *fakeControl) Ack(context.Context, string, string) error { f.acked++; return nil }
func (f *fakeControl) Result(context.Context, string, string, controlplane.CommandResult) error {
	f.results++
	return nil
}

type executor struct{ calls int }

func (e *executor) Execute(context.Context, controlplane.Command) error { e.calls++; return nil }

type grantSink struct{ grants int }

func (s *grantSink) Deliver(context.Context, json.RawMessage) error { s.grants++; return nil }
func TestCommandIsJournaledBeforeAckAndExecution(t *testing.T) {
	j, err := reconcile.OpenJournal(filepath.Join(t.TempDir(), "journal"))
	if err != nil {
		t.Fatal(err)
	}
	e := &executor{}
	c := &fakeControl{}
	l := Loop{Client: c, Agent: Agent{Journal: j, Executor: e}, HostID: "h"}
	payload, _ := json.Marshal(map[string]string{"sandbox_id": "s"})
	grant := json.RawMessage(`{"grant":"signed"}`)
	if err := l.handle(context.Background(), controlplane.Command{ID: "c", AllocationID: "a", HostIncarnation: 1, FenceEpoch: 1, Sequence: 1, LeaseToken: "lease", WatchdogGrant: grant, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if c.acked != 1 || c.results != 1 || e.calls != 1 {
		t.Fatalf("ack=%d result=%d exec=%d", c.acked, c.results, e.calls)
	}
	if err := l.handle(context.Background(), controlplane.Command{ID: "old", AllocationID: "a", HostIncarnation: 1, FenceEpoch: 1, Sequence: 1, LeaseToken: "lease", WatchdogGrant: grant, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	if e.calls != 1 {
		t.Fatal("stale command was executed")
	}
	time.Sleep(0)
}

func TestSequentialCommandsWithinFenceUseSequence(t *testing.T) {
	j, err := reconcile.OpenJournal(filepath.Join(t.TempDir(), "journal"))
	if err != nil {
		t.Fatal(err)
	}
	e := &executor{}
	c := &fakeControl{}
	l := Loop{Client: c, Agent: Agent{Journal: j, Executor: e}, HostID: "h"}
	payload, _ := json.Marshal(map[string]string{"sandbox_id": "s"})
	grant := json.RawMessage(`{"grant":"signed"}`)
	for sequence, id := range []string{"checkpoint", "destroy", "restore"} {
		if err := l.handle(context.Background(), controlplane.Command{ID: id, Kind: "CreateSandbox", AllocationID: "a", HostIncarnation: 1, FenceEpoch: 1, Sequence: uint64(sequence + 1), LeaseToken: "lease", WatchdogGrant: grant, Payload: payload}); err != nil {
			t.Fatal(err)
		}
	}
	if e.calls != 3 {
		t.Fatalf("executed %d commands, want 3", e.calls)
	}
}

func TestPayloadWithWatchdogGrant(t *testing.T) {
	payload, err := payloadWithWatchdogGrant(json.RawMessage(`{"sandbox_id":"s"}`), json.RawMessage(`{"fence_epoch":2}`))
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["signed_watchdog_grant"] == nil {
		t.Fatal("signed watchdog grant not attached")
	}
}

func TestRunRejectsEmptyInventoryBeforeHeartbeat(t *testing.T) {
	j, err := reconcile.OpenJournal(filepath.Join(t.TempDir(), "journal"))
	if err != nil {
		t.Fatal(err)
	}
	loop := Loop{Client: &fakeControl{}, Agent: Agent{Journal: j, Executor: &executor{}}, HostID: "h", PoolID: "p", BootID: "boot", HostCompatibilityKey: "key"}
	if err := loop.Run(context.Background()); err == nil {
		t.Fatal("Run accepted an empty inventory")
	}
}
