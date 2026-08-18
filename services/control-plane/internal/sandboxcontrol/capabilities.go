package sandboxcontrol

import (
	"context"

	"github.com/abskrj/velane/services/control-plane/internal/sandboxcontrol/keywrap"
)

// Capability identifies a mutation path. Keeping admission action-specific
// prevents a working create path from accidentally advertising snapshot or
// image-build support.
type Capability string

const (
	CapabilitySandboxCreate         Capability = "sandbox_create"
	CapabilitySandboxStart          Capability = "sandbox_start"
	CapabilitySandboxDelete         Capability = "sandbox_delete"
	CapabilitySandboxCheckpoint     Capability = "sandbox_checkpoint"
	CapabilitySandboxOperationRetry Capability = "sandbox_operation_retry"
	CapabilityImageRecipeMutation   Capability = "sandbox_image_recipe_mutation"
)

// Capabilities is the public, coarse-grained discovery representation. Actual
// admission remains action-specific through Provider.Available.
type Capabilities struct {
	Sandboxes           bool
	SandboxProfiles     bool
	SandboxImageRecipes bool
	SandboxOperations   bool
	SandboxSnapshots    bool
	SandboxEvents       bool
	SandboxLogs         bool
}

// Provider is the single source of truth for discovery and mutation admission.
type Provider interface {
	Capabilities(context.Context) Capabilities
	Available(context.Context, Capability) bool
}

// StaticProvider is useful for fail-closed defaults and focused tests.
type StaticProvider struct{ Value Capabilities }

func (p StaticProvider) Capabilities(context.Context) Capabilities { return p.Value }
func (p StaticProvider) Available(_ context.Context, capability Capability) bool {
	switch capability {
	case CapabilitySandboxCreate, CapabilitySandboxStart, CapabilitySandboxDelete:
		return p.Value.Sandboxes
	case CapabilitySandboxCheckpoint:
		return p.Value.SandboxSnapshots
	case CapabilitySandboxOperationRetry:
		return p.Value.SandboxOperations
	case CapabilityImageRecipeMutation:
		return p.Value.SandboxImageRecipes
	default:
		return false
	}
}

// HostCommandProbe reports command support from currently registered, leased
// hosts. A configured listener is deliberately not sufficient.
type HostCommandProbe func(context.Context) (map[string]bool, error)

// ImageBuilder is implemented only by a dispatch provider that can publish and
// verify signed immutable artifacts. No endpoint string can satisfy it.
type ImageBuilder interface {
	Operational(context.Context) bool
}

type OperationalDependencies struct {
	ControlEnabled    bool
	HostEnrollment    bool
	LifecyclePayloads bool
	CommandDispatch   bool
	WatchdogSigner    bool
	SnapshotStore     bool
	SnapshotKeyWrap   keywrap.Wrapper
	HostCommands      HostCommandProbe
	ImageBuilder      ImageBuilder
}

type OperationalProvider struct{ dependencies OperationalDependencies }

func NewOperationalProvider(dependencies OperationalDependencies) *OperationalProvider {
	return &OperationalProvider{dependencies: dependencies}
}

func (p *OperationalProvider) hostCommands(ctx context.Context) map[string]bool {
	if p == nil || p.dependencies.HostCommands == nil {
		return nil
	}
	commands, err := p.dependencies.HostCommands(ctx)
	if err != nil {
		return nil
	}
	return commands
}

func (p *OperationalProvider) baseReady() bool {
	return p != nil && p.dependencies.ControlEnabled && p.dependencies.HostEnrollment &&
		p.dependencies.LifecyclePayloads && p.dependencies.CommandDispatch &&
		p.dependencies.WatchdogSigner
}

func (p *OperationalProvider) snapshotReady() bool {
	return p.baseReady() && p.dependencies.SnapshotStore && p.dependencies.SnapshotKeyWrap != nil
}

func (p *OperationalProvider) Available(ctx context.Context, capability Capability) bool {
	return p.available(ctx, capability, p.hostCommands(ctx))
}

func (p *OperationalProvider) available(ctx context.Context, capability Capability, commands map[string]bool) bool {
	switch capability {
	case CapabilitySandboxCreate:
		return p.baseReady() && commands["CreateSandbox"] && commands["Destroy"]
	case CapabilitySandboxStart:
		return p.baseReady() && commands["RestoreFullSnapshot"] && commands["Destroy"]
	case CapabilitySandboxDelete:
		return p.baseReady() && commands["Destroy"]
	case CapabilitySandboxCheckpoint:
		return p.snapshotReady() && commands["CreateFullSnapshot"] && commands["RestoreFullSnapshot"] && commands["Destroy"]
	case CapabilitySandboxOperationRetry:
		// The retry endpoint can recreate any lifecycle operation. Admit it only
		// when every lifecycle path is executable.
		return p.snapshotReady() && commands["CreateSandbox"] && commands["CreateFullSnapshot"] && commands["RestoreFullSnapshot"] && commands["Destroy"]
	case CapabilityImageRecipeMutation:
		return p != nil && p.dependencies.ControlEnabled && p.dependencies.ImageBuilder != nil && p.dependencies.ImageBuilder.Operational(ctx)
	default:
		return false
	}
}

func (p *OperationalProvider) Capabilities(ctx context.Context) Capabilities {
	// Discovery is derived from one host-readiness snapshot so one response
	// cannot mix command observations from different database probes.
	commands := p.hostCommands(ctx)
	create := p.available(ctx, CapabilitySandboxCreate, commands)
	start := p.available(ctx, CapabilitySandboxStart, commands)
	deleteReady := p.available(ctx, CapabilitySandboxDelete, commands)
	baseReads := p != nil && p.dependencies.ControlEnabled
	return Capabilities{
		Sandboxes:           create || start || deleteReady,
		SandboxProfiles:     baseReads,
		SandboxImageRecipes: p.available(ctx, CapabilityImageRecipeMutation, commands),
		SandboxOperations:   p.available(ctx, CapabilitySandboxOperationRetry, commands),
		SandboxSnapshots:    p.available(ctx, CapabilitySandboxCheckpoint, commands),
		SandboxEvents:       baseReads,
		SandboxLogs:         baseReads,
	}
}

// AvailableActions returns only actions admitted by the same provider used by
// mutation handlers.
func AvailableActions(ctx context.Context, provider Provider, observed string) []string {
	if provider == nil {
		return nil
	}
	switch observed {
	case "running":
		if !provider.Available(ctx, CapabilitySandboxCheckpoint) {
			return nil
		}
		return []string{"stop", "restart", "snapshot"}
	case "stopped":
		actions := make([]string, 0, 2)
		if provider.Available(ctx, CapabilitySandboxStart) {
			actions = append(actions, "start")
		}
		if provider.Available(ctx, CapabilitySandboxDelete) {
			actions = append(actions, "delete")
		}
		return actions
	default:
		return nil
	}
}
