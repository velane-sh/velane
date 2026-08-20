package network

import "context"

type SandboxNetwork struct{ SandboxID, Namespace, TapName, HostVeth, IP, MAC string }
type Manager interface {
	CreateDefaultDeny(context.Context, SandboxNetwork) error
	Remove(context.Context, string) error
}
