package reconcile

import (
	"context"
	"github.com/abskrj/velane/services/control-plane/internal/sandboxcontrol"
)

// ClaimOperations is deliberately persisted/lease based; command dispatch is added by the host protocol owner.
func ClaimOperations(context.Context, *sandboxcontrol.Service) error { return nil }
