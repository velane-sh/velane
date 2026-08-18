package placement

import "context"

type Request struct {
	SandboxID, TenantID, SnapshotID, VMRestoreDescriptorDigest string
	VCPU, MemoryMB                                             int
	DiskBytes, StagingBytes                                    int64
}
type Outcome string

const (
	Placed               Outcome = "placed"
	NeedsCapacity        Outcome = "needs_capacity"
	NoCompatibleCapacity Outcome = "no_compatible_capacity"
	QuotaExceeded        Outcome = "quota_exceeded"
)

type Result struct {
	Outcome      Outcome
	AllocationID string
	HostID       string
}
type Scheduler interface {
	Place(context.Context, Request) (Result, error)
}
