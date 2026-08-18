package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/abskrj/velane/services/sandbox-host-agent/internal/microvm"
	"github.com/abskrj/velane/services/sandbox-host-agent/internal/network"
	"github.com/abskrj/velane/services/sandbox-host-agent/internal/resources"
)

// Provisioner owns the ordered initial-boot path. Restore never calls this
// type: it is intentionally usable only by CreateSandbox.
type Provisioner struct {
	RuntimeRoot, DiskRoot, JailerBinary, FirecrackerBinary string
	VM                                                     RuntimeStarter
	Cgroups                                                CgroupDriver
	Disks                                                  DiskDriver
	Network                                                network.Manager
	Watchdog                                               LeaseDeliverer
}

type RuntimeStarter interface {
	Start(context.Context, *microvm.Runtime, microvm.JailerSpec) error
	ConfigureInitial(context.Context, string, microvm.InitialConfig) error
	Destroy(context.Context, string) error
}
type CgroupDriver interface {
	Create(string, resources.CgroupLimits) (string, error)
	Remove(string) error
}
type DiskDriver interface {
	Create(root, sandboxID, driveID string, size int64) (string, error)
	Remove(path string) error
}
type LeaseDeliverer interface {
	Deliver(context.Context, json.RawMessage) error
}

type MutableDrive struct {
	ID   string `json:"id"`
	Size int64  `json:"size_bytes"`
	Root bool   `json:"root"`
}
type ImmutableDrive struct {
	ID, Path string
	Root     bool
}
type MachineRequest struct {
	VCPUCount, MemoryMB int
	SMT                 bool
}
type BootRequest struct{ KernelPath, Args string }
type ResourceRequest struct {
	CPUQuotaMicros  int64  `json:"cpu_quota_micros"`
	CPUPeriodMicros int64  `json:"cpu_period_micros"`
	MemoryMaxBytes  int64  `json:"memory_max_bytes"`
	PidsMax         int64  `json:"pids_max"`
	IOMax           string `json:"io_max"`
}

// Create validates every dependency and immutable command input before the
// first side effect. Each later failure rolls all previously created resources
// back to a default-deny/non-running state.
func (p Provisioner) Create(ctx context.Context, command LifecyclePayload) (err error) {
	if err := p.validate(command); err != nil {
		return err
	}
	if command.MutableDrivePaths == nil {
		command.MutableDrivePaths = map[string]string{}
	}
	limits := resources.CgroupLimits{CPUQuotaMicros: command.Resources.CPUQuotaMicros, CPUPeriodMicros: command.Resources.CPUPeriodMicros, MemoryMaxBytes: command.Resources.MemoryMaxBytes, PidsMax: command.Resources.PidsMax, IOMax: command.Resources.IOMax}
	cgroupPath, err := p.Cgroups.Create(command.SandboxID, limits)
	if err != nil {
		return fmt.Errorf("create sandbox cgroup: %w", err)
	}
	createdDisks := make([]string, 0, len(command.MutableDrives))
	defer func() {
		if err != nil {
			for _, path := range createdDisks {
				_ = p.Disks.Remove(path)
			}
			_ = p.Cgroups.Remove(cgroupPath)
		}
	}()
	for _, drive := range command.MutableDrives {
		path, createErr := p.Disks.Create(p.DiskRoot, command.SandboxID, drive.ID, drive.Size)
		if createErr != nil {
			err = fmt.Errorf("create mutable drive %q: %w", drive.ID, createErr)
			return err
		}
		createdDisks = append(createdDisks, path)
		command.MutableDrivePaths[drive.ID] = path
	}
	if err = p.Network.CreateDefaultDeny(ctx, command.Network); err != nil {
		return fmt.Errorf("create default-deny sandbox network: %w", err)
	}
	networkCreated := true
	defer func() {
		if err != nil && networkCreated {
			_ = p.Network.Remove(context.Background(), command.SandboxID)
		}
	}()
	jailPath := filepath.Join(p.RuntimeRoot, command.SandboxID)
	runtime := &microvm.Runtime{SandboxID: command.SandboxID, JailPath: jailPath, APISocket: filepath.Join(jailPath, "run", "firecracker.sock"), Jailer: &microvm.ProcessJailer{Binary: p.JailerBinary, FirecrackerBinary: p.FirecrackerBinary, ID: command.SandboxID, ChrootBase: p.RuntimeRoot}}
	if err = p.VM.Start(ctx, runtime, microvm.JailerSpec{UID: command.JailerUID, GID: command.JailerGID, ChrootBase: p.RuntimeRoot, CgroupPath: cgroupPath, NetworkNamespace: command.Network.Namespace, SeccompEnabled: true}); err != nil {
		return fmt.Errorf("start initial jailed Firecracker: %w", err)
	}
	started := true
	defer func() {
		if err != nil && started {
			_ = p.VM.Destroy(context.Background(), command.SandboxID)
		}
	}()
	drives := make([]microvm.DriveConfig, 0, len(command.ImmutableDrives)+len(command.MutableDrives))
	for _, drive := range command.ImmutableDrives {
		drives = append(drives, microvm.DriveConfig{ID: drive.ID, PathOnHost: drive.Path, Root: drive.Root, ReadOnly: true})
	}
	for _, drive := range command.MutableDrives {
		drives = append(drives, microvm.DriveConfig{ID: drive.ID, PathOnHost: command.MutableDrivePaths[drive.ID], Root: drive.Root, ReadOnly: false})
	}
	if err = p.VM.ConfigureInitial(ctx, command.SandboxID, microvm.InitialConfig{VCPUCount: command.Machine.VCPUCount, MemoryMB: command.Machine.MemoryMB, SMT: command.Machine.SMT, KernelPath: command.Boot.KernelPath, BootArgs: command.Boot.Args, Drives: drives}); err != nil {
		return fmt.Errorf("configure initial Firecracker: %w", err)
	}
	if err = p.Watchdog.Deliver(ctx, command.SignedLeaseGrant); err != nil {
		return fmt.Errorf("open sandbox network with signed watchdog lease: %w", err)
	}
	return nil
}

