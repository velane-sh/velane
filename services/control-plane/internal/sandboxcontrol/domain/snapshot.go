package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type ChunkDescriptor struct {
	Index            int    `json:"index"`
	PlaintextSize    int64  `json:"plaintext_size"`
	CiphertextSize   int64  `json:"ciphertext_size"`
	PlaintextSHA256  string `json:"plaintext_sha256"`
	CiphertextSHA256 string `json:"ciphertext_sha256"`
	Nonce            string `json:"nonce"`
	ObjectRef        string `json:"object_ref"`
	ObjectVersion    string `json:"object_version"`
}
type SnapshotArtifact struct {
	Type        string            `json:"type"`
	DriveID     string            `json:"drive_id,omitempty"`
	LogicalSize int64             `json:"logical_size"`
	SHA256      string            `json:"sha256"`
	Chunks      []ChunkDescriptor `json:"chunks"`
}
type DriveDescriptor struct {
	ID              string `json:"id"`
	Mutable         bool   `json:"mutable"`
	ImmutableSHA256 string `json:"immutable_sha256,omitempty"`
}

// SnapshotManifestV1 is wire-identical to sandbox-host-agent/internal/snapshot.
type SnapshotManifestV1 struct {
	SchemaVersion              string             `json:"schema_version"`
	ManifestID                 string             `json:"manifest_id"`
	SandboxID                  string             `json:"sandbox_id"`
	Generation                 uint64             `json:"generation"`
	SnapshotMode               string             `json:"snapshot_mode"`
	FirecrackerSnapshotType    string             `json:"firecracker_snapshot_type"`
	LineageID                  string             `json:"lineage_id"`
	SourceHostCompatibilityKey string             `json:"source_host_compatibility_key"`
	VMRestoreDescriptorDigest  string             `json:"vm_restore_descriptor_digest"`
	SnapshotCompatibilityKey   string             `json:"snapshot_compatibility_key"`
	MachineTopologyDigest      string             `json:"machine_topology_digest"`
	DeviceTopologyDigest       string             `json:"device_topology_digest"`
	GuestImageDigest           string             `json:"guest_image_digest"`
	Drives                     []DriveDescriptor  `json:"drives"`
	Artifacts                  []SnapshotArtifact `json:"artifacts"`
	WrappedDataKey             string             `json:"wrapped_data_key"`
	EncryptionContextDigest    string             `json:"encryption_context_digest"`
	Checksum                   string             `json:"checksum,omitempty"`
}

func (m SnapshotManifestV1) CanonicalChecksum() (string, error) {
	m.Checksum = ""
	b, e := json.Marshal(m)
	if e != nil {
		return "", e
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), nil
}
func ValidateFullSnapshotManifest(m SnapshotManifestV1, configured []DriveDescriptor) error {
	if m.SchemaVersion != "v1" || m.ManifestID == "" || m.SandboxID == "" || m.SnapshotMode != "full" || m.FirecrackerSnapshotType != "Full" || m.LineageID == "" || m.SourceHostCompatibilityKey == "" || m.VMRestoreDescriptorDigest == "" || m.MachineTopologyDigest == "" || m.DeviceTopologyDigest == "" || m.GuestImageDigest == "" || m.WrappedDataKey == "" || m.EncryptionContextDigest == "" {
		return fmt.Errorf("incomplete full snapshot manifest")
	}
	k, e := SnapshotCompatibilityDigest(SnapshotCompatibilityDescriptor{SchemaVersion: m.SchemaVersion, LineageID: m.LineageID, SourceHostCompatibilityKey: m.SourceHostCompatibilityKey, VMRestoreDescriptorDigest: m.VMRestoreDescriptorDigest})
	if e != nil || k != m.SnapshotCompatibilityKey {
		return fmt.Errorf("invalid snapshot compatibility key")
	}
	if len(m.Drives) != len(configured) {
		return fmt.Errorf("drive inventory differs")
	}
	mut := map[string]bool{}
	mem, state := false, false
	for _, a := range m.Artifacts {
		if a.LogicalSize <= 0 || len(a.Chunks) == 0 {
			return fmt.Errorf("invalid artifact")
		}
		switch a.Type {
		case "memory.full":
			if mem || a.DriveID != "" {
				return fmt.Errorf("invalid memory")
			}
			mem = true
		case "vmstate.full":
			if state || a.DriveID != "" {
				return fmt.Errorf("invalid state")
			}
			state = true
		case "drive.full":
			if a.DriveID == "" || mut[a.DriveID] {
				return fmt.Errorf("invalid drive")
			}
			mut[a.DriveID] = true
		default:
			return fmt.Errorf("invalid artifact type")
		}
	}
	if !mem || !state {
		return fmt.Errorf("memory or vmstate missing")
	}
	for i, d := range configured {
		if m.Drives[i] != d || d.Mutable != mut[d.ID] {
			return fmt.Errorf("drive inventory differs")
		}
	}
	if m.Checksum == "" {
		return fmt.Errorf("checksum missing")
	}
	actual, e := m.CanonicalChecksum()
	if e != nil || actual != m.Checksum {
		return fmt.Errorf("checksum mismatch")
	}
	return nil
}
