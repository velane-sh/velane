package agent

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/abskrj/velane/services/sandbox-host-agent/internal/controlplane"
	"github.com/abskrj/velane/services/sandbox-host-agent/internal/microvm"
	"github.com/abskrj/velane/services/sandbox-host-agent/internal/network"
	"github.com/abskrj/velane/services/sandbox-host-agent/internal/resources"
)

// LifecycleExecutor translates fenced control-plane commands to real privileged
// operations. It deliberately returns an error for missing host prerequisites;
// it never acknowledges a simulated lifecycle outcome.
type LifecycleExecutor struct {
	VM          microvm.Manager
	Network     network.Manager
	Disks       string
	RuntimeRoot string
	Snapshotter SnapshotUploader
	Create      func(context.Context, LifecyclePayload) error
	Watchdog    interface {
		Deliver(context.Context, json.RawMessage) error
	}
}

// SnapshotUploader captures, encrypts, uploads, and commits one full fenced
// bundle. A successful Firecracker API call is not a successful command until
// this returns nil.
type SnapshotUploader interface {
	UploadFullSnapshot(context.Context, controlplane.LifecyclePayloadV1, LifecyclePayload) error
}

type LifecyclePayload struct {
	SandboxID         string                 `json:"sandbox_id"`
	OperationKind     string                 `json:"kind"`
	JailPath          string                 `json:"jail_path"`
	MemoryPath        string                 `json:"memory_path"`
	VMStatePath       string                 `json:"vmstate_path"`
	MutableDrivePaths map[string]string      `json:"mutable_drive_paths"`
	MutableDrives     []MutableDrive         `json:"mutable_drives"`
	ImmutableDrives   []ImmutableDrive       `json:"immutable_drives"`
	Resources         ResourceRequest        `json:"resources"`
	Machine           MachineRequest         `json:"machine"`
	MachineConfig     MachineRequest         `json:"machine_config"`
	Boot              BootRequest            `json:"boot"`
	BootSource        BootRequest            `json:"boot_source"`
	NetworkNamespace  string                 `json:"network_namespace"`
	Network           network.SandboxNetwork `json:"network"`
	JailerUID         int                    `json:"jailer_uid"`
	JailerGID         int                    `json:"jailer_gid"`
	CgroupPath        string                 `json:"cgroup_path"`
	SignedLeaseGrant  json.RawMessage        `json:"signed_lease_grant"`
}

type lifecyclePayload = LifecyclePayload

