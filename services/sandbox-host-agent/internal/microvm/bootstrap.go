package microvm

import (
	"context"
	"errors"
	"fmt"
)

type DriveConfig struct {
	ID, PathOnHost string
	Root, ReadOnly bool
}
type InitialConfig struct {
	VCPUCount, MemoryMB  int
	SMT                  bool
	KernelPath, BootArgs string
	Drives               []DriveConfig
}

// ConfigureInitial is the sole cold-boot configuration path. It is separate
// from LoadSnapshot so strict restore cannot accidentally reuse it.
func (m *LinuxManager) ConfigureInitial(ctx context.Context, sandboxID string, cfg InitialConfig) error {
	if cfg.VCPUCount <= 0 || cfg.MemoryMB <= 0 || cfg.KernelPath == "" || len(cfg.Drives) == 0 {
		return errors.New("initial Firecracker machine, kernel, and drives are required")
	}
	r, err := m.runtime(sandboxID)
	if err != nil {
		return err
	}
	fc := FirecrackerClient{APISocket: r.APISocket}
	if err := fc.ConfigureMachine(ctx, cfg.VCPUCount, cfg.MemoryMB, cfg.SMT); err != nil {
		return err
	}
	if err := fc.ConfigureBootSource(ctx, cfg.KernelPath, cfg.BootArgs); err != nil {
		return err
	}
	rootCount := 0
	for _, drive := range cfg.Drives {
		if drive.ID == "" || drive.PathOnHost == "" {
			return errors.New("initial Firecracker drive is incomplete")
		}
		if drive.Root {
			rootCount++
		}
		if err := fc.ConfigureDrive(ctx, drive); err != nil {
			return fmt.Errorf("configure Firecracker drive %q: %w", drive.ID, err)
		}
	}
	if rootCount != 1 {
		return errors.New("initial Firecracker configuration requires exactly one root drive")
	}
	return fc.StartInstance(ctx)
}
