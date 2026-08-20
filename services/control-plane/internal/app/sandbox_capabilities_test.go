package app

import (
	"context"
	"testing"

	"github.com/abskrj/velane/services/control-plane/internal/sandboxcontrol"
)

func TestSandboxCapabilitiesRejectUnimplementedBuilderEndpoint(t *testing.T) {
	provider := sandboxcontrol.NewOperationalProvider(sandboxcontrol.OperationalDependencies{ControlEnabled: true})
	capabilities := provider.Capabilities(context.Background())
	if capabilities.SandboxImageRecipes {
		t.Fatal("advertised recipe builds without authenticated dispatch and artifact readiness")
	}
}
