package controlplane

import "fmt"

// A host may safely enroll before optional lifecycle backends are installed.
// Placement and mutation admission inspect each command capability separately.
var requiredCommandCapabilities = []string{"Destroy"}

// HostInventory is measured by the agent before it advertises itself ready.
// Values must describe usable host resources, not requested capacity.
type HostInventory struct {
	TotalVCPU, TotalMemoryMB          int
	TotalDiskBytes, TotalStagingBytes int64
	CommandCapabilities               map[string]bool
	ProtocolVersion, AgentVersion     string
}

func (i HostInventory) Validate() error {
	if i.TotalVCPU <= 0 || i.TotalMemoryMB <= 0 || i.TotalDiskBytes <= 0 || i.TotalStagingBytes <= 0 || i.ProtocolVersion == "" || i.AgentVersion == "" {
		return fmt.Errorf("inventory must contain non-zero resources and protocol/agent versions")
	}
	for _, command := range requiredCommandCapabilities {
		if !i.CommandCapabilities[command] {
			return fmt.Errorf("required command capability %q is unavailable", command)
		}
	}
	return nil
}
