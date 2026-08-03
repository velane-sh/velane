package resources

import (
	"context"
	"strings"
	"testing"
)

func TestEventTriggerResourceListedAndReadable(t *testing.T) {
	r := NewRegistry(nil)
	found := false
	for _, item := range r.List() {
		if item.URI == "velane://runtime/event-triggers" {
			found = true
		}
	}
	if !found {
		t.Fatal("event trigger resource not listed")
	}
	content, err := r.Read(context.Background(), "", "velane://runtime/event-triggers")
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != 1 {
		t.Fatalf("content length=%d", len(content))
	}
	for _, required := range []string{"sync.records.changed", "added|updated|deleted", "at least once", "integration(input.provider", "representative envelope"} {
		if !strings.Contains(content[0].Text, required) {
			t.Errorf("documentation missing %q", required)
		}
	}
}
func TestRuntimeContractLinksEventTriggerGuide(t *testing.T) {
	if !strings.Contains(runtimeContract, "velane://runtime/event-triggers") {
		t.Fatal("runtime contract does not link event trigger guide")
	}
}
