package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	JournalPath, FirecrackerBinary, JailerBinary                     string
	ControlURL, TLSServerName, CAFile, ClientCertFile, ClientKeyFile string
	HostID, PoolID, BootID, HostCompatibilityKey                     string
	WatchdogSocket                                                   string
	StagingPath, ProtocolVersion, AgentVersion                       string
	RuntimeRoot, DiskRoot, CgroupRoot, IPBinary                      string
	JailerUID, JailerGID                                             int
	PollWaitSeconds                                                  int
}

func FromEnv() Config {
	return Config{
		JournalPath: os.Getenv("SANDBOX_AGENT_JOURNAL"), FirecrackerBinary: os.Getenv("FIRECRACKER_BINARY"), JailerBinary: os.Getenv("JAILER_BINARY"), ControlURL: os.Getenv("VELANE_SANDBOX_HOST_CONTROL_URL"), TLSServerName: os.Getenv("VELANE_SANDBOX_HOST_CONTROL_TLS_SERVER_NAME"), CAFile: os.Getenv("VELANE_SANDBOX_HOST_CONTROL_CA_FILE"), ClientCertFile: os.Getenv("VELANE_SANDBOX_HOST_CLIENT_CERT_FILE"), ClientKeyFile: os.Getenv("VELANE_SANDBOX_HOST_CLIENT_KEY_FILE"), HostID: os.Getenv("VELANE_SANDBOX_HOST_ID"), PoolID: os.Getenv("VELANE_SANDBOX_HOST_POOL_ID"), BootID: os.Getenv("VELANE_SANDBOX_HOST_BOOT_ID"), HostCompatibilityKey: os.Getenv("VELANE_SANDBOX_HOST_COMPATIBILITY_KEY"), WatchdogSocket: os.Getenv("SANDBOX_WATCHDOG_CONTROL_SOCKET"), StagingPath: os.Getenv("SANDBOX_AGENT_STAGING_PATH"), ProtocolVersion: os.Getenv("VELANE_SANDBOX_HOST_PROTOCOL_VERSION"), AgentVersion: os.Getenv("VELANE_SANDBOX_HOST_AGENT_VERSION"), RuntimeRoot: envOr("SANDBOX_AGENT_RUNTIME_ROOT", "/var/lib/velane-sandbox-agent/jails"), DiskRoot: envOr("SANDBOX_AGENT_DISK_ROOT", "/var/lib/velane-sandbox-agent/disks"), CgroupRoot: envOr("SANDBOX_AGENT_CGROUP_ROOT", "/sys/fs/cgroup/velane-sandboxes"), IPBinary: envOr("SANDBOX_AGENT_IP_BINARY", "ip"), JailerUID: envInt("SANDBOX_AGENT_JAILER_UID"), JailerGID: envInt("SANDBOX_AGENT_JAILER_GID"), PollWaitSeconds: 25,
	}
}
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func envInt(key string) int { v, _ := strconv.Atoi(os.Getenv(key)); return v }
func (c Config) Validate() error {
	if c.JournalPath == "" || c.FirecrackerBinary == "" || c.JailerBinary == "" || c.RuntimeRoot == "" || c.DiskRoot == "" || c.CgroupRoot == "" || c.IPBinary == "" || c.JailerUID <= 0 || c.JailerGID <= 0 {
		return fmt.Errorf("journal, runtime, disk, cgroup, network paths, Firecracker/jailer binaries, and jailer UID/GID are required")
	}
	if !strings.HasPrefix(c.ControlURL, "https://") || c.TLSServerName == "" || c.CAFile == "" || c.ClientCertFile == "" || c.ClientKeyFile == "" || c.PoolID == "" || c.BootID == "" || c.HostCompatibilityKey == "" || c.WatchdogSocket == "" || c.StagingPath == "" || c.ProtocolVersion == "" || c.AgentVersion == "" {
		return fmt.Errorf("HTTPS control URL, CA, local client certificate/key paths, pool ID, boot ID, compatibility key, watchdog socket, staging path, and version data are required")
	}
	if c.PollWaitSeconds < 1 || c.PollWaitSeconds > 25 {
		return fmt.Errorf("poll wait must be 1-25 seconds")
	}
	for _, binary := range []string{c.FirecrackerBinary, c.JailerBinary, c.IPBinary} {
		info, err := os.Stat(binary)
		if err != nil || info.Mode()&0111 == 0 {
			return fmt.Errorf("required executable %q is unavailable", binary)
		}
	}
	if _, err := os.Stat("/dev/kvm"); err != nil {
		return fmt.Errorf("KVM is unavailable: %w", err)
	}
	if _, err := os.Stat(filepath.Join("/sys/fs/cgroup", "cgroup.controllers")); err != nil {
		return fmt.Errorf("cgroup v2 is unavailable: %w", err)
	}
	return nil
}
