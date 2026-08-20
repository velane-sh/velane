package models

import (
	"encoding/json"
	"time"
)

// Sandbox is the durable internal record. Public API uses SandboxDTO to avoid
// exposing fencing, compatibility, host, or provider fields.
type Sandbox struct {
	ID                        string    `json:"id"`
	TenantID                  string    `json:"tenant_id"`
	Name                      string    `json:"name"`
	RecipeVersionID           string    `json:"recipe_version_id"`
	ProfileVersionID          string    `json:"profile_version_id"`
	VMRestoreDescriptorDigest string    `json:"-"`
	DesiredState              string    `json:"desired_state"`
	ObservedState             string    `json:"observed_state"`
	Generation                int64     `json:"generation"`
	ObservedGeneration        int64     `json:"observed_generation"`
	FenceEpoch                int64     `json:"-"`
	EverBooted                bool      `json:"-"`
	LatestSnapshotID          *string   `json:"latest_snapshot_id,omitempty"`
	FailureCode               string    `json:"failure_code,omitempty"`
	FailureMessage            string    `json:"failure_message,omitempty"`
	CreatedAt                 time.Time `json:"created_at"`
	UpdatedAt                 time.Time `json:"updated_at"`
}

type SandboxDTO struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	RecipeVersionID  string    `json:"recipe_version_id"`
	ProfileVersionID string    `json:"profile_version_id"`
	DesiredState     string    `json:"desired_state"`
	ObservedState    string    `json:"observed_state"`
	Generation       int64     `json:"generation"`
	LatestSnapshotID *string   `json:"latest_snapshot_id,omitempty"`
	FailureCode      string    `json:"failure_code,omitempty"`
	FailureMessage   string    `json:"failure_message,omitempty"`
	AvailableActions []string  `json:"available_actions,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func PublicSandbox(s Sandbox, actions []string) SandboxDTO {
	return SandboxDTO{ID: s.ID, Name: s.Name, RecipeVersionID: s.RecipeVersionID, ProfileVersionID: s.ProfileVersionID, DesiredState: s.DesiredState, ObservedState: s.ObservedState, Generation: s.Generation, LatestSnapshotID: s.LatestSnapshotID, FailureCode: s.FailureCode, FailureMessage: s.FailureMessage, AvailableActions: actions, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt}
}

type SandboxOperation struct {
	ID                  string          `json:"id"`
	TenantID            string          `json:"-"`
	SandboxID           *string         `json:"sandbox_id,omitempty"`
	RecipeVersionID     *string         `json:"recipe_version_id,omitempty"`
	SnapshotID          *string         `json:"snapshot_id,omitempty"`
	RetryOfOperationID  *string         `json:"retry_of_operation_id,omitempty"`
	Kind                string          `json:"kind"`
	State               string          `json:"state"`
	RequestedGeneration int64           `json:"requested_generation"`
	Attempt             int             `json:"attempt"`
	MaxAttempts         int             `json:"max_attempts"`
	DeadlineAt          *time.Time      `json:"deadline_at,omitempty"`
	FailureCode         string          `json:"failure_code,omitempty"`
	FailureMessage      string          `json:"failure_message,omitempty"`
	Result              json.RawMessage `json:"result,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

type SandboxSnapshot struct {
	ID                         string    `json:"id"`
	TenantID                   string    `json:"-"`
	SandboxID                  string    `json:"sandbox_id"`
	OperationID                string    `json:"operation_id"`
	Generation                 int64     `json:"generation"`
	Kind                       string    `json:"kind"`
	State                      string    `json:"state"`
	LineageID                  string    `json:"-"`
	SourceHostCompatibilityKey string    `json:"-"`
	VMRestoreDescriptorDigest  string    `json:"-"`
	SnapshotCompatibilityKey   string    `json:"-"`
	ManifestVersion            string    `json:"manifest_version"`
	TotalBytes                 int64     `json:"total_bytes"`
	FailureCode                string    `json:"failure_code,omitempty"`
	FailureMessage             string    `json:"failure_message,omitempty"`
	CreatedAt                  time.Time `json:"created_at"`
}

type SandboxProfileDTO struct {
	ID            string `json:"id"`
	ProfileFamily string `json:"profile_family"`
	Name          string `json:"name"`
	Version       string `json:"version"`
	Status        string `json:"status"`
	VCPU          int    `json:"vcpu"`
	MemoryMB      int    `json:"memory_mb"`
}
type SandboxRecipeDTO struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
type SandboxRecipeVersionDTO struct {
	ID             string          `json:"id"`
	RecipeID       string          `json:"recipe_id"`
	VersionNumber  int             `json:"version_number"`
	SchemaVersion  string          `json:"schema_version"`
	Status         string          `json:"status"`
	FailureCode    string          `json:"failure_code,omitempty"`
	FailureMessage string          `json:"failure_message,omitempty"`
	Document       json.RawMessage `json:"document,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}
type SandboxEventDTO struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Level     string          `json:"level"`
	Message   string          `json:"message"`
	Details   json.RawMessage `json:"details,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}
type SandboxLogDTO struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`
	Stream    string    `json:"stream"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}
