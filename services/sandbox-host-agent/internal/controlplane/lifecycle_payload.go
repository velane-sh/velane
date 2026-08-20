package controlplane

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
)

const LifecyclePayloadVersionV1 = "v1"

// LifecyclePayloadV1 mirrors the versioned control-plane command envelope. It
// contains immutable object identities and never caller-controlled local paths.
type LifecyclePayloadV1 struct {
	SchemaVersion    string               `json:"schema_version"`
	Command          string               `json:"command"`
	OperationKind    string               `json:"operation_kind"`
	SandboxID        string               `json:"sandbox_id"`
	OperationID      string               `json:"operation_id"`
	SnapshotID       string               `json:"snapshot_id,omitempty"`
	Generation       int64                `json:"generation"`
	Allocation       AllocationPayloadV1  `json:"allocation"`
	Resources        LifecycleResourcesV1 `json:"resources"`
	Machine          MachinePayloadV1     `json:"machine"`
	Guest            GuestArtifactsV1     `json:"guest"`
	GuestImageDigest string               `json:"guest_image_digest"`
	Drives           []LifecycleDriveV1   `json:"drives"`
	Network          json.RawMessage      `json:"network_policy"`
	Vsock            json.RawMessage      `json:"vsock_policy"`
	Jailer           JailerPayloadV1      `json:"jailer"`
	Lineage          RestoreLineageV1     `json:"lineage"`
	Restore          *RestorePayloadV1    `json:"restore,omitempty"`
	WatchdogGrant    json.RawMessage      `json:"signed_watchdog_grant"`
}

type AllocationPayloadV1 struct {
	ID              string `json:"id"`
	HostID          string `json:"host_id"`
	HostIncarnation int64  `json:"host_incarnation"`
	FenceEpoch      int64  `json:"fence_epoch"`
}

type LifecycleResourcesV1 struct {
	VCPU            int    `json:"vcpu"`
	MemoryMB        int    `json:"memory_mb"`
	CPUQuotaMicros  int64  `json:"cpu_quota_micros"`
	CPUPeriodMicros int64  `json:"cpu_period_micros"`
	MemoryMaxBytes  int64  `json:"memory_max_bytes"`
	PidsMax         int64  `json:"pids_max"`
	IOMax           string `json:"io_max,omitempty"`
}

type MachinePayloadV1 struct {
	SMT                   bool            `json:"smt"`
	MachineConfig         json.RawMessage `json:"machine_config"`
	DeviceTopology        json.RawMessage `json:"device_topology"`
	MachineTopologyDigest string          `json:"machine_topology_digest"`
	DeviceTopologyDigest  string          `json:"device_topology_digest"`
}

type ArtifactRefV1 struct {
	ObjectRef     string `json:"object_ref"`
	ObjectVersion string `json:"object_version,omitempty"`
	Digest        string `json:"digest"`
	SizeBytes     int64  `json:"size_bytes"`
}

type GuestArtifactsV1 struct {
	Kernel ArtifactRefV1 `json:"kernel"`
	Rootfs ArtifactRefV1 `json:"rootfs"`
	Init   ArtifactRefV1 `json:"init"`
}

type LifecycleDriveV1 struct {
	ID        string         `json:"id"`
	Mutable   bool           `json:"mutable"`
	Root      bool           `json:"root"`
	Order     int            `json:"order"`
	SizeBytes int64          `json:"size_bytes"`
	Artifact  *ArtifactRefV1 `json:"artifact,omitempty"`
}

type JailerPayloadV1 struct {
	UID int `json:"uid"`
	GID int `json:"gid"`
}

type RestoreLineageV1 struct {
	LineageID                  string `json:"lineage_id,omitempty"`
	SourceHostCompatibilityKey string `json:"source_host_compatibility_key,omitempty"`
	VMRestoreDescriptorDigest  string `json:"vm_restore_descriptor_digest"`
}

type RestorePayloadV1 struct {
	SnapshotID string          `json:"snapshot_id"`
	Manifest   json.RawMessage `json:"manifest"`
}

