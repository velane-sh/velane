package network

import (
	"context"
	"strings"
	"testing"
)

type recordingRunner struct{ calls [][]string }

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) error {
	r.calls = append(r.calls, append([]string{name}, args...))
	return nil
}
func TestDefaultDenyPrecedesLeaseOpen(t *testing.T) {
	r := &recordingRunner{}
	n := NFTables{Runner: r}
	if err := n.DefaultDeny(context.Background(), "sandbox_1"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.calls[len(r.calls)-1], " "); !strings.Contains(got, "policy drop") || !strings.Contains(got, "netns exec velane-sandbox_1") {
		t.Fatalf("default deny hook missing: %s", got)
	}
	if err := n.EnableLease(context.Background(), "sandbox_1"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.calls[len(r.calls)-1], " "); !strings.Contains(got, "policy accept") {
		t.Fatalf("lease open missing: %s", got)
	}
}
func TestRejectsUnsafeSandboxID(t *testing.T) {
	if err := (NFTables{}).DefaultDeny(context.Background(), "../../x"); err == nil {
		t.Fatal("accepted unsafe chain name")
	}
}
