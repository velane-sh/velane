package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/abskrj/velane/services/sandbox-host-agent/internal/controlplane"
	"github.com/abskrj/velane/services/sandbox-host-agent/internal/microvm"
	"github.com/abskrj/velane/services/sandbox-host-agent/internal/network"
	"github.com/abskrj/velane/services/sandbox-host-agent/internal/resources"
)

type fakeRuntime struct{ started, configured, destroyed int }

func (f *fakeRuntime) Start(context.Context, *microvm.Runtime, microvm.JailerSpec) error {
	f.started++
	return nil
}
func (f *fakeRuntime) ConfigureInitial(context.Context, string, microvm.InitialConfig) error {
	f.configured++
	return nil
}
func (f *fakeRuntime) Destroy(context.Context, string) error { f.destroyed++; return nil }

type fakeCgroup struct{ created, removed int }

func (f *fakeCgroup) Create(string, resources.CgroupLimits) (string, error) {
	f.created++
	return "/cgroup/s", nil
}
func (f *fakeCgroup) Remove(string) error { f.removed++; return nil }

type fakeDisk struct{ created, removed int }

func (f *fakeDisk) Create(_, _, id string, _ int64) (string, error) {
	f.created++
	return "/disk/" + id, nil
}
func (f *fakeDisk) Remove(string) error { f.removed++; return nil }

type fakeNetwork struct{ created, removed int }

func (f *fakeNetwork) CreateDefaultDeny(context.Context, network.SandboxNetwork) error {
	f.created++
	return nil
}
func (f *fakeNetwork) Remove(context.Context, string) error { f.removed++; return nil }

type fakeWatchdog struct{ delivered int }

func (f *fakeWatchdog) Deliver(context.Context, json.RawMessage) error { f.delivered++; return nil }

func validCreatePayload() LifecyclePayload {
	return LifecyclePayload{SandboxID: "sandbox1", JailerUID: 1001, JailerGID: 1001, Network: network.SandboxNetwork{SandboxID: "sandbox1", Namespace: "velane-sandbox1", TapName: "tap0", HostVeth: "veth0"}, SignedLeaseGrant: json.RawMessage(`{"signature":"signed"}`), Resources: ResourceRequest{CPUQuotaMicros: 1000, CPUPeriodMicros: 1000, MemoryMaxBytes: 1 << 20, PidsMax: 32}, Machine: MachineRequest{VCPUCount: 1, MemoryMB: 128}, Boot: BootRequest{KernelPath: "/kernel"}, MutableDrives: []MutableDrive{{ID: "data", Size: 1024}}, ImmutableDrives: []ImmutableDrive{{ID: "root", Path: "/rootfs", Root: true}}}
}
func TestProvisionerRejectsMissingPrerequisiteBeforeSideEffects(t *testing.T) {
	vm, cg, disk, net, wd := &fakeRuntime{}, &fakeCgroup{}, &fakeDisk{}, &fakeNetwork{}, &fakeWatchdog{}
	p := Provisioner{RuntimeRoot: "/jails", DiskRoot: "/disks", JailerBinary: "/jailer", FirecrackerBinary: "/firecracker", VM: vm, Cgroups: cg, Disks: disk, Network: net, Watchdog: wd}
	payload := validCreatePayload()
	payload.SignedLeaseGrant = nil
	if err := p.Create(context.Background(), payload); err == nil {
		t.Fatal("create accepted missing signed lease")
	}
	if vm.started != 0 || cg.created != 0 || disk.created != 0 || net.created != 0 || wd.delivered != 0 {
		t.Fatalf("prerequisite failure had side effects: vm=%d cgroup=%d disk=%d net=%d watchdog=%d", vm.started, cg.created, disk.created, net.created, wd.delivered)
	}
}
func TestProvisionerCreatesWithAllDrivers(t *testing.T) {
	vm, cg, disk, net, wd := &fakeRuntime{}, &fakeCgroup{}, &fakeDisk{}, &fakeNetwork{}, &fakeWatchdog{}
	p := Provisioner{RuntimeRoot: "/jails", DiskRoot: "/disks", JailerBinary: "/jailer", FirecrackerBinary: "/firecracker", VM: vm, Cgroups: cg, Disks: disk, Network: net, Watchdog: wd}
	if err := p.Create(context.Background(), validCreatePayload()); err != nil {
		t.Fatal(err)
	}
	if vm.started != 1 || vm.configured != 1 || cg.created != 1 || disk.created != 1 || net.created != 1 || wd.delivered != 1 {
		t.Fatalf("unexpected create calls: vm=%+v cgroup=%+v disk=%+v net=%+v watchdog=%+v", vm, cg, disk, net, wd)
	}
}
func TestExecutorPassesCreateToDriver(t *testing.T) {
	called := false
	executor := LifecycleExecutor{VM: &fakeVM{}, Create: func(_ context.Context, p LifecyclePayload) error { called = p.SandboxID == "sandbox1"; return nil }}
	body, err := json.Marshal(validCanonicalCreatePayload())
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.Execute(context.Background(), controlCommand("CreateSandbox", body)); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("executor did not invoke create driver")
	}
}

