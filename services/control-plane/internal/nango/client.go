package nango

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/models"
)

// Client is a minimal Nango REST API client.
type Client struct {
	baseURL    string
	secretKey  string
	httpClient *http.Client

	providersMu       sync.RWMutex
	providersCache    []models.NangoProvider
	providersCachedAt time.Time
}
type SyncRecord struct {
	ID     string         `json:"id"`
	Data   map[string]any `json:"-"`
	Action string         `json:"-"`
}
type RecordsPage struct {
	Records    []SyncRecord
	NextCursor string
}

// ListRecords fetches one page of an incremental Nango sync model result.
func (c *Client) ListRecords(ctx context.Context, connectionID, providerConfigKey, model, modifiedAfter, cursor string) (*RecordsPage, error) {
	q := url.Values{"connection_id": {connectionID}, "provider_config_key": {providerConfigKey}, "model": {model}, "limit": {"100"}}
	if modifiedAfter != "" {
		q.Set("modified_after", modifiedAfter)
	}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/records?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nango ListRecords: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("nango ListRecords %d: %s", resp.StatusCode, raw)
	}
	var out struct {
		Records    []map[string]any `json:"records"`
		NextCursor string           `json:"next_cursor"`
		Cursor     string           `json:"cursor"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	p := &RecordsPage{NextCursor: out.NextCursor}
	if p.NextCursor == "" {
		p.NextCursor = out.Cursor
	}
	for _, raw := range out.Records {
		action := "updated"
		if v, ok := raw["_nango_metadata"].(map[string]any); ok {
			if lastAction, _ := v["last_action"].(string); lastAction == "ADDED" || lastAction == "added" {
				action = "added"
			} else if lastAction == "DELETED" || lastAction == "deleted" {
				action = "deleted"
			} else if deleted, _ := v["deleted_at"].(string); deleted != "" {
				action = "deleted"
			} else if first, ok := v["first_seen_at"].(string); ok && first > modifiedAfter {
				action = "added"
			}
		}
		id, _ := raw["id"].(string)
		p.Records = append(p.Records, SyncRecord{ID: id, Data: raw, Action: action})
	}
	return p, nil
}

// ListSyncModels uses Nango's deployed flow metadata. Older installations may
// return 404; callers can then offer a validated manual model field.
func (c *Client) ListSyncModels(ctx context.Context, providerConfigKey string) ([]string, error) {
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/flows?provider_config_key="+url.QueryEscape(providerConfigKey), nil)
	if e != nil {
		return nil, e
	}
	c.setAuth(req)
	resp, e := c.httpClient.Do(req)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("nango sync model discovery unavailable (%d)", resp.StatusCode)
	}
	var out struct {
		Data []struct {
			Models   []string `json:"models"`
			SyncName string   `json:"sync_name"`
		} `json:"data"`
	}
	if e = json.NewDecoder(resp.Body).Decode(&out); e != nil {
		return nil, e
	}
	seen := map[string]bool{}
	var result []string
	for _, f := range out.Data {
		for _, m := range f.Models {
			if !seen[m] {
				seen[m] = true
				result = append(result, m)
			}
		}
	}
	return result, nil
}

// New returns a Nango client pointing at baseURL (e.g. "http://nango:3003").
func New(baseURL, secretKey string) *Client {
	return &Client{
		baseURL:    baseURL,
		secretKey:  secretKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// CreateConnectSession asks Nango for a short-lived Connect session token.
// providerConfigKey identifies a tenant-specific integration config in Nango.
func (c *Client) CreateConnectSession(ctx context.Context, tenantID, tenantName, providerConfigKey, alias string) (string, error) {
	if alias == "" {
		alias = "default"
	}
	body := map[string]any{
		// tags replaces the deprecated end_user field.
		"tags": map[string]string{
			"end_user_id":     tenantID,
			"organization_id": tenantID,
			"display_name":    tenantName,
			"velane_alias":    alias,
		},
		"allowed_integrations": []string{providerConfigKey},
	}

	b, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/connect/sessions", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("nango CreateConnectSession: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("nango CreateConnectSession %d: %s", resp.StatusCode, raw)
	}

	var out struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("nango CreateConnectSession decode: %w", err)
	}
	return out.Data.Token, nil
}

// FindConnectionID returns the newest Nango connection matching Velane's tenant,
// provider config, and alias tags.
func (c *Client) FindConnectionID(ctx context.Context, tenantID, providerConfigKey, alias string) (string, error) {
	if alias == "" {
		alias = "default"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/connections", nil)
	if err != nil {
		return "", err
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("nango FindConnectionID: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("nango FindConnectionID %d: %s", resp.StatusCode, raw)
	}

	type nangoConnection struct {
		ConnectionID      string            `json:"connection_id"`
		ProviderConfigKey string            `json:"provider_config_key"`
		Tags              map[string]string `json:"tags"`
		Metadata          map[string]any    `json:"metadata"`
		Created           string            `json:"created"`
	}
	var envelope struct {
		Connections []nangoConnection `json:"connections"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return "", fmt.Errorf("nango FindConnectionID decode: %w", err)
	}

	var newest nangoConnection
	var newestAt time.Time
	for _, conn := range envelope.Connections {
		if conn.ConnectionID == "" || conn.ProviderConfigKey != providerConfigKey {
			continue
		}
		if !nangoConnectionMatchesTenant(conn.Tags, conn.Metadata, tenantID) {
			continue
		}
		if !nangoConnectionMatchesAlias(conn.Tags, conn.Metadata, alias) {
			continue
		}

		createdAt, err := time.Parse(time.RFC3339Nano, conn.Created)
		if err != nil {
			if newest.ConnectionID == "" {
				newest = conn
			}
			continue
		}
		if newest.ConnectionID == "" || createdAt.After(newestAt) {
			newest = conn
			newestAt = createdAt
		}
	}
	return newest.ConnectionID, nil
}

