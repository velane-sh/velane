//go:build linux

package build

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/abskrj/velane/services/sandbox-image-builder/internal/recipe"
)

// LinuxTools provides the real privileged runner. It intentionally requires an
// explicit package installer command rather than guessing an ecosystem from an
// untrusted recipe. Commands receive only immutable JSON through stdin.
type LinuxTools struct {
	PackageInstallerCommand string
	MkfsExt4Binary          string
	UnshareBinary           string
}

type installerInput struct {
	Root               string           `json:"root"`
	RepositorySnapshot string           `json:"repository_snapshot"`
	IndexDigest        string           `json:"index_digest"`
	LockDigest         string           `json:"lock_digest"`
	Packages           []recipe.Package `json:"packages"`
}

func (t LinuxTools) CopyTree(source, destination string) error {
	if source == "" {
		return fmt.Errorf("base rootfs is required")
	}
	if _, err := os.Stat(source); err != nil {
		return err
	}
	if err := os.MkdirAll(destination, 0o750); err != nil {
		return err
	}
	cmd := exec.Command("cp", "-a", filepath.Clean(source)+"/.", destination)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("copy base rootfs: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func (t LinuxTools) Install(ctx context.Context, root string, group recipe.InstallGroup) error {
	if t.PackageInstallerCommand == "" {
		return fmt.Errorf("VELANE_SANDBOX_IMAGE_PACKAGE_INSTALLER is required")
	}
	input, err := json.Marshal(installerInput{Root: root, RepositorySnapshot: group.RepositorySnapshot, IndexDigest: group.IndexDigest, LockDigest: group.LockDigest, Packages: group.Packages})
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "/bin/sh", "-ceu", t.PackageInstallerCommand)
	cmd.Stdin = strings.NewReader(string(input))
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C", "TZ=UTC", "SOURCE_DATE_EPOCH=0"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Noctty: true, Setpgid: true}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("locked package installer: %w: %s", err, boundedOutput(output))
	}
	return nil
}

func (t LinuxTools) Run(ctx context.Context, root, script string, env map[string]string) error {
	unshare := t.UnshareBinary
	if unshare == "" {
		unshare = "unshare"
	}
	// --net removes every interface including loopback. The runner gives the
	// script no inherited credentials, proxy settings, or host mounts.
	cmd := exec.CommandContext(ctx, unshare, "--user", "--map-root-user", "--mount", "--pid", "--fork", "--kill-child", "--net", "--mount-proc", "--root", root, "--wd", "/", "/bin/sh", "-ceu", script)
	cmd.Dir = "/"
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/nonexistent", "LC_ALL=C", "TZ=UTC", "VELANE_BUILD_INPUT_DIR=/run/velane-build-inputs", "SOURCE_DATE_EPOCH=0"}
	for key, value := range env {
		if key != "VELANE_BUILD_INPUT_DIR" && key != "SOURCE_DATE_EPOCH" {
			return fmt.Errorf("unsupported bootstrap environment %q", key)
		}
		if strings.ContainsAny(key+value, "\x00\r\n") {
			return fmt.Errorf("invalid bootstrap environment %q", key)
		}
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Noctty: true, Setpgid: true}
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("network-disabled bootstrap: %w: %s", err, boundedOutput(output))
	}
	return nil
}

func (t LinuxTools) MakeExt4(ctx context.Context, sourceDir, output, uuid string) error {
	mkfs := t.MkfsExt4Binary
	if mkfs == "" {
		mkfs = "mkfs.ext4"
	}
	// mke2fs's directory population gives stable ordering after normalizeTree;
	// disable lazy initialization and set an explicit UUID/time-independent label.
	cmd := exec.CommandContext(ctx, mkfs, "-q", "-F", "-d", sourceDir, "-U", uuid, "-E", "lazy_itable_init=0,lazy_journal_init=0", "-L", "velane-rootfs", output, "256M")
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C", "TZ=UTC", "SOURCE_DATE_EPOCH=0"}
	if outputBytes, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mkfs.ext4: %w: %s", err, boundedOutput(outputBytes))
	}
	return nil
}

func boundedOutput(value []byte) string {
	text := strings.ReplaceAll(string(value), "\x1b", "")
	if len(text) > 16*1024 {
		text = text[:16*1024]
	}
	return strings.TrimSpace(text)
}
