// Package domain contains provider-neutral durable sandbox invariants.
package domain

type SandboxDesiredState string
type SandboxObservedState string
type OperationKind string
type OperationState string
type HostState string
type AllocationState string
type SnapshotState string
type CommandState string

const (
	SandboxDesiredRunning           SandboxDesiredState  = "running"
	SandboxDesiredStopped           SandboxDesiredState  = "stopped"
	SandboxDesiredDeleted           SandboxDesiredState  = "deleted"
	SandboxObservedPending          SandboxObservedState = "pending"
	SandboxObservedAwaitingCapacity SandboxObservedState = "awaiting_capacity"
	SandboxObservedProvisioning     SandboxObservedState = "provisioning"
	SandboxObservedBootstrapping    SandboxObservedState = "bootstrapping"
	SandboxObservedRestoring        SandboxObservedState = "restoring"
	SandboxObservedRunning          SandboxObservedState = "running"
	SandboxObservedSnapshotting     SandboxObservedState = "snapshotting"
	SandboxObservedStopping         SandboxObservedState = "stopping"
	SandboxObservedStopped          SandboxObservedState = "stopped"
	SandboxObservedRecovering       SandboxObservedState = "recovering"
	SandboxObservedDeleting         SandboxObservedState = "deleting"
	SandboxObservedFailed           SandboxObservedState = "failed"
)
const (
	OperationRecipeBuild    OperationKind   = "recipe_build"
	OperationCreate         OperationKind   = "create"
	OperationStart          OperationKind   = "start"
	OperationStop           OperationKind   = "stop"
	OperationRestart        OperationKind   = "restart"
	OperationSnapshot       OperationKind   = "snapshot"
	OperationRestore        OperationKind   = "restore"
	OperationRecover        OperationKind   = "recover"
	OperationDelete         OperationKind   = "delete"
	OperationSnapshotDelete OperationKind   = "snapshot_delete"
	OperationQueued         OperationState  = "queued"
	OperationClaimed        OperationState  = "claimed"
	OperationDispatched     OperationState  = "dispatched"
	OperationWaiting        OperationState  = "waiting"
	OperationSucceeded      OperationState  = "succeeded"
	OperationFailed         OperationState  = "failed"
	OperationCancelled      OperationState  = "cancelled"
	HostRegistering         HostState       = "registering"
	HostReady               HostState       = "ready"
	HostDraining            HostState       = "draining"
	HostUnreachable         HostState       = "unreachable"
	HostTerminated          HostState       = "terminated"
	AllocationReserved      AllocationState = "reserved"
	AllocationActive        AllocationState = "active"
	AllocationReleasing     AllocationState = "releasing"
	AllocationReleased      AllocationState = "released"
	AllocationLost          AllocationState = "lost"
	SnapshotRequested       SnapshotState   = "requested"
	SnapshotUploading       SnapshotState   = "uploading"
	SnapshotVerifying       SnapshotState   = "verifying"
	SnapshotReady           SnapshotState   = "ready"
	SnapshotFailed          SnapshotState   = "failed"
	SnapshotDeleting        SnapshotState   = "deleting"
	SnapshotDeleted         SnapshotState   = "deleted"
	CommandPending          CommandState    = "pending"
	CommandDelivered        CommandState    = "delivered"
	CommandAcknowledged     CommandState    = "acknowledged"
	CommandSucceeded        CommandState    = "succeeded"
	CommandFailed           CommandState    = "failed"
	CommandSuperseded       CommandState    = "superseded"
)

func (s OperationState) Terminal() bool {
	return s == OperationSucceeded || s == OperationFailed || s == OperationCancelled
}
