//go:build linux

package quiesce

import (
	"context"
	"testing"
)

type fsFake struct{ synced, frozen, thawed []string }

func (f *fsFake) SyncFS(_ context.Context, p string) error {
	f.synced = append(f.synced, p)
	return nil
}
func (f *fsFake) Freeze(_ context.Context, p string) error {
	f.frozen = append(f.frozen, p)
	return nil
}
func (f *fsFake) Thaw(_ context.Context, p string) error { f.thawed = append(f.thawed, p); return nil }

type drainerFake struct{}

func (drainerFake) Drain(context.Context) error                      { return nil }
func (drainerFake) RevokeEphemeralCredentials(context.Context) error { return nil }
func TestQuiesceFreezesEveryWritableMountAndThaws(t *testing.T) {
	f := &fsFake{}
	c := Coordinator{Filesystem: f, Drainer: drainerFake{}}
	nonce, mounts, e := c.Quiesce(context.Background(), []Mount{{DriveID: "data", Path: "/data", Writable: true}, {DriveID: "root", Path: "/", Writable: true}})
	if e != nil || nonce == "" || len(mounts) != 2 {
		t.Fatalf("%q %+v %v", nonce, mounts, e)
	}
	if e := c.Thaw(context.Background(), mounts); e != nil {
		t.Fatal(e)
	}
	if len(f.frozen) != 2 || len(f.thawed) != 2 {
		t.Fatalf("%+v", f)
	}
}
