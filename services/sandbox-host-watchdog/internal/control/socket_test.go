package control

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/abskrj/velane/services/sandbox-host-watchdog/internal/lease"
)

type acceptor struct{ accepted bool }

func (a *acceptor) Accept(_ context.Context, _ lease.Grant) error { a.accepted = true; return nil }
func TestSocketDeliversGrant(t *testing.T) {
	path := filepath.Join(t.TempDir(), "control.sock")
	a := &acceptor{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = (Server{Path: path, Acceptor: a}).Listen(ctx) }()
	deadline := time.Now().Add(time.Second)
	for {
		err := (Client{Path: path}).Deliver(context.Background(), lease.Grant{})
		if err == nil || a.accepted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	if !a.accepted {
		t.Fatal("grant was not delivered")
	}
}
