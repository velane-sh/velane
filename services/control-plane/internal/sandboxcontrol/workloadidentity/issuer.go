package workloadidentity

import (
	"context"
	"time"
)

type Claims struct {
	SandboxID, AllocationID     string
	HostIncarnation, FenceEpoch int64
	ExpiresAt                   time.Time
}
type Issuer interface {
	Issue(context.Context, Claims) (string, error)
}
type Validator interface {
	Validate(context.Context, string) (Claims, error)
}
