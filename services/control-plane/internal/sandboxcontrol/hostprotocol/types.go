// Package hostprotocol declares cloud-neutral private host wire contracts.
package hostprotocol

import "time"

type FencedRef struct {
	AllocationID    string `json:"allocation_id"`
	HostIncarnation int64  `json:"host_incarnation"`
	FenceEpoch      int64  `json:"fence_epoch"`
}
type RegisterRequest struct {
	PoolID, BootID, HostCompatibilityKey string `json:"-"`
	Challenge                            string `json:"challenge"`
	VMRestoreCapabilities                map[string]bool
	TotalVCPU, TotalMemoryMB             int
	TotalDiskBytes, TotalStagingBytes    int64
}
type HeartbeatRequest struct {
	FencedRef
	Sequence        int64     `json:"sequence"`
	WatchdogHealthy bool      `json:"watchdog_healthy"`
	SentAt          time.Time `json:"sent_at"`
}
type Command struct {
	ID, Kind string
	FencedRef
	Sequence int64
	Payload  []byte
}
type CommandResult struct {
	FencedRef
	CommandID                   string
	Succeeded                   bool
	FailureCode, FailureMessage string
	Result                      []byte
}