func validCanonicalCreatePayload() controlplane.LifecyclePayloadV1 {
	const digest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	artifact := controlplane.ArtifactRefV1{ObjectRef: "objects/artifact", Digest: digest, SizeBytes: 1}
	return controlplane.LifecyclePayloadV1{
		SchemaVersion: "v1",
		Command:       "CreateSandbox",
		SandboxID:     "sandbox1",
		OperationID:   "operation1",
		Generation:    1,
		Allocation: controlplane.AllocationPayloadV1{
			ID:              "allocation1",
			HostID:          "host1",
			HostIncarnation: 1,
			FenceEpoch:      1,
		},
		Resources: controlplane.LifecycleResourcesV1{
			VCPU:            1,
			MemoryMB:        128,
			CPUQuotaMicros:  1000,
			CPUPeriodMicros: 1000,
			MemoryMaxBytes:  1 << 20,
			PidsMax:         32,
		},
		Machine: controlplane.MachinePayloadV1{
			MachineConfig:         json.RawMessage(`{"vcpu_count":1}`),
			DeviceTopology:        json.RawMessage(`{"drives":2}`),
			MachineTopologyDigest: "9124b57112bc0ee3f8344183aacbad36ac5c0146ac608efcd4bab58375f40135",
			DeviceTopologyDigest:  "13d2f9e80113e12f4ec61f43c07f437979df3a08f9e7a9b40c05313890fcfa84",
		},
		Guest: controlplane.GuestArtifactsV1{Kernel: artifact, Rootfs: artifact, Init: artifact},
		Drives: []controlplane.LifecycleDriveV1{
			{ID: "root", Root: true, Order: 0, SizeBytes: 1024, Artifact: &artifact},
			{ID: "data", Mutable: true, Order: 1, SizeBytes: 1024},
		},
		Network:          json.RawMessage(`{"mode":"default-deny"}`),
		Vsock:            json.RawMessage(`{"cid":3}`),
		Jailer:           controlplane.JailerPayloadV1{UID: 1001, GID: 1001},
		GuestImageDigest: digest,
		Lineage:          controlplane.RestoreLineageV1{VMRestoreDescriptorDigest: digest},
		WatchdogGrant:    json.RawMessage(`{"signature":"signed"}`),
	}
}

type fakeVM struct{}

func (*fakeVM) Pause(context.Context, string) error  { return errors.New("unexpected") }
func (*fakeVM) Resume(context.Context, string) error { return errors.New("unexpected") }
func (*fakeVM) CreateFullSnapshot(context.Context, microvm.SnapshotRequest) error {
	return errors.New("unexpected")
}
func (*fakeVM) LoadSnapshot(context.Context, microvm.RestoreRequest) error {
	return errors.New("unexpected")
}
func (*fakeVM) Destroy(context.Context, string) error { return nil }
func controlCommand(kind string, payload json.RawMessage) controlplane.Command {
	return controlplane.Command{Kind: kind, Payload: payload, AllocationID: "allocation1", HostIncarnation: 1, FenceEpoch: 1}
}