func (e LifecycleExecutor) Execute(ctx context.Context, command controlplane.Command) error {
	if e.VM == nil {
		return errors.New("microVM lifecycle driver is unavailable; host is not placeable")
	}
	if controlplane.CommandKind(command.Kind) == controlplane.CommandDestroy {
		var destroy struct {
			SandboxID string `json:"sandbox_id"`
		}
		if err := json.Unmarshal(command.Payload, &destroy); err != nil || destroy.SandboxID == "" {
			return errors.New("destroy command lacks sandbox identity")
		}
		var errs []error
		if e.Network != nil {
			errs = append(errs, e.Network.Remove(ctx, destroy.SandboxID))
		}
		errs = append(errs, e.VM.Destroy(ctx, destroy.SandboxID))
		return errors.Join(errs...)
	}
	var canonical controlplane.LifecyclePayloadV1
	if err := json.Unmarshal(command.Payload, &canonical); err != nil {
		return fmt.Errorf("decode canonical lifecycle command: %w", err)
	}
	if err := canonical.Validate(command.Kind); err != nil {
		return fmt.Errorf("reject canonical lifecycle command before side effects: %w", err)
	}
	if canonical.Allocation.ID != command.AllocationID || canonical.Allocation.HostIncarnation != int64(command.HostIncarnation) || canonical.Allocation.FenceEpoch != int64(command.FenceEpoch) {
		return errors.New("canonical lifecycle payload fencing does not match command")
	}
	var p lifecyclePayload
	if err := json.Unmarshal(command.Payload, &p); err != nil {
		return fmt.Errorf("decode lifecycle command: %w", err)
	}
	if p.SandboxID == "" {
		return errors.New("lifecycle command lacks sandbox_id")
	}
	switch controlplane.CommandKind(command.Kind) {
	case controlplane.CommandCreateSandbox:
		if e.Create == nil {
			return errors.New("sandbox create driver is unavailable; host is not placeable")
		}
		p = e.localCreatePayload(canonical)
		return e.Create(ctx, p)
	case controlplane.CommandCreateFullSnapshot:
		if e.Snapshotter == nil {
			return errors.New("full snapshot uploader is unavailable")
		}
		p = e.localSnapshotPayload(canonical)
		if err := validateSnapshotPaths(p); err != nil {
			return err
		}
		if err := e.VM.Pause(ctx, p.SandboxID); err != nil {
			return fmt.Errorf("pause sandbox: %w", err)
		}
		err := e.VM.CreateFullSnapshot(ctx, microvm.SnapshotRequest{SandboxID: p.SandboxID, MemoryPath: p.MemoryPath, VMStatePath: p.VMStatePath})
		if err != nil {
			_ = e.VM.Resume(ctx, p.SandboxID)
			return fmt.Errorf("create full snapshot: %w", err)
		}
		for _, id := range sortedKeys(p.MutableDrivePaths) {
			source := p.MutableDrivePaths[id]
			if err := copyDrive(p.JailPath, id, source); err != nil {
				_ = e.VM.Resume(ctx, p.SandboxID)
				return err
			}
		}
		if err := e.Snapshotter.UploadFullSnapshot(ctx, canonical, p); err != nil {
			_ = e.VM.Resume(ctx, p.SandboxID)
			return fmt.Errorf("upload full snapshot: %w", err)
		}
		// Stop/restart checkpoints stay paused after a durable commit; a manual
		// snapshot resumes only after complete/verification succeeded.
		if canonical.OperationKind == "stop" || canonical.OperationKind == "restart" || canonical.OperationKind == "drain" {
			return nil
		}
		return e.VM.Resume(ctx, p.SandboxID)
	case controlplane.CommandRestoreFullSnapshot:
		candidate := controlplane.RestoreCandidateV1{
			LineageID:                 canonical.Lineage.LineageID,
			HostCompatibilityKey:      canonical.Lineage.SourceHostCompatibilityKey,
			VMRestoreDescriptorDigest: canonical.Lineage.VMRestoreDescriptorDigest,
		}
		if err := canonical.ValidateRestoreManifest(candidate); err != nil {
			return fmt.Errorf("reject restore before artifact download: %w", err)
		}
		if err := e.deliverLease(ctx, canonical.WatchdogGrant); err != nil {
			return err
		}
		// This is the second strict compatibility gate. Artifact download/staging
		// belongs to the snapshot backend; do not invoke Firecracker if the
		// restored envelope or its pinned manifest drifted in the meantime.
		if canonical.Restore == nil || canonical.Lineage.LineageID == "" || canonical.Lineage.SourceHostCompatibilityKey == "" {
			return errors.New("restore compatibility inputs are unavailable")
		}
		p = e.localRestorePayload(canonical)
		if err := canonical.ValidateRestoreManifest(candidate); err != nil {
			return fmt.Errorf("reject restore before Firecracker load: %w", err)
		}
		if err := microvm.Restore(ctx, e.VM, microvm.RestoreRequest{SandboxID: p.SandboxID, JailPath: p.JailPath, MemoryPath: p.MemoryPath, VMStatePath: p.VMStatePath, MutableDrivePaths: p.MutableDrivePaths}); err != nil {
			return fmt.Errorf("strict full restore: %w", err)
		}
		return e.VM.Resume(ctx, p.SandboxID)
	default:
		return fmt.Errorf("unsupported sandbox host command %q", command.Kind)
	}
}

// localCreatePayload derives paths only from fixed host-owned roots. Remote
// payloads can name immutable objects but cannot select a local filesystem path.
func (e LifecycleExecutor) localCreatePayload(p controlplane.LifecyclePayloadV1) LifecyclePayload {
	immutable := make([]ImmutableDrive, 0)
	mutable := make([]MutableDrive, 0)
	for _, drive := range p.Drives {
		if drive.Mutable {
			mutable = append(mutable, MutableDrive{ID: drive.ID, Size: drive.SizeBytes, Root: drive.Root})
			continue
		}
		immutable = append(immutable, ImmutableDrive{ID: drive.ID, Path: e.localArtifactPath(drive.Artifact.Digest), Root: drive.Root})
	}
	return LifecyclePayload{SandboxID: p.SandboxID, Resources: ResourceRequest{CPUQuotaMicros: p.Resources.CPUQuotaMicros, CPUPeriodMicros: p.Resources.CPUPeriodMicros, MemoryMaxBytes: p.Resources.MemoryMaxBytes, PidsMax: p.Resources.PidsMax, IOMax: p.Resources.IOMax}, Machine: MachineRequest{VCPUCount: p.Resources.VCPU, MemoryMB: p.Resources.MemoryMB, SMT: p.Machine.SMT}, Boot: BootRequest{KernelPath: e.localArtifactPath(p.Guest.Kernel.Digest)}, MutableDrives: mutable, ImmutableDrives: immutable, Network: deterministicNetwork(p.SandboxID), JailerUID: p.Jailer.UID, JailerGID: p.Jailer.GID, SignedLeaseGrant: p.WatchdogGrant}
}

