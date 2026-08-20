package agent

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/abskrj/velane/services/sandbox-host-agent/internal/controlplane"
	"github.com/abskrj/velane/services/sandbox-host-agent/internal/snapshot"
)

const defaultSnapshotChunkSize = 8 << 20

// SnapshotControl is the narrow mTLS protocol needed after Firecracker has
// created a full snapshot. It only returns presigned part capabilities.
type SnapshotControl interface {
	SnapshotUploadPlan(context.Context, string, string, controlplane.SnapshotUploadPlanRequest) (controlplane.SnapshotUploadPlan, error)
	CompleteSnapshot(context.Context, string, string, controlplane.SnapshotCompleteRequest) error
}

// FullSnapshotUploader encrypts every full-snapshot file before the host sends
// it to a one-use URL. It deliberately has no S3 client or provider secret.
type FullSnapshotUploader struct {
	Control   SnapshotControl
	HostID    string
	DataKeys  snapshot.DataKeyProvider
	HTTP      controlplane.PresignedSnapshotUploader
	ChunkSize int
}

func (u FullSnapshotUploader) UploadFullSnapshot(ctx context.Context, command controlplane.LifecyclePayloadV1, files LifecyclePayload) error {
	if u.Control == nil || u.HostID == "" || u.DataKeys == nil || command.SnapshotID == "" {
		return fmt.Errorf("snapshot upload control, data key, or snapshot ID is unavailable")
	}
	if files.MemoryPath == "" || files.VMStatePath == "" || len(files.MutableDrivePaths) == 0 {
		return fmt.Errorf("full snapshot files are incomplete")
	}
	contextBytes, err := json.Marshal(map[string]string{"sandbox_id": command.SandboxID, "snapshot_id": command.SnapshotID, "allocation_id": command.Allocation.ID})
	if err != nil {
		return err
	}
	key, wrapped, err := u.DataKeys.NewDataKey(ctx, contextBytes)
	if err != nil {
		return fmt.Errorf("create snapshot data key: %w", err)
	}
	if len(key) != 32 || len(wrapped) == 0 {
		return fmt.Errorf("snapshot data key provider returned incomplete key material")
	}
	contextDigest := sha256.Sum256(contextBytes)
	manifest, encrypted, err := buildEncryptedSnapshot(command, files, key, wrapped, hex.EncodeToString(contextDigest[:]), u.chunkSize())
	if err != nil {
		return err
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	plan, err := u.Control.SnapshotUploadPlan(ctx, u.HostID, command.SnapshotID, controlplane.SnapshotUploadPlanRequest{AllocationID: command.Allocation.ID, HostIncarnation: uint64(command.Allocation.HostIncarnation), FenceEpoch: uint64(command.Allocation.FenceEpoch), Manifest: manifestBytes})
	if err != nil {
		return fmt.Errorf("obtain full snapshot upload plan: %w", err)
	}
	if plan.SnapshotID != command.SnapshotID || len(plan.Artifacts) != len(manifest.Artifacts) {
		return fmt.Errorf("control plane returned an incomplete snapshot upload plan")
	}
	completed := make([]controlplane.SnapshotCompletedUpload, 0)
	for artifactIndex := range manifest.Artifacts {
		planArtifact, ok := planArtifactFor(plan, string(manifest.Artifacts[artifactIndex].Type), manifest.Artifacts[artifactIndex].DriveID)
		if !ok || len(planArtifact.Chunks) != len(manifest.Artifacts[artifactIndex].Chunks) {
			return fmt.Errorf("upload plan is missing an expected artifact")
		}
		for chunkIndex := range manifest.Artifacts[artifactIndex].Chunks {
			chunk := &manifest.Artifacts[artifactIndex].Chunks[chunkIndex]
			descriptor, ok := planChunkFor(planArtifact, chunk.Index)
			if !ok || descriptor.ChecksumSHA256 != encrypted[artifactIndex][chunkIndex].ChecksumBase64 {
				return fmt.Errorf("upload plan checksum does not match encrypted chunk")
			}
			result, err := u.HTTP.Put(ctx, descriptor, bytesReader(encrypted[artifactIndex][chunkIndex].Ciphertext), int64(len(encrypted[artifactIndex][chunkIndex].Ciphertext)))
			if err != nil {
				return err
			}
			completed = append(completed, result)
			chunk.ObjectRef = descriptor.ObjectRef
		}
	}
	manifestBytes, err = json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err := u.Control.CompleteSnapshot(ctx, u.HostID, command.SnapshotID, controlplane.SnapshotCompleteRequest{AllocationID: command.Allocation.ID, HostIncarnation: uint64(command.Allocation.HostIncarnation), FenceEpoch: uint64(command.Allocation.FenceEpoch), Manifest: manifestBytes, CompletedUploads: completed}); err != nil {
		return fmt.Errorf("complete full snapshot: %w", err)
	}
	return nil
}

type encryptedSnapshotChunk struct {
	Ciphertext     []byte
	ChecksumBase64 string
}

func (u FullSnapshotUploader) chunkSize() int {
	if u.ChunkSize > 0 {
		return u.ChunkSize
	}
	return defaultSnapshotChunkSize
}

func buildEncryptedSnapshot(command controlplane.LifecyclePayloadV1, files LifecyclePayload, key, wrapped []byte, contextDigest string, chunkSize int) (snapshot.SnapshotManifestV1, [][]encryptedSnapshotChunk, error) {
	drives := make([]snapshot.DriveDescriptor, 0, len(command.Drives))
	for _, drive := range command.Drives {
		descriptor := snapshot.DriveDescriptor{ID: drive.ID, Mutable: drive.Mutable}
		if !drive.Mutable && drive.Artifact != nil {
			descriptor.ImmutableSHA256 = drive.Artifact.Digest
		}
		drives = append(drives, descriptor)
	}
	manifest := snapshot.SnapshotManifestV1{SchemaVersion: snapshot.ManifestVersionV1, ManifestID: command.SnapshotID, SandboxID: command.SandboxID, Generation: uint64(command.Generation), SnapshotMode: "full", FirecrackerSnapshotType: "Full", LineageID: command.Lineage.LineageID, SourceHostCompatibilityKey: command.Lineage.SourceHostCompatibilityKey, VMRestoreDescriptorDigest: command.Lineage.VMRestoreDescriptorDigest, MachineTopologyDigest: command.Machine.MachineTopologyDigest, DeviceTopologyDigest: command.Machine.DeviceTopologyDigest, GuestImageDigest: command.Guest.Rootfs.Digest, Drives: drives, WrappedDataKey: hex.EncodeToString(wrapped), EncryptionContextDigest: contextDigest}
	compatibility, err := snapshot.SnapshotCompatibilityKey(snapshot.CompatibilityDescriptor{SchemaVersion: manifest.SchemaVersion, LineageID: manifest.LineageID, SourceHostCompatibilityKey: manifest.SourceHostCompatibilityKey, VMRestoreDescriptorDigest: manifest.VMRestoreDescriptorDigest})
	if err != nil {
		return snapshot.SnapshotManifestV1{}, nil, err
	}
	manifest.SnapshotCompatibilityKey = compatibility
	sources := []struct {
		typ           snapshot.ArtifactType
		driveID, path string
	}{{snapshot.ArtifactMemory, "", files.MemoryPath}, {snapshot.ArtifactVMState, "", files.VMStatePath}}
	for _, id := range sortedKeys(files.MutableDrivePaths) {
		sources = append(sources, struct {
			typ           snapshot.ArtifactType
			driveID, path string
		}{snapshot.ArtifactDrive, id, files.MutableDrivePaths[id]})
	}
	encrypted := make([][]encryptedSnapshotChunk, 0, len(sources))
	for _, source := range sources {
		artifact, chunks, err := encryptArtifact(command.SnapshotID, source.typ, source.driveID, source.path, key, chunkSize)
		if err != nil {
			return snapshot.SnapshotManifestV1{}, nil, err
		}
		manifest.Artifacts, encrypted = append(manifest.Artifacts, artifact), append(encrypted, chunks)
	}
	return manifest, encrypted, nil
}

func encryptArtifact(snapshotID string, typ snapshot.ArtifactType, driveID, path string, key []byte, chunkSize int) (snapshot.SnapshotArtifact, []encryptedSnapshotChunk, error) {
	file, err := os.Open(path)
	if err != nil {
		return snapshot.SnapshotArtifact{}, nil, fmt.Errorf("open snapshot artifact %q: %w", path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() <= 0 {
		return snapshot.SnapshotArtifact{}, nil, fmt.Errorf("snapshot artifact %q is empty or unavailable", path)
	}
	artifact := snapshot.SnapshotArtifact{Type: typ, DriveID: driveID, LogicalSize: info.Size()}
	logical := sha256.New()
	chunks := make([]encryptedSnapshotChunk, 0)
	buffer := make([]byte, chunkSize)
	for index := 0; ; index++ {
		n, readErr := file.Read(buffer)
		if n > 0 {
			plaintext := append([]byte(nil), buffer[:n]...)
			_, _ = logical.Write(plaintext)
			associatedData, _ := json.Marshal(map[string]any{"snapshot_id": snapshotID, "type": typ, "drive_id": driveID, "index": index})
			encrypted, encryptErr := snapshot.EncryptChunk(key, plaintext, associatedData)
			if encryptErr != nil {
				return snapshot.SnapshotArtifact{}, nil, encryptErr
			}
			checksumBytes, _ := hex.DecodeString(encrypted.CiphertextSHA256)
			artifact.Chunks = append(artifact.Chunks, snapshot.ChunkDescriptor{Index: index, PlaintextSize: int64(len(plaintext)), CiphertextSize: int64(len(encrypted.Ciphertext)), PlaintextSHA256: encrypted.PlaintextSHA256, CiphertextSHA256: encrypted.CiphertextSHA256, Nonce: hex.EncodeToString(encrypted.Nonce)})
			chunks = append(chunks, encryptedSnapshotChunk{Ciphertext: encrypted.Ciphertext, ChecksumBase64: base64Encode(checksumBytes)})
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return snapshot.SnapshotArtifact{}, nil, readErr
		}
	}
	artifact.SHA256 = hex.EncodeToString(logical.Sum(nil))
	return artifact, chunks, nil
}

func planArtifactFor(plan controlplane.SnapshotUploadPlan, typ, driveID string) (controlplane.SnapshotUploadPlanArtifact, bool) {
	for _, artifact := range plan.Artifacts {
		if artifact.Type == typ && artifact.DriveID == driveID {
			return artifact, true
		}
	}
	return controlplane.SnapshotUploadPlanArtifact{}, false
}
func planChunkFor(artifact controlplane.SnapshotUploadPlanArtifact, index int) (controlplane.SnapshotUploadPlanChunk, bool) {
	for _, chunk := range artifact.Chunks {
		if chunk.Index == index {
			return chunk, true
		}
	}
	return controlplane.SnapshotUploadPlanChunk{}, false
}
func base64Encode(v []byte) string { return base64.StdEncoding.EncodeToString(v) }

// byteReader avoids buffering an additional copy while preserving an exact
// length-bounded reader for the scoped HTTP PUT.
type byteReader []byte

func (b *byteReader) Read(p []byte) (int, error) {
	if len(*b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, *b)
	*b = (*b)[n:]
	return n, nil
}
func bytesReader(b []byte) *byteReader { reader := byteReader(b); return &reader }
