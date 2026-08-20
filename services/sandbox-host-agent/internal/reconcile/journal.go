// Package reconcile persists command fencing before privileged side effects.
package reconcile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var ErrStaleFence = errors.New("stale or conflicting command fence")

type CommandFence struct {
	SandboxID    string `json:"sandbox_id"`
	AllocationID string `json:"allocation_id"`
	Incarnation  uint64 `json:"incarnation"`
	FenceEpoch   uint64 `json:"fence_epoch"`
	Sequence     uint64 `json:"sequence"`
	CommandID    string `json:"command_id"`
}

type journalState struct {
	Commands map[string]CommandFence `json:"commands"`
}

type Journal struct {
	path  string
	mu    sync.Mutex
	state journalState
}

func OpenJournal(path string) (*Journal, error) {
	j := &Journal{path: path, state: journalState{Commands: map[string]CommandFence{}}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return j, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &j.state); err != nil {
		return nil, fmt.Errorf("decode host command journal: %w", err)
	}
	if j.state.Commands == nil {
		j.state.Commands = map[string]CommandFence{}
	}
	return j, nil
}

// Accept atomically records the fence before the caller performs any VM action.
// Replayed IDs are safe; a newer epoch supersedes older work only.
func (j *Journal) Accept(c CommandFence) error {
	if c.SandboxID == "" || c.AllocationID == "" || c.CommandID == "" {
		return errors.New("incomplete command fence")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	old, found := j.state.Commands[c.SandboxID]
	if found {
		if old.CommandID == c.CommandID && old.AllocationID == c.AllocationID && old.Incarnation == c.Incarnation && old.FenceEpoch == c.FenceEpoch && old.Sequence == c.Sequence {
			return nil
		}
		if c.Incarnation < old.Incarnation || (c.Incarnation == old.Incarnation && c.FenceEpoch < old.FenceEpoch) || (c.Incarnation == old.Incarnation && c.FenceEpoch == old.FenceEpoch && c.Sequence <= old.Sequence) {
			return ErrStaleFence
		}
	}
	j.state.Commands[c.SandboxID] = c
	return j.persistLocked()
}
func (j *Journal) persistLocked() error {
	b, err := json.Marshal(j.state)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(j.path), 0700); err != nil {
		return err
	}
	tmp := j.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, j.path)
}
