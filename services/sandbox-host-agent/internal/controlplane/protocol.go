package controlplane

type CommandKind string

const (
	CommandCreateSandbox       CommandKind = "CreateSandbox"
	CommandCreateFullSnapshot  CommandKind = "CreateFullSnapshot"
	CommandRestoreFullSnapshot CommandKind = "RestoreFullSnapshot"
	CommandDestroy             CommandKind = "Destroy"
)

type LeaseGrant struct {
	SandboxID, AllocationID, HostID, Signature string
	HostIncarnation, FenceEpoch                uint64
	IssuedAtUnixMilli, ExpiresAtUnixMilli      int64
}
