package microvm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type JailerSpec struct {
	UID, GID                                 int
	ChrootBase, CgroupPath, NetworkNamespace string
	SeccompEnabled                           bool
}

type Jailer interface {
	Start(context.Context, JailerSpec) error
}

func ValidateJailerSpec(s JailerSpec) error {
	if s.UID <= 0 || s.GID <= 0 || s.ChrootBase == "" || s.CgroupPath == "" || s.NetworkNamespace == "" || !s.SeccompEnabled {
		return errors.New("jailer must use unprivileged identity, chroot, cgroup, network namespace, and seccomp")
	}
	return nil
}
func StartJailed(ctx context.Context, jailer Jailer, spec JailerSpec) error {
	if err := ValidateJailerSpec(spec); err != nil {
		return err
	}
	if err := jailer.Start(ctx, spec); err != nil {
		return fmt.Errorf("start jailed Firecracker: %w", err)
	}
	return nil
}

// ProcessJailer starts Firecracker only via jailer. It retains the process so
// cleanup can terminate the exact VMM, never a body-supplied PID.
type ProcessJailer struct {
	Binary, FirecrackerBinary, ID, ChrootBase, CgroupVersion, NetNS string
	UID, GID                                                        int
	ExtraArgs                                                       []string
	cmd                                                             *exec.Cmd
}

func (j *ProcessJailer) Start(ctx context.Context, spec JailerSpec) error {
	if err := ValidateJailerSpec(spec); err != nil {
		return err
	}
	if j.Binary == "" || j.FirecrackerBinary == "" || j.ID == "" {
		return errors.New("jailer binary, Firecracker binary, and sandbox ID are required")
	}
	if _, err := os.Stat(j.Binary); err != nil {
		return fmt.Errorf("jailer unavailable: %w", err)
	}
	if _, err := os.Stat(j.FirecrackerBinary); err != nil {
		return fmt.Errorf("Firecracker unavailable: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(j.ChrootBase, j.ID), 0700); err != nil {
		return fmt.Errorf("create jail directory: %w", err)
	}
	args := []string{"--id", j.ID, "--exec-file", j.FirecrackerBinary, "--uid", fmt.Sprint(spec.UID), "--gid", fmt.Sprint(spec.GID), "--chroot-base-dir", spec.ChrootBase, "--cgroup-version", "2", "--netns", spec.NetworkNamespace}
	if spec.SeccompEnabled {
		args = append(args, "--seccomp-level", "2")
	}
	args = append(args, "--", "--api-sock", "run/firecracker.sock")
	args = append(args, j.ExtraArgs...)
	j.cmd = exec.CommandContext(ctx, j.Binary, args...)
	if err := j.cmd.Start(); err != nil {
		j.cmd = nil
		return fmt.Errorf("start jailer: %w", err)
	}
	return nil
}
func (j *ProcessJailer) Stop() error {
	if j.cmd == nil || j.cmd.Process == nil {
		return nil
	}
	err := j.cmd.Process.Kill()
	_, _ = j.cmd.Process.Wait()
	j.cmd = nil
	if err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}
func (j *ProcessJailer) PID() int {
	if j.cmd == nil || j.cmd.Process == nil {
		return 0
	}
	return j.cmd.Process.Pid
}
