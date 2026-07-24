package models

import "time"

// InvocationStatus tracks the lifecycle of a single snippet execution.
type InvocationStatus string

const (
	InvocationPending   InvocationStatus = "pending"
	InvocationRunning   InvocationStatus = "running"
	InvocationCompleted InvocationStatus = "completed"
	InvocationFailed    InvocationStatus = "failed"
	InvocationTimeout   InvocationStatus = "timeout"
	InvocationOOMKilled InvocationStatus = "oom_killed"
)

// Invocation records a single execution of a snippet version.
type Invocation struct {
	ID           string           `json:"id"`
	SnippetID    string           `json:"snippet_id"`
	VersionID    string           `json:"version_id"`
	Environment  string           `json:"environment"`
	TenantID     string           `json:"tenant_id"`
	Status       InvocationStatus `json:"status"`
	InputPayload string           `json:"input_payload"` // raw JSON
	InputRef     string           `json:"input_ref,omitempty"`
	Output       string           `json:"output"` // raw JSON
	OutputRef    string           `json:"output_ref,omitempty"`
	Error        string           `json:"error,omitempty"`
	Stderr       string           `json:"stderr"`
	StderrRef    string           `json:"stderr_ref,omitempty"`
	DurationMs   int              `json:"duration_ms"`
	PeakMemoryMB int              `json:"peak_memory_mb"`
	CPUMs        int              `json:"cpu_ms"`
	CreatedAt    time.Time        `json:"created_at"`
	CompletedAt  *time.Time       `json:"completed_at,omitempty"`
	CallbackURL  string           `json:"callback_url,omitempty"`
	InvokeMode   string           `json:"invoke_mode"` // "sync" | "async" | "stream"
	PayloadState string           `json:"payload_state"`
}
