package domain

import "testing"

func TestSandboxTransitions(t *testing.T) {
	if err := ValidateSandboxTransition(SandboxObservedStopped, SandboxObservedRestoring); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSandboxTransition(SandboxObservedStopped, SandboxObservedBootstrapping); err == nil {
		t.Fatal("forbidden transition accepted")
	}
}
func TestSnapshotCompatibilityDigestIncludesAllInputs(t *testing.T) {
	base := SnapshotCompatibilityDescriptor{"1", "lineage", "host-key", "vm"}
	d := MustSnapshotDigest(t, base)
	for _, v := range []SnapshotCompatibilityDescriptor{{"2", "lineage", "host-key", "vm"}, {"1", "lineage2", "host-key", "vm"}, {"1", "lineage", "host-key2", "vm"}, {"1", "lineage", "host-key", "vm2"}} {
		if d == MustSnapshotDigest(t, v) {
			t.Fatalf("digest failed to vary: %#v", v)
		}
	}
}
func MustSnapshotDigest(t *testing.T, d SnapshotCompatibilityDescriptor) string {
	t.Helper()
	v, e := SnapshotCompatibilityDigest(d)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestFullManifestRequiresCompleteBundle(t *testing.T) {
	d := SnapshotCompatibilityDescriptor{"v1", "lineage", "key", "vm"}
	k := MustSnapshotDigest(t, d)
	drives := []DriveDescriptor{{ID: "data", Mutable: true}}
	m := SnapshotManifestV1{SchemaVersion: "v1", ManifestID: "m", SandboxID: "sandbox", SnapshotMode: "full", FirecrackerSnapshotType: "Full", LineageID: "lineage", SourceHostCompatibilityKey: "key", VMRestoreDescriptorDigest: "vm", SnapshotCompatibilityKey: k, MachineTopologyDigest: "machine", DeviceTopologyDigest: "device", GuestImageDigest: "guest", Drives: drives, WrappedDataKey: "wrapped", EncryptionContextDigest: "context", Artifacts: []SnapshotArtifact{{Type: "memory.full", LogicalSize: 1, SHA256: "a", Chunks: []ChunkDescriptor{{}}}, {Type: "vmstate.full", LogicalSize: 1, SHA256: "b", Chunks: []ChunkDescriptor{{}}}, {Type: "drive.full", DriveID: "data", LogicalSize: 1, SHA256: "c", Chunks: []ChunkDescriptor{{}}}}}
	checksum, err := m.CanonicalChecksum()
	if err != nil {
		t.Fatal(err)
	}
	m.Checksum = checksum
	if err := ValidateFullSnapshotManifest(m, drives); err != nil {
		t.Fatal(err)
	}
	m.FirecrackerSnapshotType = "Diff"
	if err := ValidateFullSnapshotManifest(m, drives); err == nil {
		t.Fatal("diff snapshot accepted")
	}
}
