package worker

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSplitEventRecords_CountLimitAndOrdering(t *testing.T) {
	records := make([]eventRecord, 201)
	for i := range records {
		records[i] = eventRecord{Action: "updated", Data: map[string]any{"index": i}}
	}
	batches := splitEventRecords(records)
	if len(batches) != 3 {
		t.Fatalf("batches=%d, want 3", len(batches))
	}
	for i, want := range []int{100, 100, 1} {
		if len(batches[i]) != want {
			t.Errorf("batch %d length=%d, want %d", i, len(batches[i]), want)
		}
	}
	got := 0
	for _, batch := range batches {
		for _, record := range batch {
			if record.Data["index"] != got {
				t.Fatalf("ordering at %d: got %v", got, record.Data["index"])
			}
			got++
		}
	}
}

func TestSplitEventRecords_ByteLimit(t *testing.T) {
	records := []eventRecord{{Action: "added", Data: map[string]any{"value": strings.Repeat("a", 300*1024)}}, {Action: "updated", Data: map[string]any{"value": strings.Repeat("b", 300*1024)}}}
	batches := splitEventRecords(records)
	if len(batches) != 2 {
		t.Fatalf("batches=%d, want 2", len(batches))
	}
	for i, batch := range batches {
		raw, err := json.Marshal(batch)
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) > 512*1024 {
			t.Errorf("batch %d size=%d exceeds limit", i, len(raw))
		}
	}
}

func TestSplitEventRecords_Empty(t *testing.T) {
	if got := splitEventRecords(nil); len(got) != 0 {
		t.Fatalf("got %d batches, want 0", len(got))
	}
}

func TestFilterEventRecords(t *testing.T) {
	records := []eventRecord{{Action: "added"}, {Action: "updated"}, {Action: "deleted"}, {Action: "updated"}}
	got := filterEventRecords(records, []string{"updated", "deleted"})
	want := []string{"updated", "deleted", "updated"}
	if len(got) != len(want) {
		t.Fatalf("records=%d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Action != want[i] {
			t.Errorf("record %d=%q, want %q", i, got[i].Action, want[i])
		}
	}
}

func TestPredatesActivation(t *testing.T) {
	activation := time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)
	if !predatesActivation("2026-08-03T09:59:59Z", &activation) {
		t.Error("older cursor should be suppressed")
	}
	if predatesActivation("2026-08-03T10:00:00Z", &activation) {
		t.Error("equal cursor should be delivered")
	}
	if predatesActivation("not-a-date", &activation) {
		t.Error("invalid cursor should not be silently suppressed")
	}
	if predatesActivation("2020-01-01T00:00:00Z", nil) {
		t.Error("nil activation should not suppress")
	}
}

func TestBuildEventEnvelope(t *testing.T) {
	records := []eventRecord{{Action: "deleted", Data: map[string]any{"id": "case-1"}}}
	got := buildEventEnvelope("receipt-1", "salesforce", "conn-1", "default", "Case", "2026-08-03T10:00:00Z", records, 2, 3)
	if got["source"] != "nango" || got["id"] != "receipt-1" {
		t.Fatalf("identity=%v", got)
	}
	batch := got["batch"].(map[string]any)
	if batch["index"] != 2 || batch["count"] != 3 || batch["is_last"] != false {
		t.Errorf("batch=%v", batch)
	}
	event := got["event"].(map[string]any)
	if event["type"] != "sync.records.changed" || event["model"] != "Case" {
		t.Errorf("event=%v", event)
	}
	connection := got["connection"].(map[string]any)
	if connection["id"] != "conn-1" || connection["alias"] != "default" {
		t.Errorf("connection=%v", connection)
	}
}