func nangoConnectionMatchesTenant(tags map[string]string, metadata map[string]any, tenantID string) bool {
	if tenantID == "" {
		return true
	}
	if tags["end_user_id"] == tenantID || tags["organization_id"] == tenantID {
		return true
	}
	if metadata != nil {
		if value, ok := metadata["velane_tenant_id"].(string); ok && value != "" {
			return value == tenantID
		}
	}
	return tags["end_user_id"] == "" && tags["organization_id"] == ""
}

func nangoConnectionMatchesAlias(tags map[string]string, metadata map[string]any, alias string) bool {
	if alias == "" {
		alias = "default"
	}
	if value := tags["velane_alias"]; value != "" {
		return value == alias
	}
	if metadata != nil {
		if value, ok := metadata["velane_alias"].(string); ok && value != "" {
			return value == alias
		}
	}
	return true
}

// ImportConnection creates or updates a Nango connection for providers where
// Velane already has the end-user credentials, such as API key integrations.
func (c *Client) ImportConnection(ctx context.Context, providerConfigKey, connectionID string, credentials, connectionConfig, metadata map[string]any, tags map[string]string) (string, error) {
	body := map[string]any{
		"provider_config_key": providerConfigKey,
		"connection_id":       connectionID,
		"credentials":         credentials,
	}
	if len(connectionConfig) > 0 {
		body["connection_config"] = connectionConfig
	}
	if len(metadata) > 0 {
		body["metadata"] = metadata
	}
	if len(tags) > 0 {
		body["tags"] = tags
	}
	b, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/connections", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("nango ImportConnection: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("nango ImportConnection %d: %s", resp.StatusCode, raw)
	}

	var out struct {
		ConnectionID string `json:"connection_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("nango ImportConnection decode: %w", err)
	}
	if out.ConnectionID == "" {
		out.ConnectionID = connectionID
	}
	return out.ConnectionID, nil
}

// PatchConnectionMetadata updates metadata on an existing Nango connection without
// overwriting fields not included in the patch (PATCH semantics).
func (c *Client) PatchConnectionMetadata(ctx context.Context, connectionID, providerConfigKey string, metadata map[string]any) error {
	body := map[string]any{
		"connection_id":       connectionID,
		"provider_config_key": providerConfigKey,
		"metadata":            metadata,
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.baseURL+"/connections/metadata", bytes.NewReader(b))
	if err != nil {
		return err
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nango PatchConnectionMetadata: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("nango PatchConnectionMetadata %d: %s", resp.StatusCode, raw)
	}
	return nil
}

// DeleteConnection removes a connection from Nango.
func (c *Client) DeleteConnection(ctx context.Context, connectionID, provider string) error {
	url := fmt.Sprintf("%s/connection/%s?provider_config_key=%s", c.baseURL, connectionID, provider)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nango DeleteConnection: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode != 404 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("nango DeleteConnection %d: %s", resp.StatusCode, raw)
	}
	return nil
}

// ListProviders returns all provider templates from Nango, cached for 1 hour.
func (c *Client) ListProviders(ctx context.Context) ([]models.NangoProvider, error) {
	c.providersMu.RLock()
	if time.Since(c.providersCachedAt) < time.Hour && c.providersCache != nil {
		cached := c.providersCache
		c.providersMu.RUnlock()
		return cached, nil
	}
	c.providersMu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/providers", nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nango ListProviders: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("nango ListProviders %d: %s", resp.StatusCode, raw)
	}

	// Nango wraps the list in {"data": [...]}
	var envelope struct {
		Data []models.NangoProvider `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("nango ListProviders decode: %w", err)
	}
	providers := envelope.Data

	c.providersMu.Lock()
	c.providersCache = providers
	c.providersCachedAt = time.Now()
	c.providersMu.Unlock()

	return providers, nil
}

