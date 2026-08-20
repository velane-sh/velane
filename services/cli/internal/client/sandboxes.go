package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-resty/resty/v2"
)

// Sandbox is the provider-neutral public sandbox representation.
type Sandbox struct {
	ID                  string         `json:"id"`
	Name                string         `json:"name"`
	RecipeVersionID     string         `json:"recipe_version_id"`
	ProfileVersionID    string         `json:"profile_version_id"`
	DesiredState        string         `json:"desired_state"`
	ObservedState       string         `json:"observed_state"`
	Generation          int64          `json:"generation"`
	LatestSnapshotID    *string        `json:"latest_snapshot_id,omitempty"`
	FailureCode         string         `json:"failure_code"`
	FailureMessage      string         `json:"failure_message"`
	AvailableActions    []string       `json:"available_actions"`
	CreatedAt           string         `json:"created_at"`
	UpdatedAt           string         `json:"updated_at"`
	RestoreAvailability map[string]any `json:"restore_availability"`
}

// Operation represents an asynchronous sandbox or image build operation.
type Operation struct {
	ID              string          `json:"id"`
	SandboxID       *string         `json:"sandbox_id,omitempty"`
	RecipeVersionID *string         `json:"recipe_version_id,omitempty"`
	SnapshotID      *string         `json:"snapshot_id,omitempty"`
	Kind            string          `json:"kind"`
	State           string          `json:"state"`
	FailureCode     string          `json:"failure_code"`
	FailureMessage  string          `json:"failure_message"`
	Retryable       bool            `json:"retryable"`
	Result          json.RawMessage `json:"result,omitempty"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
}

// Snapshot is a safe public summary of a full sandbox checkpoint.
type Snapshot struct {
	ID              string `json:"id"`
	SandboxID       string `json:"sandbox_id"`
	OperationID     string `json:"operation_id"`
	Generation      int64  `json:"generation"`
	Kind            string `json:"kind"`
	State           string `json:"state"`
	ManifestVersion string `json:"manifest_version"`
	TotalBytes      int64  `json:"total_bytes"`
	FailureCode     string `json:"failure_code,omitempty"`
	FailureMessage  string `json:"failure_message,omitempty"`
	CreatedAt       string `json:"created_at"`
}

// SandboxProfile is an immutable, provider-neutral sandbox profile version.
type SandboxProfile struct {
	ID            string `json:"id"`
	ProfileFamily string `json:"profile_family"`
	Name          string `json:"name"`
	Version       string `json:"version"`
	Status        string `json:"status"`
	VCPU          int    `json:"vcpu"`
	MemoryMB      int    `json:"memory_mb"`
}

// Event is a sanitized durable sandbox event.
type Event struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Level     string          `json:"level"`
	Message   string          `json:"message"`
	Details   json.RawMessage `json:"details,omitempty"`
	CreatedAt string          `json:"created_at"`
}

// LogEntry is a bounded and redacted sandbox log entry.
type LogEntry struct {
	ID        string `json:"id"`
	Source    string `json:"source"`
	Stream    string `json:"stream"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}

type listResponse[T any] struct {
	Items      []T    `json:"items"`
	Total      int    `json:"total"`
	NextCursor string `json:"next_cursor"`
}

type sandboxMutationResponse struct {
	Sandbox   *Sandbox   `json:"sandbox"`
	Operation *Operation `json:"operation"`
}

// CreateSandboxRequest identifies the immutable recipe and profile to pin.
type CreateSandboxRequest struct {
	Name             string `json:"name"`
	RecipeVersionID  string `json:"recipe_version_id"`
	ProfileVersionID string `json:"profile_version_id"`
}

// InstanceCapabilities mirrors the server's operational admission state.
// Missing values are unavailable to preserve a fail-closed CLI default.
type InstanceCapabilities struct {
	Sandboxes           bool `json:"sandboxes"`
	SandboxProfiles     bool `json:"sandbox_profiles"`
	SandboxImageRecipes bool `json:"sandbox_image_recipes"`
	SandboxOperations   bool `json:"sandbox_operations"`
	SandboxSnapshots    bool `json:"sandbox_snapshots"`
	SandboxEvents       bool `json:"sandbox_events"`
	SandboxLogs         bool `json:"sandbox_logs"`
}

type InstanceInfo struct {
	Capabilities InstanceCapabilities `json:"capabilities"`
}

// SandboxCapabilityUnavailableError explains a failed mutation preflight.
type SandboxCapabilityUnavailableError struct{ Capability string }

func (e *SandboxCapabilityUnavailableError) Error() string {
	return fmt.Sprintf("sandbox %s capability is unavailable on this server; configure its required backend dependencies and try again", e.Capability)
}

func (c *Client) GetInstanceInfo(ctx context.Context) (*InstanceInfo, error) {
	var info InstanceInfo
	if err := c.request(ctx, resty.MethodGet, "/v1/instance/info", nil, &info, nil); err != nil {
		return nil, err
	}
	return &info, nil
}

