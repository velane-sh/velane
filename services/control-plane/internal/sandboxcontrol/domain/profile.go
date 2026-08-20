package domain

import (
	"encoding/json"
	"fmt"
)

type SandboxProfileVersion struct {
	ID                      string          `json:"id"`
	ProfileFamily           string          `json:"profile_family"`
	Name                    string          `json:"name"`
	Version                 string          `json:"version"`
	VCPU                    int             `json:"vcpu"`
	MemoryMB                int             `json:"memory_mb"`
	MutableDiskLayout       json.RawMessage `json:"mutable_disk_layout"`
	StagingOverheadBytes    int64           `json:"staging_overhead_bytes"`
	RuntimeOverhead         json.RawMessage `json:"runtime_overhead"`
	MachineConfig           json.RawMessage `json:"machine_config"`
	DeviceTopology          json.RawMessage `json:"device_topology"`
	ArchitectureConstraints json.RawMessage `json:"architecture_constraints"`
	Status                  string          `json:"status"`
	CanonicalDocument       json.RawMessage `json:"canonical_document"`
	DocumentDigest          string          `json:"document_digest"`
}

func (p SandboxProfileVersion) CanonicalDigest() (string, error) {
	if len(p.CanonicalDocument) > 0 {
		var v any
		if err := json.Unmarshal(p.CanonicalDocument, &v); err != nil {
			return "", err
		}
		return CanonicalDigest(v)
	}
	return CanonicalDigest(struct {
		Family, Version          string
		VCPU, Memory             int
		Disks, Machine, Topology json.RawMessage
	}{p.ProfileFamily, p.Version, p.VCPU, p.MemoryMB, p.MutableDiskLayout, p.MachineConfig, p.DeviceTopology})
}
func ResolveVMRestoreDescriptor(recipe RecipeSpecV1, profile SandboxProfileVersion, guestKernelDigest, guestInitDigest, rootfsDigest string) (VMRestoreDescriptor, error) {
	if profile.Status != "active" {
		return VMRestoreDescriptor{}, fmt.Errorf("profile is not active")
	}
	d, err := profile.CanonicalDigest()
	if err != nil {
		return VMRestoreDescriptor{}, err
	}
	return VMRestoreDescriptor{SchemaVersion: "1", GuestKernelDigest: guestKernelDigest, GuestInitDigest: guestInitDigest, RootfsDigest: rootfsDigest, ProfileDocumentDigest: d, MachineConfig: profile.MachineConfig, DeviceTopology: profile.DeviceTopology, MutableDrives: profile.MutableDiskLayout, GuestProtocol: recipe.GuestProtocol}, nil
}
