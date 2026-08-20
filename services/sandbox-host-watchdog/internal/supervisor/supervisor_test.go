package supervisor

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"github.com/abskrj/velane/services/sandbox-host-watchdog/internal/journal"
	"github.com/abskrj/velane/services/sandbox-host-watchdog/internal/lease"
	"path/filepath"
	"testing"
	"time"
)

type fakeClock struct {
	wall time.Time
	boot string
}

func (f *fakeClock) Now() time.Time { return f.wall }
func (f *fakeClock) BootID() string { return f.boot }

type fakeNetwork struct{ enabled, denied, removed int }

func (f *fakeNetwork) DefaultDeny(context.Context, string) error { f.denied++; return nil }
func (f *fakeNetwork) EnableLease(context.Context, string) error { f.enabled++; return nil }
func (f *fakeNetwork) Remove(context.Context, string) error      { f.removed++; return nil }

type fakeFencer struct{ paused, killed int }

func (f *fakeFencer) Pause(context.Context, string) error { f.paused++; return nil }
func (f *fakeFencer) Kill(context.Context, string) error  { f.killed++; return nil }
func testSupervisor(t *testing.T) (*Supervisor, *fakeClock, *fakeNetwork, *fakeFencer, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	clock := &fakeClock{wall: time.Unix(1000, 0), boot: "boot-a"}
	store, err := journal.Open(filepath.Join(t.TempDir(), "leases.json"))
	if err != nil {
		t.Fatal(err)
	}
	net := &fakeNetwork{}
	f := &fakeFencer{}
	return &Supervisor{PublicKey: pub, Store: store, Network: net, Fencer: f, Clock: clock, MaxTTL: 60 * time.Second, SafetyMargin: 10 * time.Second}, clock, net, f, priv
}
func grant(t *testing.T, p ed25519.PrivateKey, now time.Time) lease.Grant {
	t.Helper()
	g, e := lease.Sign(lease.Grant{SandboxID: "s", AllocationID: "a", HostID: "h", HostIncarnation: 1, FenceEpoch: 1, IssuedAtUnixMilli: now.UnixMilli(), ExpiresAtUnixMilli: now.Add(40 * time.Second).UnixMilli()}, p)
	if e != nil {
		t.Fatal(e)
	}
	return g
}
func TestLeaseFencesAtSafetyDeadline(t *testing.T) {
	s, c, n, f, p := testSupervisor(t)
	if e := s.Accept(context.Background(), grant(t, p, c.wall)); e != nil {
		t.Fatal(e)
	}
	c.wall = c.wall.Add(30 * time.Second)
	if e := s.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	if n.denied != 1 || f.paused != 1 || f.killed != 0 {
		t.Fatalf("not fail closed: %+v %+v", n, f)
	}
}
func TestRestartAndBootChangeKillPersistedLease(t *testing.T) {
	s, c, n, f, p := testSupervisor(t)
	if e := s.Accept(context.Background(), grant(t, p, c.wall)); e != nil {
		t.Fatal(e)
	}
	if e := s.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	if n.denied != 1 || f.paused != 1 || f.killed != 1 || n.removed != 1 {
		t.Fatalf("restart remained live: %+v %+v", n, f)
	}
	c.wall = c.wall.Add(time.Second)
	if e := s.Accept(context.Background(), grant(t, p, c.wall)); e != nil {
		t.Fatal(e)
	}
	c.boot = "boot-b"
	if e := s.Tick(context.Background()); e != nil {
		t.Fatal(e)
	}
	if f.killed != 2 {
		t.Fatal("boot identity change did not kill")
	}
}
