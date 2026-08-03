package handlers

import "testing"

func TestNormalizeTriggerRequestDefaultsAndSorts(t *testing.T) {
	q := triggerRequest{Model: "Case", Environment: "dev"}
	if !normalizeTriggerRequest(&q) {
		t.Fatal("valid request rejected")
	}
	want := []string{"added", "deleted", "updated"}
	for i := range want {
		if q.ChangeTypes[i] != want[i] {
			t.Errorf("change_types[%d]=%q, want %q", i, q.ChangeTypes[i], want[i])
		}
	}
}
func TestNormalizeTriggerRequestValidation(t *testing.T) {
	tests := []struct {
		name string
		q    triggerRequest
	}{{"bad model", triggerRequest{Model: "../../Case", Environment: "dev", ChangeTypes: []string{"added"}}}, {"bad environment", triggerRequest{Model: "Case", Environment: "production", ChangeTypes: []string{"added"}}}, {"unknown change", triggerRequest{Model: "Case", Environment: "dev", ChangeTypes: []string{"created"}}}, {"duplicate change", triggerRequest{Model: "Case", Environment: "dev", ChangeTypes: []string{"added", "added"}}}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if normalizeTriggerRequest(&tc.q) {
				t.Fatal("invalid request accepted")
			}
		})
	}
}
