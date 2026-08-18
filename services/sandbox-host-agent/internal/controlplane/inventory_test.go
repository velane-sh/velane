package controlplane

import "testing"

func TestHostInventoryRequiresResourcesAndCommands(t *testing.T) {
	valid := HostInventory{TotalVCPU: 1, TotalMemoryMB: 1, TotalDiskBytes: 1, TotalStagingBytes: 1, ProtocolVersion: "v1", AgentVersion: "test", CommandCapabilities: map[string]bool{"Destroy": true}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid inventory: %v", err)
	}
	valid.TotalDiskBytes = 0
	if err := valid.Validate(); err == nil {
		t.Fatal("zero disk capacity accepted")
	}
	valid.TotalDiskBytes = 1
	valid.CommandCapabilities["Destroy"] = false
	if err := valid.Validate(); err == nil {
		t.Fatal("missing Destroy capability accepted")
	}
}
