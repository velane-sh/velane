// Package snapshot defines the canonical, complete Firecracker snapshot bundle.
package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

const ManifestVersionV1 = "v1"

var (
	ErrIncompleteBundle = errors.New("snapshot bundle is incomplete")
	ErrNotFullSnapshot  = errors.New("snapshot must use Firecracker Full mode")
)

type ArtifactType string

const (
	ArtifactMemory  ArtifactType = "memory.full"
	ArtifactVMState ArtifactType = "vmstate.full"
	ArtifactDrive   ArtifactType = "drive.full"
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
	Type        ArtifactType      `json:"type"`
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

// SnapshotManifestV1 intentionally embeds all restore-critical inputs. No
// restore implementation may infer omitted disks or replace them with a boot.
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

type CompatibilityDescriptor struct {
	SchemaVersion              string `json:"schema_version"`
	LineageID                  string `json:"lineage_id"`
	SourceHostCompatibilityKey string `json:"source_host_compatibility_key"`
	VMRestoreDescriptorDigest  string `json:"vm_restore_descriptor_digest"`
}

func SnapshotCompatibilityKey(d CompatibilityDescriptor) (string, error) {
	if d.SchemaVersion == "" || d.LineageID == "" || d.SourceHostCompatibilityKey == "" || d.VMRestoreDescriptorDigest == "" {
		return "", errors.New("all snapshot compatibility fields are required")
	}
	b, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), nil
}

func (m SnapshotManifestV1) CanonicalChecksum() (string, error) {
	m.Checksum = ""
	b, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), nil
}

func ValidateFullSnapshotManifest(m SnapshotManifestV1, configuredDrives []DriveDescriptor) error {
	if m.SchemaVersion != ManifestVersionV1 || m.ManifestID == "" || m.SandboxID == "" || m.LineageID == "" || m.SourceHostCompatibilityKey == "" || m.VMRestoreDescriptorDigest == "" {
		return fmt.Errorf("%w: required identity fields are absent", ErrIncompleteBundle)
	}
	if m.SnapshotMode != "full" || m.FirecrackerSnapshotType != "Full" {
		return ErrNotFullSnapshot
	}
	key, err := SnapshotCompatibilityKey(CompatibilityDescriptor{m.SchemaVersion, m.LineageID, m.SourceHostCompatibilityKey, m.VMRestoreDescriptorDigest})
	if err != nil || m.SnapshotCompatibilityKey != key {
		return fmt.Errorf("%w: invalid compatibility key", ErrIncompleteBundle)
	}
	if m.MachineTopologyDigest == "" || m.DeviceTopologyDigest == "" || m.GuestImageDigest == "" || m.WrappedDataKey == "" || m.EncryptionContextDigest == "" {
		return fmt.Errorf("%w: missing restore metadata", ErrIncompleteBundle)
	}
	if len(m.Drives) != len(configuredDrives) {
		return fmt.Errorf("%w: drive inventory differs", ErrIncompleteBundle)
	}
	for i, expected := range configuredDrives {
		actual := m.Drives[i]
		if actual != expected || (!actual.Mutable && actual.ImmutableSHA256 == "") {
			return fmt.Errorf("%w: drive %d differs", ErrIncompleteBundle, i)
		}
	}
	seenMemory, seenState := false, false
	mutableArtifacts := map[string]bool{}
	for _, a := range m.Artifacts {
		if a.LogicalSize <= 0 || !validDigest(a.SHA256) || len(a.Chunks) == 0 {
			return fmt.Errorf("%w: malformed %s artifact", ErrIncompleteBundle, a.Type)
		}
		for i, c := range a.Chunks {
			if c.Index != i || c.PlaintextSize <= 0 || c.CiphertextSize <= 0 || !validDigest(c.PlaintextSHA256) || !validDigest(c.CiphertextSHA256) || !validNonce(c.Nonce) || c.ObjectRef == "" || c.ObjectVersion == "" {
				return fmt.Errorf("%w: malformed chunk", ErrIncompleteBundle)
			}
		}
		switch a.Type {
		case ArtifactMemory:
			if seenMemory || a.DriveID != "" {
				return fmt.Errorf("%w: memory must appear exactly once", ErrIncompleteBundle)
			}
			seenMemory = true
		case ArtifactVMState:
			if seenState || a.DriveID != "" {
				return fmt.Errorf("%w: VM state must appear exactly once", ErrIncompleteBundle)
			}
			seenState = true
		case ArtifactDrive:
			if a.DriveID == "" || mutableArtifacts[a.DriveID] {
				return fmt.Errorf("%w: duplicate or unnamed drive", ErrIncompleteBundle)
			}
			mutableArtifacts[a.DriveID] = true
		default:
			return fmt.Errorf("%w: unknown artifact type", ErrIncompleteBundle)
		}
	}
	if !seenMemory || !seenState {
		return fmt.Errorf("%w: memory or VM state missing", ErrIncompleteBundle)
	}
	for _, d := range configuredDrives {
		if d.Mutable != mutableArtifacts[d.ID] {
			return fmt.Errorf("%w: mutable drive %q missing or immutable drive included", ErrIncompleteBundle, d.ID)
		}
	}
	if m.Checksum == "" {
		return fmt.Errorf("%w: checksum missing", ErrIncompleteBundle)
	}
	actual, err := m.CanonicalChecksum()
	if err != nil || actual != m.Checksum {
		return fmt.Errorf("%w: checksum mismatch", ErrIncompleteBundle)
	}
	return nil
}

func validDigest(v string) bool { _, err := hex.DecodeString(v); return len(v) == 64 && err == nil }

func validNonce(v string) bool {
	b, err := hex.DecodeString(v)
	return err == nil && len(b) == 12
}

// SortedArtifactRefs returns deterministic refs for audit or upload planning.
func (m SnapshotManifestV1) SortedArtifactRefs() []string {
	refs := make([]string, 0)
	for _, a := range m.Artifacts {
		for _, c := range a.Chunks {
			refs = append(refs, c.ObjectRef)
		}
	}
	sort.Strings(refs)
	return refs
}
