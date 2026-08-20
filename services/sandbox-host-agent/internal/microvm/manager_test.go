package microvm

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

type fakeManager struct{ loaded bool }

func (f *fakeManager) Pause(context.Context, string) error                       { return nil }
func (f *fakeManager) Resume(context.Context, string) error                      { return nil }
func (f *fakeManager) CreateFullSnapshot(context.Context, SnapshotRequest) error { return nil }
func (f *fakeManager) LoadSnapshot(context.Context, RestoreRequest) error {
	f.loaded = true
	return nil
}
func (f *fakeManager) Destroy(context.Context, string) error { return nil }
func TestRestoreRejectsIncompleteBundleWithoutLoad(t *testing.T) {
	f := &fakeManager{}
	if err := Restore(context.Background(), f, RestoreRequest{SandboxID: "s"}); !errors.Is(err, ErrColdBootForbidden) {
		t.Fatalf("got %v", err)
	}
	if f.loaded {
		t.Fatal("restore loaded an incomplete bundle")
	}
}
func TestValidateOwnedPathRejectsEscape(t *testing.T) {
	if err := validateOwnedPath("/jail/s", filepath.Clean("/jail/s/../other/memory")); err == nil {
		t.Fatal("accepted escaped artifact")
	}
}