func DecodeLifecyclePayloadV1(raw []byte, command string) (LifecyclePayloadV1, error) {
	var value LifecyclePayloadV1
	if err := decodeStrict(raw, &value); err != nil {
		return value, err
	}
	return value, value.Validate(command)
}

func (p LifecyclePayloadV1) Validate(command string) error {
	if p.SchemaVersion != LifecyclePayloadVersionV1 || p.Command != command || !safeSegment(p.SandboxID) || p.OperationID == "" || p.Generation < 1 || p.Allocation.ID == "" || p.Allocation.HostID == "" || p.Allocation.HostIncarnation < 1 || p.Allocation.FenceEpoch < 1 {
		return fmt.Errorf("lifecycle identity is incomplete")
	}
	if err := requireJSONObject("signed watchdog grant", p.WatchdogGrant); err != nil {
		return err
	}
	if p.Resources.VCPU <= 0 || p.Resources.MemoryMB <= 0 || p.Resources.CPUQuotaMicros <= 0 || p.Resources.CPUPeriodMicros <= 0 || p.Resources.MemoryMaxBytes <= 0 || p.Resources.PidsMax <= 0 {
		return fmt.Errorf("lifecycle resource limits are incomplete")
	}
	if err := requireJSONObject("machine config", p.Machine.MachineConfig); err != nil {
		return err
	}
	if err := requireJSONObject("device topology", p.Machine.DeviceTopology); err != nil {
		return err
	}
	machineDigest, err := canonicalDigest(p.Machine.MachineConfig)
	if err != nil || machineDigest != p.Machine.MachineTopologyDigest {
		return fmt.Errorf("machine topology digest does not match machine config")
	}
	deviceDigest, err := canonicalDigest(p.Machine.DeviceTopology)
	if err != nil || deviceDigest != p.Machine.DeviceTopologyDigest {
		return fmt.Errorf("device topology digest does not match device topology")
	}
	if err := requireJSONObject("network policy", p.Network); err != nil {
		return err
	}
	if err := requireJSONObject("vsock policy", p.Vsock); err != nil {
		return err
	}
	if p.Jailer.UID <= 0 || p.Jailer.GID <= 0 || !validDigest(p.GuestImageDigest) || !validDigest(p.Lineage.VMRestoreDescriptorDigest) {
		return fmt.Errorf("lifecycle isolation or immutable image identity is incomplete")
	}
	for name, artifact := range map[string]ArtifactRefV1{"kernel": p.Guest.Kernel, "rootfs": p.Guest.Rootfs, "init": p.Guest.Init} {
		if err := validateArtifact(artifact); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	rootCount := 0
	mutableCount := 0
	for i, drive := range p.Drives {
		if !safeSegment(drive.ID) || drive.Order != i || drive.SizeBytes <= 0 {
			return fmt.Errorf("invalid drive inventory")
		}
		if drive.Root {
			rootCount++
		}
		if drive.Mutable {
			mutableCount++
			if drive.Artifact != nil {
				return fmt.Errorf("mutable drive has immutable artifact")
			}
		} else if drive.Artifact == nil {
			return fmt.Errorf("immutable drive artifact is missing")
		} else if err := validateArtifact(*drive.Artifact); err != nil {
			return err
		}
	}
	if rootCount != 1 || mutableCount == 0 {
		return fmt.Errorf("exactly one root and at least one mutable drive are required")
	}
	switch command {
	case string(CommandCreateSandbox):
		if p.SnapshotID != "" || p.Restore != nil || p.Lineage.LineageID != "" || p.Lineage.SourceHostCompatibilityKey != "" {
			return fmt.Errorf("create payload contains snapshot restore inputs")
		}
	case string(CommandCreateFullSnapshot):
		if p.SnapshotID == "" || p.Restore != nil {
			return fmt.Errorf("full snapshot payload is incomplete")
		}
	case string(CommandRestoreFullSnapshot):
		if p.SnapshotID != "" || p.Restore == nil || p.Restore.SnapshotID == "" || len(p.Restore.Manifest) == 0 || p.Lineage.LineageID == "" || p.Lineage.SourceHostCompatibilityKey == "" {
			return fmt.Errorf("restore inputs are incomplete")
		}
	default:
		return fmt.Errorf("unsupported lifecycle command %q", command)
	}
	return nil
}

func (p LifecyclePayloadV1) SnapshotManifest() (snapshotManifestIdentityV1, error) {
	var manifest snapshotManifestIdentityV1
	if p.Restore == nil {
		return manifest, fmt.Errorf("restore input is missing")
	}
	if err := json.Unmarshal(p.Restore.Manifest, &manifest); err != nil {
		return manifest, fmt.Errorf("restore manifest is invalid: %w", err)
	}
	return manifest, nil
}

// ValidateRestoreManifest binds the selected complete snapshot manifest to this
// exact lifecycle payload. Call it before staging and again immediately before
// Firecracker LoadSnapshot.
func (p LifecyclePayloadV1) ValidateRestoreManifest(candidate RestoreCandidateV1) error {
	manifest, err := p.SnapshotManifest()
	if err != nil {
		return err
	}
	if manifest.SchemaVersion != "v1" || manifest.SnapshotMode != "full" || manifest.FirecrackerSnapshotType != "Full" || manifest.SandboxID != p.SandboxID || int64(manifest.Generation) != p.Generation || manifest.LineageID != p.Lineage.LineageID || manifest.SourceHostCompatibilityKey != p.Lineage.SourceHostCompatibilityKey || manifest.VMRestoreDescriptorDigest != p.Lineage.VMRestoreDescriptorDigest || manifest.MachineTopologyDigest != p.Machine.MachineTopologyDigest || manifest.DeviceTopologyDigest != p.Machine.DeviceTopologyDigest || manifest.GuestImageDigest != p.GuestImageDigest {
		return fmt.Errorf("restore manifest is incompatible with canonical payload")
	}
	if candidate.LineageID != p.Lineage.LineageID || candidate.HostCompatibilityKey != p.Lineage.SourceHostCompatibilityKey || candidate.VMRestoreDescriptorDigest != p.Lineage.VMRestoreDescriptorDigest || candidate.ObservedCapabilitiesDigest == "" {
		return fmt.Errorf("restore candidate is incompatible with canonical payload")
	}
	return nil
}

type RestoreCandidateV1 struct {
	LineageID                  string
	HostCompatibilityKey       string
	VMRestoreDescriptorDigest  string
	ObservedCapabilitiesDigest string
}

type snapshotManifestIdentityV1 struct {
	SchemaVersion              string `json:"schema_version"`
	SandboxID                  string `json:"sandbox_id"`
	Generation                 uint64 `json:"generation"`
	SnapshotMode               string `json:"snapshot_mode"`
	FirecrackerSnapshotType    string `json:"firecracker_snapshot_type"`
	LineageID                  string `json:"lineage_id"`
	SourceHostCompatibilityKey string `json:"source_host_compatibility_key"`
	VMRestoreDescriptorDigest  string `json:"vm_restore_descriptor_digest"`
	MachineTopologyDigest      string `json:"machine_topology_digest"`
	DeviceTopologyDigest       string `json:"device_topology_digest"`
	GuestImageDigest           string `json:"guest_image_digest"`
}

func validateArtifact(artifact ArtifactRefV1) error {
	if !safeObjectRef(artifact.ObjectRef) || !validDigest(artifact.Digest) || artifact.SizeBytes <= 0 {
		return fmt.Errorf("artifact reference is incomplete or unsafe")
	}
	return nil
}

func validDigest(value string) bool {
	if len(value) != 64 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func safeSegment(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, `/\\`) && !strings.ContainsRune(value, '\x00')
}

func safeObjectRef(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "\\\x00\r\n") || strings.HasPrefix(value, "/") || strings.HasPrefix(strings.ToLower(value), "file:") {
		return false
	}
	clean := path.Clean(value)
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, "../")
}

func requireJSONObject(name string, raw json.RawMessage) error {
	var value map[string]any
	if err := decodeStrict(raw, &value); err != nil || value == nil || len(value) == 0 {
		return fmt.Errorf("%s must be a non-empty JSON object", name)
	}
	return nil
}

func canonicalDigest(raw json.RawMessage) (string, error) {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("only one JSON document is allowed")
		}
		return err
	}
	return nil
}