func (e LifecycleExecutor) localRestorePayload(p controlplane.LifecyclePayloadV1) LifecyclePayload {
	// The snapshot backend writes all reconstructed files below the jail. This
	// executor refuses arbitrary remote paths and will not cold boot if they are
	// absent.
	root := e.RuntimeRoot
	if root == "" {
		root = "/var/lib/velane-sandbox-agent/jails"
	}
	jail := filepath.Join(root, p.SandboxID)
	drives := make(map[string]string)
	for _, drive := range p.Drives {
		if drive.Mutable {
			drives[drive.ID] = filepath.Join(jail, "restore", "drives", drive.ID+".raw")
		}
	}
	return LifecyclePayload{SandboxID: p.SandboxID, JailPath: jail, MemoryPath: filepath.Join(jail, "restore", "memory.full"), VMStatePath: filepath.Join(jail, "restore", "vmstate.full"), MutableDrivePaths: drives, SignedLeaseGrant: p.WatchdogGrant}
}

func (e LifecycleExecutor) localSnapshotPayload(p controlplane.LifecyclePayloadV1) LifecyclePayload {
	root := e.RuntimeRoot
	if root == "" {
		root = "/var/lib/velane-sandbox-agent/jails"
	}
	jail := filepath.Join(root, p.SandboxID)
	drives := make(map[string]string)
	for _, drive := range p.Drives {
		if drive.Mutable {
			drives[drive.ID] = filepath.Join(root, "disks", p.SandboxID, drive.ID+".raw")
		}
	}
	return LifecyclePayload{SandboxID: p.SandboxID, OperationKind: p.OperationKind, JailPath: jail, MemoryPath: filepath.Join(jail, "snapshot", "memory.full"), VMStatePath: filepath.Join(jail, "snapshot", "vmstate.full"), MutableDrivePaths: drives}
}

func validateSnapshotPaths(p LifecyclePayload) error {
	if p.SandboxID == "" || p.JailPath == "" || p.MemoryPath == "" || p.VMStatePath == "" || len(p.MutableDrivePaths) == 0 {
		return errors.New("full snapshot requires memory, VM state, and every mutable drive")
	}
	return nil
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (e LifecycleExecutor) localArtifactPath(digest string) string {
	root := e.RuntimeRoot
	if root == "" {
		root = "/var/lib/velane-sandbox-agent/jails"
	}
	return filepath.Join(root, "artifacts", digest)
}

func deterministicNetwork(sandboxID string) network.SandboxNetwork {
	sum := sha256.Sum256([]byte(sandboxID))
	return network.SandboxNetwork{SandboxID: sandboxID, Namespace: "velane-" + sandboxID, TapName: fmt.Sprintf("tap-%x", sum[:4]), HostVeth: fmt.Sprintf("veth-%x", sum[4:8])}
}
func (p *LifecyclePayload) normalizeCreate() {
	if p.Network.Namespace == "" {
		p.Network.Namespace = p.NetworkNamespace
	}
	if p.Network.SandboxID == "" {
		p.Network.SandboxID = p.SandboxID
	}
	if p.Machine.VCPUCount == 0 {
		p.Machine = p.MachineConfig
	}
	if p.Boot.KernelPath == "" {
		p.Boot = p.BootSource
	}
}
func (e LifecycleExecutor) deliverLease(ctx context.Context, grant json.RawMessage) error {
	if len(grant) == 0 {
		return errors.New("lifecycle command lacks signed watchdog lease grant")
	}
	if e.Watchdog == nil {
		return errors.New("watchdog lease delivery is unavailable; host is not placeable")
	}
	if err := e.Watchdog.Deliver(ctx, grant); err != nil {
		return fmt.Errorf("deliver signed watchdog lease: %w", err)
	}
	return nil
}
func copyDrive(jailPath, id, source string) error {
	if jailPath == "" || id == "" || source == "" {
		return errors.New("snapshot drive staging data is incomplete")
	}
	if filepath.Base(id) != id {
		return errors.New("snapshot drive ID is unsafe")
	}
	if err := os.MkdirAll(filepath.Join(jailPath, "snapshot-drives"), 0700); err != nil {
		return err
	}
	destination := filepath.Join(jailPath, "snapshot-drives", id+".raw")
	return resources.CopyStable(source, destination)
}
