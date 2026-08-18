package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/abskrj/velane/services/sandbox-host-agent/internal/controlplane"
	"github.com/abskrj/velane/services/sandbox-host-agent/internal/reconcile"
)

type Executor interface {
	Execute(context.Context, controlplane.Command) error
}
type Agent struct {
	Journal  *reconcile.Journal
	Executor Executor
}

func (a Agent) Handle(ctx context.Context, command controlplane.Command) error {
	if a.Journal == nil || a.Executor == nil {
		return fmt.Errorf("host agent is not configured")
	}
	var payload struct {
		SandboxID string `json:"sandbox_id"`
	}
	_ = json.Unmarshal(command.Payload, &payload)
	if err := a.Journal.Accept(reconcile.CommandFence{SandboxID: payload.SandboxID, AllocationID: command.AllocationID, Incarnation: command.HostIncarnation, FenceEpoch: command.FenceEpoch, Sequence: command.Sequence, CommandID: command.ID}); err != nil {
		return err
	}
	return a.Executor.Execute(ctx, command)
}
