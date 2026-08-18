// Package inventory measures the host's usable capacity for authenticated
// registration. It intentionally refuses unavailable/unknown resources.
package inventory

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/abskrj/velane/services/sandbox-host-agent/internal/controlplane"
)

func Collect(stagingPath, protocolVersion, agentVersion string) (controlplane.HostInventory, error) {
	if stagingPath == "" {
		return controlplane.HostInventory{}, fmt.Errorf("staging path is required")
	}
	memoryMB, err := memoryMB()
	if err != nil {
		return controlplane.HostInventory{}, err
	}
	var filesystem syscall.Statfs_t
	if err := syscall.Statfs(stagingPath, &filesystem); err != nil {
		return controlplane.HostInventory{}, fmt.Errorf("stat staging filesystem: %w", err)
	}
	stagingBytes := int64(filesystem.Bavail) * int64(filesystem.Bsize)
	if stagingBytes <= 0 {
		return controlplane.HostInventory{}, fmt.Errorf("staging filesystem has no usable capacity")
	}
	return controlplane.HostInventory{TotalVCPU: runtime.NumCPU(), TotalMemoryMB: memoryMB, TotalDiskBytes: stagingBytes, TotalStagingBytes: stagingBytes, ProtocolVersion: protocolVersion, AgentVersion: agentVersion, CommandCapabilities: map[string]bool{}}, nil
}

func memoryMB() (int, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kib, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil || kib <= 0 {
				break
			}
			return int(kib / 1024), nil
		}
	}
	return 0, fmt.Errorf("read MemTotal from /proc/meminfo")
}
