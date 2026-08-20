package resources

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

type Cgroup struct{ Root string }
type CgroupLimits struct {
	CPUQuotaMicros, CPUPeriodMicros, MemoryMaxBytes, PidsMax int64
	IOMax                                                    string
}

func (c Cgroup) Create(sandboxID string, limits CgroupLimits) (string, error) {
	if sandboxID == "" || limits.CPUQuotaMicros <= 0 || limits.CPUPeriodMicros <= 0 || limits.MemoryMaxBytes <= 0 || limits.PidsMax <= 0 {
		return "", errors.New("invalid cgroup limits")
	}
	root := c.Root
	if root == "" {
		root = "/sys/fs/cgroup/velane-sandboxes"
	}
	if _, err := os.Stat("/sys/fs/cgroup/cgroup.controllers"); err != nil {
		return "", fmt.Errorf("cgroup v2 unavailable: %w", err)
	}
	path := filepath.Join(root, sandboxID)
	if err := os.MkdirAll(path, 0750); err != nil {
		return "", fmt.Errorf("create sandbox cgroup: %w", err)
	}
	values := map[string]string{
		"cpu.max":    strconv.FormatInt(limits.CPUQuotaMicros, 10) + " " + strconv.FormatInt(limits.CPUPeriodMicros, 10),
		"memory.max": strconv.FormatInt(limits.MemoryMaxBytes, 10), "memory.swap.max": "0", "pids.max": strconv.FormatInt(limits.PidsMax, 10),
	}
	if limits.IOMax != "" {
		values["io.max"] = limits.IOMax
	}
	for name, value := range values {
		if err := os.WriteFile(filepath.Join(path, name), []byte(value), 0644); err != nil {
			_ = os.RemoveAll(path)
			return "", fmt.Errorf("set %s: %w", name, err)
		}
	}
	return path, nil
}
func (c Cgroup) Remove(path string) error {
	if path == "" {
		return nil
	}
	return os.Remove(path)
}