// RequireSandboxCapability discovers server admission before a mutation. This
// avoids creating client-side intent against a server that cannot execute it.
func (c *Client) RequireSandboxCapability(ctx context.Context, capability string) error {
	info, err := c.GetInstanceInfo(ctx)
	if err != nil {
		return fmt.Errorf("discover sandbox capabilities: %w", err)
	}
	available := false
	switch capability {
	case "sandboxes":
		available = info.Capabilities.Sandboxes
	case "sandbox_operations":
		available = info.Capabilities.SandboxOperations
	case "sandbox_snapshots":
		available = info.Capabilities.SandboxSnapshots
	case "sandbox_image_recipes":
		available = info.Capabilities.SandboxImageRecipes
	default:
		return fmt.Errorf("unknown sandbox capability %q", capability)
	}
	if !available {
		return &SandboxCapabilityUnavailableError{Capability: capability}
	}
	return nil
}

func (c *Client) request(ctx context.Context, method, path string, body any, result any, headers map[string]string) error {
	_, err := c.requestStatus(ctx, method, path, body, result, headers, http.StatusOK)
	return err
}

func (c *Client) requestStatus(ctx context.Context, method, path string, body any, result any, headers map[string]string, expectedStatus int) (*resty.Response, error) {
	resp, err := c.do(ctx, method, path, body, result, headers)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != expectedStatus {
		return nil, fmt.Errorf("API returned unexpected status %d (want %d)", resp.StatusCode(), expectedStatus)
	}
	return resp, nil
}

func (c *Client) do(ctx context.Context, method, path string, body any, result any, headers map[string]string) (*resty.Response, error) {
	req := c.http.R().SetContext(ctx).SetResult(result)
	if body != nil {
		req.SetBody(body)
	}
	for key, value := range headers {
		if value != "" {
			req.SetHeader(key, value)
		}
	}
	resp, err := req.Execute(method, path)
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, parseAPIError(resp.StatusCode(), resp.String(), resp.Header().Get("Retry-After"))
	}
	return resp, nil
}

// ListSandboxes returns one bounded page of sandboxes.
func (c *Client) ListSandboxes(ctx context.Context, offset, limit int) ([]Sandbox, int, error) {
	path := fmt.Sprintf("/v1/sandboxes?offset=%d&limit=%d", offset, limit)
	var response listResponse[Sandbox]
	if err := c.request(ctx, resty.MethodGet, path, nil, &response, nil); err != nil {
		return nil, 0, err
	}
	return response.Items, response.Total, nil
}

func (c *Client) GetSandbox(ctx context.Context, id string) (*Sandbox, error) {
	var response sandboxDetailResponse
	if err := c.request(ctx, resty.MethodGet, "/v1/sandboxes/"+url.PathEscape(id), nil, &response, nil); err != nil {
		return nil, err
	}
	if response.Sandbox == nil {
		return nil, fmt.Errorf("API response did not include a sandbox")
	}
	return response.Sandbox, nil
}

type sandboxDetailResponse struct{ Sandbox *Sandbox }

