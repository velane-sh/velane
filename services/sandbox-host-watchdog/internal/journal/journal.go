// Package journal owns root-only persisted watchdog lease state. It never
// persists process-relative monotonic timestamps because they are meaningless
// after a watchdog restart.
package journal

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/abskrj/velane/services/sandbox-host-watchdog/internal/lease"
)

type Entry struct {
	Grant                 lease.Grant `json:"grant"`
	AcceptedWallUnixMilli int64       `json:"accepted_wall_unix_milli"`
	AcceptedBootID        string      `json:"accepted_boot_id"`
}

type Store struct {
	path    string
	mu      sync.Mutex
	entries map[string]Entry
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, entries: map[string]Entry{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(b, &s.entries); err != nil {
		return nil, err
	}
	if s.entries == nil {
		s.entries = map[string]Entry{}
	}
	return s, nil
}
func (s *Store) Put(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[e.Grant.SandboxID] = e
	return s.persist()
}
func (s *Store) Entries() map[string]Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]Entry, len(s.entries))
	for k, v := range s.entries {
		out[k] = v
	}
	return out
}
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, id)
	return s.persist()
}
func (s *Store) persist() error {
	b, err := json.Marshal(s.entries)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err = os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
