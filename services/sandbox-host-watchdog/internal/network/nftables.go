package network

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
)

// Controller owns a watchdog-only nftables table in each sandbox network
// namespace. The hook chain applies to real packet flow, rather than being an
// unattached bookkeeping chain in the host namespace.
type Controller interface {
	DefaultDeny(context.Context, string) error
	EnableLease(context.Context, string) error
	Remove(context.Context, string) error
}
type Runner interface {
	Run(context.Context, string, ...string) error
}
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}

type NFTables struct {
	Runner                                           Runner
	Binary, Table, Family, IPBinary, NamespacePrefix string
}

func (n NFTables) normalized() (NFTables, error) {
	if n.Runner == nil {
		n.Runner = ExecRunner{}
	}
	if n.Binary == "" {
		n.Binary = "nft"
	}
	if n.IPBinary == "" {
		n.IPBinary = "ip"
	}
	if n.Table == "" {
		n.Table = "velane_sandbox_watchdog"
	}
	if n.Family == "" {
		n.Family = "inet"
	}
	if n.NamespacePrefix == "" {
		n.NamespacePrefix = "velane-"
	}
	if !validName(n.Table) || !validName(n.NamespacePrefix) {
		return NFTables{}, errors.New("invalid nftables table or namespace prefix")
	}
	return n, nil
}
func (n NFTables) run(ctx context.Context, sandboxID string, args ...string) error {
	if !validName(sandboxID) {
		return errors.New("invalid sandbox ID for nftables chain")
	}
	full := append([]string{"netns", "exec", n.NamespacePrefix + sandboxID, n.Binary}, args...)
	return n.Runner.Run(ctx, n.IPBinary, full...)
}
func (n NFTables) replacePolicy(ctx context.Context, sandboxID, policy string) error {
	if err := n.run(ctx, sandboxID, "-exist", "add", "table", n.Family, n.Table); err != nil {
		return fmt.Errorf("create watchdog nft table: %w", err)
	}
	// Delete/recreate removes every previous lease accept rule before installing
	// the hook chain. The netns is exclusive to one sandbox, so policy applies
	// only to that sandbox's packet path.
	_ = n.run(ctx, sandboxID, "delete", "chain", n.Family, n.Table, "watchdog")
	if err := n.run(ctx, sandboxID, "add", "chain", n.Family, n.Table, "watchdog", "{", "type", "filter", "hook", "forward", "priority", "filter;", "policy", policy+";", "}"); err != nil {
		return fmt.Errorf("install watchdog %s policy: %w", policy, err)
	}
	return nil
}
func (n NFTables) DefaultDeny(ctx context.Context, sandboxID string) error {
	n, err := n.normalized()
	if err != nil {
		return err
	}
	return n.replacePolicy(ctx, sandboxID, "drop")
}
func (n NFTables) EnableLease(ctx context.Context, sandboxID string) error {
	n, err := n.normalized()
	if err != nil {
		return err
	}
	return n.replacePolicy(ctx, sandboxID, "accept")
}
func (n NFTables) Remove(ctx context.Context, sandboxID string) error {
	n, err := n.normalized()
	if err != nil {
		return err
	}
	if err := n.run(ctx, sandboxID, "delete", "table", n.Family, n.Table); err != nil {
		return fmt.Errorf("remove sandbox nft table: %w", err)
	}
	return nil
}

var nftName = regexp.MustCompile(`^[A-Za-z0-9_-]{1,48}$`)

func validName(v string) bool { return nftName.MatchString(v) }
