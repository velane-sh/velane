package domain

import "fmt"

func valid[T comparable](from, to T, table map[T]map[T]bool) error {
	if from == to || table[from][to] {
		return nil
	}
	return fmt.Errorf("invalid transition from %v to %v", from, to)
}
func ValidateSandboxTransition(from, to SandboxObservedState) error {
	return valid(from, to, map[SandboxObservedState]map[SandboxObservedState]bool{
		SandboxObservedPending: {SandboxObservedAwaitingCapacity: true, SandboxObservedProvisioning: true, SandboxObservedDeleting: true, SandboxObservedFailed: true}, SandboxObservedAwaitingCapacity: {SandboxObservedProvisioning: true, SandboxObservedRestoring: true, SandboxObservedDeleting: true, SandboxObservedFailed: true}, SandboxObservedProvisioning: {SandboxObservedBootstrapping: true, SandboxObservedFailed: true, SandboxObservedDeleting: true}, SandboxObservedBootstrapping: {SandboxObservedRunning: true, SandboxObservedFailed: true, SandboxObservedDeleting: true}, SandboxObservedRunning: {SandboxObservedSnapshotting: true, SandboxObservedStopping: true, SandboxObservedRecovering: true, SandboxObservedDeleting: true, SandboxObservedFailed: true}, SandboxObservedSnapshotting: {SandboxObservedRunning: true, SandboxObservedStopping: true, SandboxObservedRecovering: true, SandboxObservedFailed: true}, SandboxObservedStopping: {SandboxObservedStopped: true, SandboxObservedFailed: true, SandboxObservedDeleting: true}, SandboxObservedStopped: {SandboxObservedAwaitingCapacity: true, SandboxObservedRestoring: true, SandboxObservedDeleting: true}, SandboxObservedRecovering: {SandboxObservedAwaitingCapacity: true, SandboxObservedRestoring: true, SandboxObservedFailed: true, SandboxObservedDeleting: true}, SandboxObservedRestoring: {SandboxObservedRunning: true, SandboxObservedFailed: true, SandboxObservedStopped: true}, SandboxObservedDeleting: {SandboxObservedFailed: true}, SandboxObservedFailed: {SandboxObservedPending: true, SandboxObservedDeleting: true}})
}
func ValidateOperationTransition(from, to OperationState) error {
	return valid(from, to, map[OperationState]map[OperationState]bool{OperationQueued: {OperationClaimed: true, OperationCancelled: true}, OperationClaimed: {OperationDispatched: true, OperationWaiting: true, OperationSucceeded: true, OperationFailed: true, OperationQueued: true}, OperationDispatched: {OperationWaiting: true, OperationSucceeded: true, OperationFailed: true}, OperationWaiting: {OperationClaimed: true, OperationSucceeded: true, OperationFailed: true, OperationCancelled: true}})
}
func ValidateHostTransition(from, to HostState) error {
	return valid(from, to, map[HostState]map[HostState]bool{HostRegistering: {HostReady: true, HostDraining: true, HostTerminated: true}, HostReady: {HostDraining: true, HostUnreachable: true, HostTerminated: true}, HostDraining: {HostUnreachable: true, HostTerminated: true}, HostUnreachable: {HostDraining: true, HostTerminated: true}})
}
func ValidateSnapshotTransition(from, to SnapshotState) error {
	return valid(from, to, map[SnapshotState]map[SnapshotState]bool{SnapshotRequested: {SnapshotUploading: true, SnapshotFailed: true}, SnapshotUploading: {SnapshotVerifying: true, SnapshotFailed: true}, SnapshotVerifying: {SnapshotReady: true, SnapshotFailed: true}, SnapshotReady: {SnapshotDeleting: true}, SnapshotFailed: {SnapshotDeleting: true}, SnapshotDeleting: {SnapshotDeleted: true}})
}
