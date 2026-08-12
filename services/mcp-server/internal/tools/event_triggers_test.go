package tools

import (
	"context"
	"github.com/abskrj/velane/services/mcp-server/internal/controlplane"
	"testing"
)

func TestEventTriggerToolsRegistered(t *testing.T) {
	r := NewRegistry(controlplane.New("http://control-plane.invalid"))
	names := map[string]bool{}
	for _, tool := range r.List() {
		names[tool.Name] = true
	}
	for _, name := range []string{"get_event_trigger_docs", "list_integration_event_models", "list_workflow_triggers", "create_workflow_trigger", "update_workflow_trigger", "delete_workflow_trigger"} {
		if !names[name] {
			t.Errorf("tool %q not registered", name)
		}
	}
}
func TestGetEventTriggerDocsReturnsEnvelopeAndRequirements(t *testing.T) {
	r := NewRegistry(controlplane.New("http://control-plane.invalid"))
	result, err := r.Call(context.Background(), "Bearer vl_test", "get_event_trigger_docs", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	out, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type %T", result)
	}
	if out["resource"] != "velane://runtime/event-triggers" {
		t.Errorf("resource=%v", out["resource"])
	}
	if _, ok := out["envelope"].(map[string]any); !ok {
		t.Fatal("missing structured envelope")
	}
	requirements, ok := out["requirements"].([]string)
	if !ok || len(requirements) < 5 {
		t.Fatalf("requirements=%#v", out["requirements"])
	}
}