func (p Provisioner) validate(command LifecyclePayload) error {
	if p.VM == nil || p.Cgroups == nil || p.Disks == nil || p.Network == nil || p.Watchdog == nil {
		return errors.New("initial sandbox provisioning driver is incomplete; host is not placeable")
	}
	if p.RuntimeRoot == "" || p.DiskRoot == "" || p.JailerBinary == "" || p.FirecrackerBinary == "" {
		return errors.New("initial sandbox runtime paths or binaries are unavailable; host is not placeable")
	}
	if command.SandboxID == "" || filepath.Base(command.SandboxID) != command.SandboxID || command.JailerUID <= 0 || command.JailerGID <= 0 || command.Network.Namespace != "velane-"+command.SandboxID || command.Network.SandboxID != command.SandboxID || command.Network.TapName == "" || command.Network.HostVeth == "" || len(command.SignedLeaseGrant) == 0 {
		return errors.New("sandbox create command lacks safe runtime identity, network, or signed watchdog lease")
	}
	if command.Resources.CPUQuotaMicros <= 0 || command.Resources.CPUPeriodMicros <= 0 || command.Resources.MemoryMaxBytes <= 0 || command.Resources.PidsMax <= 0 || len(command.MutableDrives) == 0 || command.Machine.VCPUCount <= 0 || command.Machine.MemoryMB <= 0 || command.Boot.KernelPath == "" {
		return errors.New("sandbox create command lacks immutable resource, machine, boot, or mutable drive layout")
	}
	for _, drive := range command.MutableDrives {
		if drive.ID == "" || filepath.Base(drive.ID) != drive.ID || drive.Size <= 0 {
			return errors.New("sandbox create command contains an invalid mutable drive")
		}
	}
	rootCount := 0
	for _, drive := range command.ImmutableDrives {
		if drive.ID == "" || drive.Path == "" {
			return errors.New("sandbox create command contains an invalid immutable drive")
		}
		if drive.Root {
			rootCount++
		}
	}
	for _, drive := range command.MutableDrives {
		if drive.Root {
			rootCount++
		}
	}
	if rootCount != 1 {
		return errors.New("sandbox create command requires exactly one root drive")
	}
	return nil
}