func (r *sandboxDetailResponse) UnmarshalJSON(data []byte) error {
	var envelope struct {
		Sandbox          *Sandbox `json:"sandbox"`
		AvailableActions []string `json:"available_actions"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	if envelope.Sandbox == nil {
		return fmt.Errorf("API response did not include a sandbox")
	}
	envelope.Sandbox.AvailableActions = envelope.AvailableActions
	r.Sandbox = envelope.Sandbox
	return nil
}

func (c *Client) CreateSandbox(ctx context.Context, request CreateSandboxRequest, idempotencyKey string) (*Sandbox, *Operation, error) {
	return c.mutateSandbox(ctx, resty.MethodPost, "/v1/sandboxes", request, idempotencyKey, "")
}

func (c *Client) StartSandbox(ctx context.Context, id, key, ifMatch string) (*Operation, error) {
	return c.sandboxOperation(ctx, id, "start", nil, key, ifMatch)
}

func (c *Client) StopSandbox(ctx context.Context, id, key, ifMatch string) (*Operation, error) {
	return c.sandboxOperation(ctx, id, "stop", nil, key, ifMatch)
}

func (c *Client) RestartSandbox(ctx context.Context, id, key, ifMatch string) (*Operation, error) {
	return c.sandboxOperation(ctx, id, "restart", nil, key, ifMatch)
}

func (c *Client) CreateSnapshot(ctx context.Context, id, key, ifMatch string) (*Operation, error) {
	return c.sandboxOperation(ctx, id, "snapshots", nil, key, ifMatch)
}

func (c *Client) RestoreSnapshot(ctx context.Context, sandboxID, snapshotID, key, ifMatch string) (*Operation, error) {
	return c.sandboxOperation(ctx, sandboxID, "snapshots/"+url.PathEscape(snapshotID)+"/restore", nil, key, ifMatch)
}

func (c *Client) RetrySandboxOperation(ctx context.Context, sandboxID, operationID, key, ifMatch string) (*Operation, error) {
	return c.sandboxOperation(ctx, sandboxID, "retry", map[string]string{"operation_id": operationID}, key, ifMatch)
}

func (c *Client) DeleteSandbox(ctx context.Context, id string, deleteSnapshots bool, key, ifMatch string) (*Operation, error) {
	_, operation, err := c.mutateSandbox(ctx, resty.MethodDelete, "/v1/sandboxes/"+url.PathEscape(id), map[string]bool{"delete_snapshots": deleteSnapshots}, key, ifMatch)
	return operation, err
}

func (c *Client) DeleteSnapshot(ctx context.Context, sandboxID, snapshotID, key, ifMatch string) (*Operation, error) {
	_, operation, err := c.mutateSandbox(ctx, resty.MethodDelete, "/v1/sandboxes/"+url.PathEscape(sandboxID)+"/snapshots/"+url.PathEscape(snapshotID), nil, key, ifMatch)
	return operation, err
}

func (c *Client) sandboxOperation(ctx context.Context, id, action string, body any, key, ifMatch string) (*Operation, error) {
	_, operation, err := c.mutateSandbox(ctx, resty.MethodPost, "/v1/sandboxes/"+url.PathEscape(id)+"/"+action, body, key, ifMatch)
	return operation, err
}

func (c *Client) mutateSandbox(ctx context.Context, method, path string, body any, key, ifMatch string) (*Sandbox, *Operation, error) {
	if err := ValidateIdempotencyKey(key); err != nil {
		return nil, nil, err
	}
	var response sandboxMutationResponse
	headers := map[string]string{"Idempotency-Key": key}
	if ifMatch != "" {
		headers["If-Match"] = strconv.Quote(ifMatch)
	}
	if _, err := c.requestStatus(ctx, method, path, body, &response, headers, http.StatusAccepted); err != nil {
		return nil, nil, err
	}
	if response.Operation == nil {
		return nil, nil, fmt.Errorf("API response did not include an operation")
	}
	return response.Sandbox, response.Operation, nil
}

func (c *Client) ListSnapshots(ctx context.Context, sandboxID string, offset, limit int) ([]Snapshot, int, error) {
	var response listResponse[Snapshot]
	path := fmt.Sprintf("/v1/sandboxes/%s/snapshots?offset=%d&limit=%d", url.PathEscape(sandboxID), offset, limit)
	if err := c.request(ctx, resty.MethodGet, path, nil, &response, nil); err != nil {
		return nil, 0, err
	}
	return response.Items, response.Total, nil
}

func (c *Client) GetSnapshot(ctx context.Context, sandboxID, snapshotID string) (*Snapshot, error) {
	var snapshot Snapshot
	path := "/v1/sandboxes/" + url.PathEscape(sandboxID) + "/snapshots/" + url.PathEscape(snapshotID)
	if err := c.request(ctx, resty.MethodGet, path, nil, &snapshot, nil); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (c *Client) GetOperation(ctx context.Context, operationID string) (*Operation, int, error) {
	var operation Operation
	resp, err := c.requestStatus(ctx, resty.MethodGet, "/v1/sandbox-operations/"+url.PathEscape(operationID), nil, &operation, nil, http.StatusOK)
	if err != nil {
		return nil, 0, err
	}
	retryAfter, _ := strconv.Atoi(resp.Header().Get("Retry-After"))
	return &operation, retryAfter, nil
}

func (c *Client) ListSandboxProfiles(ctx context.Context) ([]SandboxProfile, error) {
	var response listResponse[SandboxProfile]
	if err := c.request(ctx, resty.MethodGet, "/v1/sandbox-profiles", nil, &response, nil); err != nil {
		return nil, err
	}
	return response.Items, nil
}

func sandboxCursorPath(sandboxID, resource, after string, limit int) string {
	values := url.Values{}
	if after != "" {
		values.Set("after", after)
	}
	values.Set("limit", strconv.Itoa(limit))
	return "/v1/sandboxes/" + url.PathEscape(sandboxID) + "/" + resource + "?" + values.Encode()
}

// ListSandboxEvents returns a durable cursor page, oldest first.
func (c *Client) ListSandboxEvents(ctx context.Context, sandboxID, after string, limit int) ([]Event, string, error) {
	var response listResponse[Event]
	if err := c.request(ctx, resty.MethodGet, sandboxCursorPath(sandboxID, "events", after, limit), nil, &response, nil); err != nil {
		return nil, "", err
	}
	return response.Items, response.NextCursor, nil
}

// ListSandboxLogs returns a durable cursor page of bounded/redacted logs.
func (c *Client) ListSandboxLogs(ctx context.Context, sandboxID, after string, limit int) ([]LogEntry, string, error) {
	var response listResponse[LogEntry]
	if err := c.request(ctx, resty.MethodGet, sandboxCursorPath(sandboxID, "logs", after, limit), nil, &response, nil); err != nil {
		return nil, "", err
	}
	return response.Items, response.NextCursor, nil
}

// IsTerminalOperation reports whether no more server-side progress is expected.
func IsTerminalOperation(state string) bool {
	switch strings.ToLower(state) {
	case "succeeded", "failed", "cancelled":
		return true
	default:
		return false
	}
}
