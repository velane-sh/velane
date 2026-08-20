package network

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
)

type CommandRunner interface {
	Run(context.Context, string, ...string) error
}
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

// LinuxManager creates the namespace/veth/tap path but leaves its policy
// default-denied. Only the independent root watchdog may open connectivity
// after it validates and persists a signed lease.
type LinuxManager struct {
	Runner                    CommandRunner
	IPBinary, NamespacePrefix string
}

func (m LinuxManager) normalized() LinuxManager {
	if m.Runner == nil {
		m.Runner = ExecRunner{}
	}
	if m.IPBinary == "" {
		m.IPBinary = "ip"
	}
	if m.NamespacePrefix == "" {
		m.NamespacePrefix = "velane-"
	}
	return m
}
func (m LinuxManager) CreateDefaultDeny(ctx context.Context, n SandboxNetwork) error {
	m = m.normalized()
	if n.SandboxID == "" || n.Namespace != m.NamespacePrefix+n.SandboxID || n.TapName == "" || n.HostVeth == "" {
		return errors.New("sandbox network identity is incomplete or non-canonical")
	}
	if err := m.Runner.Run(ctx, m.IPBinary, "netns", "add", n.Namespace); err != nil {
		return fmt.Errorf("create sandbox network namespace: %w", err)
	}
	cleanup := func() { _ = m.Runner.Run(context.Background(), m.IPBinary, "netns", "delete", n.Namespace) }
	if err := m.Runner.Run(ctx, m.IPBinary, "link", "add", n.HostVeth, "type", "veth", "peer", "name", n.TapName); err != nil {
		cleanup()
		return fmt.Errorf("create sandbox veth: %w", err)
	}
	if err := m.Runner.Run(ctx, m.IPBinary, "link", "set", n.TapName, "netns", n.Namespace); err != nil {
		cleanup()
		return fmt.Errorf("move sandbox veth to namespace: %w", err)
	}
	if err := m.Runner.Run(ctx, m.IPBinary, "netns", "exec", n.Namespace, m.IPBinary, "link", "set", "lo", "up"); err != nil {
		cleanup()
		return fmt.Errorf("initialize sandbox network namespace: %w", err)
	}
	if err := m.Runner.Run(ctx, m.IPBinary, "netns", "exec", n.Namespace, m.IPBinary, "link", "set", n.TapName, "up"); err != nil {
		cleanup()
		return fmt.Errorf("bring sandbox veth up: %w", err)
	}
	return nil
}
func (m LinuxManager) Remove(ctx context.Context, sandboxID string) error {
	m = m.normalized()
	if sandboxID == "" {
		return errors.New("sandbox ID is required")
	}
	return m.Runner.Run(ctx, m.IPBinary, "netns", "delete", m.NamespacePrefix+sandboxID)
}
