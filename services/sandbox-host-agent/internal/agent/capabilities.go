package agent

import (
	"os"

	"github.com/abskrj/velane/services/sandbox-host-agent/internal/config"
)

// DetectedCapabilities reports only drivers whose host prerequisites are
// currently observable. Registration must not advertise work that a rebooted
// or partially configured host cannot execute.
func DetectedCapabilities(cfg config.Config) map[string]bool {
	ready := hostPrerequisitesPresent(cfg)
	// Initial boot additionally requires an authenticated, content-addressed
	// artifact staging implementation. Local digest-derived paths alone do not
	// prove ObjectRef/ObjectVersion contents, so create stays fail-closed.
	createReady := false
	// The executable advertises this only when bootstrap installs the
	// control-plane key-wrapper-backed uploader. The bare host binary has no
	// cloud credentials and must remain unavailable rather than fake success.
	restoreReady := false
	return map[string]bool{
		"CreateSandbox":       createReady,
		"CreateFullSnapshot":  false,
		"RestoreFullSnapshot": restoreReady,
		"Destroy":             ready,
	}
}

func hostPrerequisitesPresent(cfg config.Config) bool {
	if cfg.RuntimeRoot == "" || cfg.DiskRoot == "" || cfg.CgroupRoot == "" || cfg.WatchdogSocket == "" || cfg.JailerUID <= 0 || cfg.JailerGID <= 0 {
		return false
	}
	for _, binary := range []string{cfg.FirecrackerBinary, cfg.JailerBinary, cfg.IPBinary} {
		info, err := os.Stat(binary)
		if err != nil || info.Mode()&0111 == 0 {
			return false
		}
	}
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return false
	}
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		return false
	}
	if _, err := os.Stat(cfg.WatchdogSocket); err != nil {
		return false
	}
	return true
}
