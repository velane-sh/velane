package client

import (
	"context"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
)

// Snippet mirrors the control-plane snippet model.
type Snippet struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenant_id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Language  string `json:"language"`
	CreatedAt string `json:"created_at"`
	CreatedBy string `json:"created_by"`
}

// SnippetVersion mirrors the control-plane version model.
type SnippetVersion struct {
	ID            string `json:"id"`
	SnippetID     string `json:"snippet_id"`
	VersionNumber int    `json:"version_number"`
	Code          string `json:"code"`
	Status        string `json:"status"`
	CreatedAt     string `json:"created_at"`
}

// InvocationResult is the response from an invoke call.
type InvocationResult struct {
	Output       any    `json:"output"`
	InvocationID string `json:"invocation_id"`
	DurationMs   int    `json:"duration_ms"`
	Status       string `json:"status"`
	Error        string `json:"error"`
	Stderr       string `json:"stderr"`
}

// Client is a typed HTTP client for the Velane control plane.
type Client struct {
	http *resty.Client
}

// New constructs a Client.
func New(base, tenant, key string) *Client {
	rc := resty.New().
		SetBaseURL(base).
		SetHeader("Authorization", "Bearer "+key).
		SetHeader("X-Tenant", tenant)
	return &Client{http: rc}
}

// SetRequestTimeout bounds each HTTP request made by this client.
func (c *Client) SetRequestTimeout(timeout time.Duration) {
	c.http.SetTimeout(timeout)
}

// ListSnippets returns all snippets for the tenant.
func (c *Client) ListSnippets(_ context.Context) ([]*Snippet, error) {
	var snippets []*Snippet
	resp, err := c.http.R().SetResult(&snippets).Get("/v1/snippets")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode(), resp.String())
	}
	return snippets, nil
}

// GetSnippet retrieves a snippet by ID or slug.
func (c *Client) GetSnippet(_ context.Context, id string) (*Snippet, error) {
	var sn Snippet
	resp, err := c.http.R().SetResult(&sn).Get("/v1/snippets/" + id)
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode(), resp.String())
	}
	return &sn, nil
}

// CreateSnippet creates a new snippet.
func (c *Client) CreateSnippet(_ context.Context, name, lang string) (*Snippet, error) {
	var sn Snippet
	resp, err := c.http.R().
		SetBody(map[string]string{"name": name, "language": lang}).
		SetResult(&sn).
		Post("/v1/snippets")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode(), resp.String())
	}
	return &sn, nil
}

// DeleteSnippet removes a snippet by ID.
func (c *Client) DeleteSnippet(_ context.Context, id string) error {
	resp, err := c.http.R().Delete("/v1/snippets/" + id)
	if err != nil {
		return err
	}
	if resp.IsError() {
		return fmt.Errorf("API error %d: %s", resp.StatusCode(), resp.String())
	}
	return nil
}

// CreateVersion creates a new version for a snippet.
func (c *Client) CreateVersion(_ context.Context, snippetID, code string) (*SnippetVersion, error) {
	var v SnippetVersion
	resp, err := c.http.R().
		SetBody(map[string]string{"code": code}).
		SetResult(&v).
		Post("/v1/snippets/" + snippetID + "/versions")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode(), resp.String())
	}
	return &v, nil
}

// ListVersions returns all versions for a snippet.
func (c *Client) ListVersions(_ context.Context, snippetID string) ([]*SnippetVersion, error) {
	var versions []*SnippetVersion
	resp, err := c.http.R().SetResult(&versions).Get("/v1/snippets/" + snippetID + "/versions")
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode(), resp.String())
	}
	return versions, nil
}

// PublishVersion publishes a version to the given environment.
func (c *Client) PublishVersion(_ context.Context, snippetID string, versionNum int, env string) (*SnippetVersion, error) {
	var v SnippetVersion
	resp, err := c.http.R().
		SetResult(&v).
		Post(fmt.Sprintf("/v1/snippets/%s/versions/%d/publish?env=%s", snippetID, versionNum, env))
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode(), resp.String())
	}
	return &v, nil
}

// Invoke synchronously invokes a snippet.
func (c *Client) Invoke(_ context.Context, tenantSlug, snippetSlug, env, input string) (*InvocationResult, error) {
	var result InvocationResult
	resp, err := c.http.R().
		SetBody(input).
		SetHeader("Content-Type", "application/json").
		SetResult(&result).
		Post(fmt.Sprintf("/v1/invoke/%s/%s?env=%s", tenantSlug, snippetSlug, env))
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode(), resp.String())
	}
	return &result, nil
}
