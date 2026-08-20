package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type HostCompatibilityDescriptor struct {
	SchemaVersion string            `json:"schema_version"`
	Provider      string            `json:"provider"`
	Architecture  string            `json:"architecture"`
	Attributes    map[string]string `json:"attributes,omitempty"`
}
type VMRestoreDescriptor struct {
	SchemaVersion         string          `json:"schema_version"`
	GuestKernelDigest     string          `json:"guest_kernel_digest"`
	GuestInitDigest       string          `json:"guest_init_digest"`
	RootfsDigest          string          `json:"rootfs_digest"`
	ProfileDocumentDigest string          `json:"profile_document_digest"`
	MachineConfig         json.RawMessage `json:"machine_config"`
	DeviceTopology        json.RawMessage `json:"device_topology"`
	MutableDrives         json.RawMessage `json:"mutable_drives"`
	GuestProtocol         string          `json:"guest_protocol"`
}
type SnapshotCompatibilityDescriptor struct {
	SchemaVersion              string `json:"schema_version"`
	LineageID                  string `json:"lineage_id"`
	SourceHostCompatibilityKey string `json:"source_host_compatibility_key"`
	VMRestoreDescriptorDigest  string `json:"vm_restore_descriptor_digest"`
}
type Candidate struct {
	LineageID                 string
	HostCompatibilityKey      string
	VMRestoreDescriptorDigest string
	RestoreCapabilities       map[string]bool
}
type CandidateMatch struct {
	Compatible bool
	Reason     string
}

func CanonicalDigest(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
func MustCanonicalDigest(v any) string {
	d, err := CanonicalDigest(v)
	if err != nil {
		panic(err)
	}
	return d
}
func SnapshotCompatibilityDigest(d SnapshotCompatibilityDescriptor) (string, error) {
	if d.SchemaVersion == "" || d.LineageID == "" || d.SourceHostCompatibilityKey == "" || d.VMRestoreDescriptorDigest == "" {
		return "", fmt.Errorf("snapshot compatibility descriptor is incomplete")
	}
	return CanonicalDigest(d)
}
func EvaluateCandidate(candidate Candidate, snapshot SnapshotCompatibilityDescriptor) CandidateMatch {
	if candidate.LineageID != snapshot.LineageID {
		return CandidateMatch{Reason: "lineage_mismatch"}
	}
	if candidate.HostCompatibilityKey != snapshot.SourceHostCompatibilityKey {
		return CandidateMatch{Reason: "host_compatibility_mismatch"}
	}
	if candidate.VMRestoreDescriptorDigest != snapshot.VMRestoreDescriptorDigest {
		return CandidateMatch{Reason: "vm_restore_descriptor_mismatch"}
	}
	if candidate.RestoreCapabilities != nil && !candidate.RestoreCapabilities["RestoreFullSnapshot"] {
		return CandidateMatch{Reason: "capabilities_unavailable"}
	}
	return CandidateMatch{Compatible: true}
}
