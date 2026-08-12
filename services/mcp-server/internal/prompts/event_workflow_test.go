package prompts

import (
	"strings"
	"testing"
)

func TestCreateEventWorkflowPromptSequence(t *testing.T) {
	r := NewRegistry()
	listed := false
	for _, p := range r.List() {
		if p.Name == "create_event_workflow" {
			listed = true
		}
	}
	if !listed {
		t.Fatal("create_event_workflow not listed")
	}
	_, messages, err := r.Get("create_event_workflow", map[string]any{"connection_id": "conn-1", "goal": "mirror cases", "language": "python", "env": "staging"})
	if err != nil {
		t.Fatal(err)
	}
	text := messages[0].Content.Text
	steps := []string{"list_integration_event_models", "representative event envelope", "Publish the exact", "create_workflow_trigger", "explicit confirmation"}
	position := -1
	for _, step := range steps {
		next := strings.Index(text, step)
		if next < 0 {
			t.Fatalf("prompt missing %q", step)
		}
		if next <= position {
			t.Fatalf("step %q is out of order", step)
		}
		position = next
	}
	for _, value := range []string{"conn-1", "mirror cases", "python", "staging"} {
		if !strings.Contains(text, value) {
			t.Errorf("prompt missing argument %q", value)
		}
	}
}
