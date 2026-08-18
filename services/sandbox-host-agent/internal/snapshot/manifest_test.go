package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func digest(v string) string { d := sha256.Sum256([]byte(v)); return hex.EncodeToString(d[:]) }
func validManifest(t *testing.T) (SnapshotManifestV1, []DriveDescriptor) {
	t.Helper()
	drives := []DriveDescriptor{{ID: "root", Mutable: false, ImmutableSHA256: digest("root")}, {ID: "data", Mutable: true}}
	art := func(kind ArtifactType, drive string) SnapshotArtifact {
		return SnapshotArtifact{Type: kind, DriveID: drive, LogicalSize: 1, SHA256: digest(string(kind) + drive), Chunks: []ChunkDescriptor{{Index: 0, PlaintextSize: 1, CiphertextSize: 17, PlaintextSHA256: digest("p" + drive + string(kind)), CiphertextSHA256: digest("c" + drive + string(kind)), Nonce: "000102030405060708090a0b", ObjectRef: "object/" + string(kind) + drive, ObjectVersion: "1"}}}
	}
	m := SnapshotManifestV1{SchemaVersion: ManifestVersionV1, ManifestID: "m", SandboxID: "s", Generation: 1, SnapshotMode: "full", FirecrackerSnapshotType: "Full", LineageID: "lineage", SourceHostCompatibilityKey: "host-key", VMRestoreDescriptorDigest: digest("vm"), MachineTopologyDigest: digest("machine"), DeviceTopologyDigest: digest("device"), GuestImageDigest: digest("guest"), Drives: drives, Artifacts: []SnapshotArtifact{art(ArtifactMemory, ""), art(ArtifactVMState, ""), art(ArtifactDrive, "data")}, WrappedDataKey: "wrapped", EncryptionContextDigest: digest("context")}
	key, err := SnapshotCompatibilityKey(CompatibilityDescriptor{m.SchemaVersion, m.LineageID, m.SourceHostCompatibilityKey, m.VMRestoreDescriptorDigest})
	if err != nil {
		t.Fatal(err)
	}
	m.SnapshotCompatibilityKey = key
	checksum, err := m.CanonicalChecksum()
	if err != nil {
		t.Fatal(err)
	}
	m.Checksum = checksum
	return m, drives
}
func TestValidateFullSnapshotManifest(t *testing.T) {
	m, drives := validManifest(t)
	if err := ValidateFullSnapshotManifest(m, drives); err != nil {
		t.Fatal(err)
	}
}
func TestManifestRejectsDiffAndIncompleteDrives(t *testing.T) {
	m, drives := validManifest(t)
	m.SnapshotMode = "diff"
	if err := ValidateFullSnapshotManifest(m, drives); err == nil {
		t.Fatal("accepted diff")
	}
	m, drives = validManifest(t)
	m.Artifacts = m.Artifacts[:2]
	c, _ := m.CanonicalChecksum()
	m.Checksum = c
	if err := ValidateFullSnapshotManifest(m, drives); err == nil {
		t.Fatal("accepted missing drive")
	}
}
func TestCompatibilityRequiresExactHostInputs(t *testing.T) {
	m, _ := validManifest(t)
	good := Candidate{LineageID: "lineage", HostCompatibilityKey: "host-key", VMRestoreDescriptorDigest: m.VMRestoreDescriptorDigest, ObservedCapabilitiesDigest: "observed"}
	if err := ValidateRestoreCandidate(m, good); err != nil {
		t.Fatal(err)
	}
	good.HostCompatibilityKey = "different"
	if err := ValidateRestoreCandidate(m, good); err == nil {
		t.Fatal("accepted a host key mismatch")
	}
}
