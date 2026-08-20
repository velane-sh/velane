package domain

import "fmt"

type ErrorCode string

const (
	SandboxNotFound           ErrorCode = "SANDBOX_NOT_FOUND"
	GenerationConflict        ErrorCode = "GENERATION_CONFLICT"
	IdempotencyConflict       ErrorCode = "IDEMPOTENCY_CONFLICT"
	OperationConflict         ErrorCode = "OPERATION_CONFLICT"
	QuotaExceeded             ErrorCode = "QUOTA_EXCEEDED"
	CapacityUnavailable       ErrorCode = "CAPACITY_UNAVAILABLE"
	SnapshotIncompatible      ErrorCode = "SNAPSHOT_INCOMPATIBLE"
	NoRecoverableSnapshot     ErrorCode = "NO_RECOVERABLE_SNAPSHOT"
	SnapshotIncomplete        ErrorCode = "SNAPSHOT_INCOMPLETE"
	HostLeaseLost             ErrorCode = "HOST_LEASE_LOST"
	StaleFence                ErrorCode = "STALE_FENCE"
	RecipeNotReady            ErrorCode = "RECIPE_NOT_READY"
	ProfileUnavailable        ErrorCode = "PROFILE_UNAVAILABLE"
	RecipeProfileIncompatible ErrorCode = "RECIPE_PROFILE_INCOMPATIBLE"
	BootstrapFailed           ErrorCode = "BOOTSTRAP_FAILED"
	CapabilityUnavailable     ErrorCode = "SANDBOX_CAPABILITY_UNAVAILABLE"
)

type Error struct {
	Code      ErrorCode
	Message   string
	Retryable bool
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }
func NewError(code ErrorCode, message string, retryable bool) *Error {
	return &Error{code, message, retryable}
}
