package microvm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

type Fencer interface {
	Pause(context.Context, string) error
	Kill(context.Context, string) error
}

// ProcessFencer only trusts root-owned PID files created during runtime setup.
// A missing or malformed handle fails closed instead of reporting a false fence.
type ProcessFencer struct{ PIDDir string }

func (f ProcessFencer) pid(sandboxID string) (int, error) {
	if sandboxID == "" || filepath.Base(sandboxID) != sandboxID {
		return 0, errors.New("invalid sandbox ID")
	}
	root := f.PIDDir
	if root == "" {
		root = "/run/velane-sandbox-watchdog/pids"
	}
	b, err := os.ReadFile(filepath.Join(root, sandboxID+".pid"))
	if err != nil {
		return 0, fmt.Errorf("read sandbox fence handle: %w", err)
	}
	pid, err := strconv.Atoi(string(b))
	if err != nil || pid <= 1 {
		return 0, errors.New("invalid sandbox fence handle")
	}
	return pid, nil
}
func (f ProcessFencer) Pause(_ context.Context, sandboxID string) error {
	pid, err := f.pid(sandboxID)
	if err != nil {
		return err
	}
	return syscall.Kill(pid, syscall.SIGSTOP)
}
func (f ProcessFencer) Kill(_ context.Context, sandboxID string) error {
	pid, err := f.pid(sandboxID)
	if err != nil {
		return err
	}
	err = syscall.Kill(pid, syscall.SIGKILL)
	if err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}
