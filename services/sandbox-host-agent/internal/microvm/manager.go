// Package microvm defines the narrow, fakeable privileged lifecycle boundary.
package microvm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var ErrColdBootForbidden = errors.New("cold boot is forbidden during restore")

type RestoreRequest struct {
	SandboxID, JailPath, MemoryPath, VMStatePath string
	MutableDrivePaths                            map[string]string
}
type SnapshotRequest struct{ SandboxID, MemoryPath, VMStatePath string }

type Manager interface {
	Pause(context.Context, string) error
	Resume(context.Context, string) error
	CreateFullSnapshot(context.Context, SnapshotRequest) error
	LoadSnapshot(context.Context, RestoreRequest) error
	Destroy(context.Context, string) error
}

// Restore only invokes LoadSnapshot. Its shape prevents a caller from falling
// back to boot-source creation when any snapshot input is missing.
func Restore(ctx context.Context, manager Manager, req RestoreRequest) error {
	if req.SandboxID == "" || req.MemoryPath == "" || req.VMStatePath == "" || len(req.MutableDrivePaths) == 0 {
		return ErrColdBootForbidden
	}
	return manager.LoadSnapshot(ctx, req)
}

type Runtime struct {
	SandboxID, JailPath, APISocket string
	Jailer                         *ProcessJailer
}

// LinuxManager owns runtime handles. A runtime is registered only after a
// successful jailer start; every error is returned to the command caller.
type LinuxManager struct {
	mu       sync.Mutex
	runtimes map[string]*Runtime
}

func NewLinuxManager() *LinuxManager { return &LinuxManager{runtimes: map[string]*Runtime{}} }
func (m *LinuxManager) Register(r *Runtime) error {
	if r == nil || r.SandboxID == "" || r.JailPath == "" || r.APISocket == "" || r.Jailer == nil {
		return errors.New("incomplete microVM runtime")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.runtimes[r.SandboxID]; exists {
		return fmt.Errorf("sandbox runtime already exists")
	}
	m.runtimes[r.SandboxID] = r
	return nil
}

// Start registers an already prepared runtime only after jailer startup
// succeeds. Callers must provision cgroups, disks, and networking before this
// point; this method never creates a restore boot fallback.
func (m *LinuxManager) Start(ctx context.Context, r *Runtime, spec JailerSpec) error {
	if r == nil || r.Jailer == nil {
		return errors.New("incomplete microVM runtime")
	}
	if err := r.Jailer.Start(ctx, spec); err != nil {
		return err
	}
	if err := m.Register(r); err != nil {
		_ = r.Jailer.Stop()
		return err
	}
	return nil
}
func (m *LinuxManager) runtime(id string) (*Runtime, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.runtimes[id]
	if r == nil {
		return nil, fmt.Errorf("sandbox runtime %q not found", id)
	}
	return r, nil
}
func (m *LinuxManager) Pause(ctx context.Context, id string) error {
	r, err := m.runtime(id)
	if err != nil {
		return err
	}
	return (FirecrackerClient{APISocket: r.APISocket}).Pause(ctx)
}
func (m *LinuxManager) Resume(ctx context.Context, id string) error {
	r, err := m.runtime(id)
	if err != nil {
		return err
	}
	return (FirecrackerClient{APISocket: r.APISocket}).Resume(ctx)
}
func (m *LinuxManager) CreateFullSnapshot(ctx context.Context, req SnapshotRequest) error {
	r, err := m.runtime(req.SandboxID)
	if err != nil {
		return err
	}
	if err := validateOwnedPath(r.JailPath, req.MemoryPath); err != nil {
		return err
	}
	if err := validateOwnedPath(r.JailPath, req.VMStatePath); err != nil {
		return err
	}
	if err := (FirecrackerClient{APISocket: r.APISocket}).CreateFullSnapshot(ctx, req.MemoryPath, req.VMStatePath); err != nil {
		return err
	}
	return syncFile(req.MemoryPath, req.VMStatePath)
}
func (m *LinuxManager) LoadSnapshot(ctx context.Context, req RestoreRequest) error {
	r, err := m.runtime(req.SandboxID)
	if err != nil {
		return err
	}
	if req.JailPath != r.JailPath {
		return errors.New("restore jail path does not match runtime")
	}
	for _, path := range append([]string{req.MemoryPath, req.VMStatePath}, mapPaths(req.MutableDrivePaths)...) {
		if err := validateOwnedPath(r.JailPath, path); err != nil {
			return err
		}
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("restore artifact unavailable: %w", err)
		}
	}
	return (FirecrackerClient{APISocket: r.APISocket}).LoadSnapshot(ctx, req.MemoryPath, req.VMStatePath)
}
func (m *LinuxManager) Destroy(_ context.Context, id string) error {
	m.mu.Lock()
	r := m.runtimes[id]
	delete(m.runtimes, id)
	m.mu.Unlock()
	if r == nil {
		return nil
	}
	var errs []error
	if err := r.Jailer.Stop(); err != nil {
		errs = append(errs, err)
	}
	if err := os.RemoveAll(r.JailPath); err != nil {
		errs = append(errs, fmt.Errorf("remove jail: %w", err))
	}
	return errors.Join(errs...)
}
func validateOwnedPath(root, path string) error {
	if path == "" {
		return errors.New("artifact path is required")
	}
	if !filepath.IsAbs(root) || !filepath.IsAbs(path) {
		return errors.New("artifact and jail paths must be absolute")
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || len(rel) >= 3 && rel[:3] == ".."+string(os.PathSeparator) {
		return fmt.Errorf("artifact path escapes jail")
	}
	return nil
}
func syncFile(paths ...string) error {
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		err = f.Sync()
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
func mapPaths(paths map[string]string) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		out = append(out, path)
	}
	return out
}
