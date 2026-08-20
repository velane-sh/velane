package reconcile

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestJournalPersistsFenceAndRejectsStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.json")
	j, err := OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	first := CommandFence{SandboxID: "s", AllocationID: "a", Incarnation: 1, FenceEpoch: 2, Sequence: 1, CommandID: "one"}
	if err := j.Accept(first); err != nil {
		t.Fatal(err)
	}
	j, err = OpenJournal(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Accept(CommandFence{SandboxID: "s", AllocationID: "a", Incarnation: 1, FenceEpoch: 1, CommandID: "old"}); !errors.Is(err, ErrStaleFence) {
		t.Fatalf("got %v", err)
	}
	if err := j.Accept(CommandFence{SandboxID: "s", AllocationID: "a", Incarnation: 1, FenceEpoch: 2, Sequence: 2, CommandID: "next"}); err != nil {
		t.Fatalf("newer command sequence in current fence rejected: %v", err)
	}
}