// GetProviderDetail fetches a single provider from Nango's GET /providers/{provider}.
// This returns richer metadata than the list endpoint, including docs URL and proxy base_url.
func (c *Client) GetProviderDetail(ctx context.Context, providerKey string) (*models.NangoProvider, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/providers/"+providerKey, nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nango GetProviderDetail: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("provider %q not found", providerKey)
	}
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("nango GetProviderDetail %d: %s", resp.StatusCode, raw)
	}

	var envelope struct {
		Data models.NangoProvider `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("nango GetProviderDetail decode: %w", err)
	}
	return &envelope.Data, nil
}

// GetProvider returns metadata for a single provider from the cached list.
func (c *Client) GetProvider(ctx context.Context, providerKey string) (*models.NangoProvider, error) {
	providers, err := c.ListProviders(ctx)
	if err != nil {
		return nil, err
	}
	for i := range providers {
		if providers[i].UniqueKey == providerKey { // UniqueKey maps to Nango's "name" field (the slug)
			return &providers[i], nil
		}
	}
	return nil, fmt.Errorf("provider %q not found", providerKey)
}

// Proxy forwards an HTTP request to Nango's proxy endpoint, which injects
// the OAuth token and proxies to the external provider API.
// path is the provider-relative path, e.g. "/user/repos".
func (c *Client) Proxy(w http.ResponseWriter, r *http.Request, connectionID, providerConfigKey, path string) {
	// Build the Nango proxy URL: strip leading slash from path then concatenate.
	nangoURL := c.baseURL + "/proxy" + path
	if r.URL.RawQuery != "" {
		nangoURL += "?" + r.URL.RawQuery
	}

	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, nangoURL, r.Body)
	if err != nil {
		http.Error(w, "proxy request build failed", http.StatusInternalServerError)
		return
	}

	// Forward content-type if set.
	if ct := r.Header.Get("Content-Type"); ct != "" {
		proxyReq.Header.Set("Content-Type", ct)
	}

	c.setAuth(proxyReq)
	proxyReq.Header.Set("Connection-Id", connectionID)
	proxyReq.Header.Set("Provider-Config-Key", providerConfigKey)

	resp, err := c.httpClient.Do(proxyReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("proxy failed: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Forward response headers and status.
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body) //nolint:errcheck
}

// CreateIntegrationConfig creates a provider config in Nango.
// Nango API: POST /integrations with unique_key, provider, credentials.
func (c *Client) CreateIntegrationConfig(ctx context.Context, providerConfigKey, provider string, credentials map[string]any) error {
	body := map[string]any{
		"unique_key": providerConfigKey,
		"provider":   provider,
	}
	if len(credentials) > 0 {
		body["credentials"] = credentials
	}

	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/integrations", bytes.NewReader(b))
	if err != nil {
		return err
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nango CreateIntegrationConfig: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("nango CreateIntegrationConfig %d: %s", resp.StatusCode, raw)
	}
	return nil
}

// UpdateIntegrationConfig updates an existing provider config in Nango.
// Nango API: PATCH /integrations/{uniqueKey}
func (c *Client) UpdateIntegrationConfig(ctx context.Context, providerConfigKey string, credentials map[string]any) error {
	body := map[string]any{}
	if len(credentials) > 0 {
		body["credentials"] = credentials
	}

	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.baseURL+"/integrations/"+providerConfigKey, bytes.NewReader(b))
	if err != nil {
		return err
	}
	c.setAuth(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nango UpdateIntegrationConfig: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("nango UpdateIntegrationConfig %d: %s", resp.StatusCode, raw)
	}
	return nil
}

// ListIntegrationConfigs returns all configured integrations from Nango.
// Nango API: GET /integrations → {"data":[...]}
func (c *Client) ListIntegrationConfigs(ctx context.Context) ([]models.NangoIntegrationConfig, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/integrations", nil)
	if err != nil {
		return nil, err
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nango ListIntegrationConfigs: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("nango ListIntegrationConfigs %d: %s", resp.StatusCode, raw)
	}

	var envelope struct {
		Data []models.NangoIntegrationConfig `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("nango ListIntegrationConfigs decode: %w", err)
	}
	return envelope.Data, nil
}

// DeleteIntegrationConfig removes a provider config from Nango.
// Nango API: DELETE /integrations/{uniqueKey}
func (c *Client) DeleteIntegrationConfig(ctx context.Context, providerConfigKey string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/integrations/"+providerConfigKey, nil)
	if err != nil {
		return err
	}
	c.setAuth(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nango DeleteIntegrationConfig: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode != 404 {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("nango DeleteIntegrationConfig %d: %s", resp.StatusCode, raw)
	}
	return nil
}

func (c *Client) setAuth(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.secretKey)
}
