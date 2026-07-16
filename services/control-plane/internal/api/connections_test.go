// Package api_test — integration tests for connections, proxy and integrations APIs.
// These tests require TEST_DATABASE_URL to be set; they are skipped otherwise.
package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	api "github.com/abskrj/velane/services/control-plane/internal/api"
	"github.com/abskrj/velane/services/control-plane/internal/auth"
	"github.com/abskrj/velane/services/control-plane/internal/executor/remote"
	"github.com/abskrj/velane/services/control-plane/internal/nango"
	"github.com/abskrj/velane/services/control-plane/internal/scheduler"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
	"go.uber.org/zap"
)

// setupWithNango wires the full stack with a mock Nango server.
func setupWithNango(t *testing.T) *testEnv {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store, err := postgres.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect test postgres: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// Mock Nango server handling the subset of endpoints we need.
	configuredIntegrations := map[string]string{} // key: unique_key, val: provider
	importedConnections := map[string]struct {
		ConnectionID      string
		ProviderConfigKey string
		Provider          string
	}{}
	providerAuthModes := map[string]string{
		"github":        "OAUTH2",
		"slack":         "OAUTH2",
		"figma":         "OAUTH2",
		"8x8":           "OAUTH2_CC",
		"google-gemini": "API_KEY",
	}
	mockNango := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		// POST /integrations — create provider config.
		case r.Method == http.MethodPost && r.URL.Path == "/integrations":
			var req struct {
				UniqueKey   string         `json:"unique_key"`
				Provider    string         `json:"provider"`
				Credentials map[string]any `json:"credentials"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.UniqueKey != "" {
				if _, exists := configuredIntegrations[req.UniqueKey]; exists {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":{"code":"invalid_body","errors":[{"code":"invalid_string","message":"Unique key already exists","path":["uniqueKey"]}]}}`))
					return
				}
			}
			if scopes, ok := req.Credentials["scopes"]; ok {
				if _, isSlice := scopes.([]any); isSlice {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":{"code":"invalid_body","errors":[{"code":"invalid_union","message":"Invalid input","path":["credentials","scopes"]}]}}`))
					return
				}
			}
			if expectedType, ok := providerAuthModes[req.Provider]; ok {
				if expectedType == "OAUTH2_CC" || expectedType == "API_KEY" {
					if len(req.Credentials) > 0 {
						w.WriteHeader(http.StatusBadRequest)
						_, _ = w.Write([]byte(`{"error":{"code":"invalid_body","errors":[{"code":"invalid_union","message":"invalid credentials object","path":["credentials","type"]}]}}`))
						return
					}
				} else {
					gotType, _ := req.Credentials["type"].(string)
					if gotType != expectedType {
						w.WriteHeader(http.StatusBadRequest)
						_, _ = w.Write([]byte(`{"error":{"code":"invalid_body","message":"incompatible credentials auth type and provider auth"}}`))
						return
					}
				}
			}
			if req.UniqueKey != "" && req.Provider != "" {
				configuredIntegrations[req.UniqueKey] = req.Provider
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))

		// PATCH /integrations/{unique_key} — update provider config.
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/integrations/"):
			key := strings.TrimPrefix(r.URL.Path, "/integrations/")
			if key == "" || configuredIntegrations[key] == "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			var req struct {
				Credentials map[string]any `json:"credentials"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if scopes, ok := req.Credentials["scopes"]; ok {
				if _, isSlice := scopes.([]any); isSlice {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":{"code":"invalid_body","errors":[{"code":"invalid_union","message":"Invalid input","path":["credentials","scopes"]}]}}`))
					return
				}
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))

		// GET /integrations — list configured provider configs.
		case r.Method == http.MethodGet && r.URL.Path == "/integrations":
			type cfg struct {
				UniqueKey string `json:"unique_key"`
				Provider  string `json:"provider"`
			}
			out := make([]cfg, 0, len(configuredIntegrations))
			for k, p := range configuredIntegrations {
				out = append(out, cfg{UniqueKey: k, Provider: p})
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": out})

		// DELETE /integrations/{unique_key}
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/integrations/"):
			key := strings.TrimPrefix(r.URL.Path, "/integrations/")
			delete(configuredIntegrations, key)
			w.WriteHeader(http.StatusNoContent)

		// POST /connect/sessions — returns a session token.
		case r.Method == http.MethodPost && r.URL.Path == "/connect/sessions":
			var req struct {
				AllowedIntegrations []string `json:"allowed_integrations"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if len(req.AllowedIntegrations) > 0 {
				if _, ok := configuredIntegrations[req.AllowedIntegrations[0]]; !ok {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"provider config not found"}`))
					return
				}
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":{"token":"mock-session-token"}}`))

		// DELETE /connection/{anything} — 204 no body.
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/connection/"):
			w.WriteHeader(http.StatusNoContent)

		// POST /connections — import existing credentials for API-key providers.
		case r.Method == http.MethodPost && r.URL.Path == "/connections":
			var req struct {
				ProviderConfigKey string         `json:"provider_config_key"`
				ConnectionID      string         `json:"connection_id"`
				Credentials       map[string]any `json:"credentials"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			provider := configuredIntegrations[req.ProviderConfigKey]
			if provider == "" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"provider config not found"}`))
				return
			}
			if providerAuthModes[provider] == "API_KEY" {
				if req.Credentials["type"] != "API_KEY" || req.Credentials["apiKey"] == "" {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":"apiKey required"}`))
					return
				}
			}
			if req.ConnectionID == "" {
				req.ConnectionID = "mock-connection-" + req.ProviderConfigKey
			}
			importedConnections[req.ProviderConfigKey] = struct {
				ConnectionID      string
				ProviderConfigKey string
				Provider          string
			}{ConnectionID: req.ConnectionID, ProviderConfigKey: req.ProviderConfigKey, Provider: provider}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"connection_id":       req.ConnectionID,
				"provider_config_key": req.ProviderConfigKey,
				"provider":            provider,
			})

		// GET /connections — returns connection records for reconciliation after OAuth.
		case r.Method == http.MethodGet && r.URL.Path == "/connections":
			type conn struct {
				ConnectionID      string            `json:"connection_id"`
				ProviderConfigKey string            `json:"provider_config_key"`
				Provider          string            `json:"provider"`
				Tags              map[string]string `json:"tags"`
				Created           string            `json:"created"`
			}
			connections := []conn{
				{
					ConnectionID:      "mock-connection-github",
					ProviderConfigKey: "github",
					Provider:          "github",
					Tags:              map[string]string{"velane_alias": "default"},
					Created:           "2026-01-01T00:00:00Z",
				},
				{
					ConnectionID:      "mock-connection-figma",
					ProviderConfigKey: "figma",
					Provider:          "figma",
					Tags:              map[string]string{"velane_alias": "default"},
					Created:           "2026-01-01T00:00:00Z",
				},
			}
			for key, provider := range configuredIntegrations {
				connections = append(connections, conn{
					ConnectionID:      "mock-connection-" + key,
					ProviderConfigKey: key,
					Provider:          provider,
					Created:           "2026-01-02T00:00:00Z",
				})
			}
			for _, imported := range importedConnections {
				connections = append(connections, conn{
					ConnectionID:      imported.ConnectionID,
					ProviderConfigKey: imported.ProviderConfigKey,
					Provider:          imported.Provider,
					Tags:              map[string]string{"velane_alias": "default"},
					Created:           "2026-01-03T00:00:00Z",
				})
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"connections": connections})

		// GET /providers — returns a minimal provider list.
		case r.Method == http.MethodGet && r.URL.Path == "/providers":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"data":[` +
				`{"name":"github","display_name":"GitHub","auth_mode":"OAUTH2","categories":["developer-tools"],"docs":"https://nango.dev/docs/api-integrations/github","proxy":{"base_url":"https://api.github.com"}},` +
				`{"name":"slack","display_name":"Slack","auth_mode":"OAUTH2","categories":["communication"]},` +
				`{"name":"figma","display_name":"Figma","auth_mode":"OAUTH2","categories":["design"],"docs":"https://nango.dev/docs/api-integrations/figma"},` +
				`{"name":"8x8","display_name":"8x8","auth_mode":"OAUTH2_CC","categories":["communication"]},` +
				`{"name":"google-gemini","display_name":"Google Gemini","auth_mode":"API_KEY","categories":["dev-tools"],"credentials":{"apiKey":{"type":"string","title":"API Key"}}}` +
				`]}`))

		// GET /providers/{provider} — single provider detail.
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/providers/"):
			key := strings.TrimPrefix(r.URL.Path, "/providers/")
			detail := map[string]any{
				"name":         key,
				"display_name": key,
				"auth_mode":    "OAUTH2",
				"docs":         "https://nango.dev/docs/api-integrations/" + key,
			}
			if key == "github" {
				detail["display_name"] = "GitHub"
				detail["proxy"] = map[string]string{"base_url": "https://api.github.com"}
			}
			if key == "figma" {
				detail["display_name"] = "Figma"
			}
			if key == "nonexistent-xyz" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			if _, ok := providerAuthModes[key]; !ok && key != "figma" && key != "github" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": detail})

		// GET /proxy/* — forward as a JSON response from the mock provider.
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/proxy/"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"login":               "mockuser",
				"provider_config_key": r.Header.Get("Provider-Config-Key"),
				"connection_id":       r.Header.Get("Connection-Id"),
			})

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(mockNango.Close)

	nangoClient := nango.New(mockNango.URL, "test-key")

	// Mock executor: always succeeds.
	mockExec := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"output":"{\"ok\":true}","stderr":"","duration_ms":10,"peak_memory_mb":8,"exit_code":0,"error":""}`))
	}))
	t.Cleanup(mockExec.Close)

	exec := remote.New(mockExec.URL, mockExec.URL)
	sched := scheduler.New(store, exec, testEncKey, nil)
	log := zap.NewNop()
	router := api.NewRouter(store, sched, log, testEncKey, auth.NewPasswordProvider(store), nangoClient, "", "", "", "", "", "", nil, nil)

	slug := fmt.Sprintf("test-nango-%d", time.Now().UnixNano())
	tenant, err := store.CreateTenant(context.Background(), "Nango Tenant", slug)
	if err != nil {
		t.Fatalf("create test tenant: %v", err)
	}

	_, manageKey, err := store.CreateAPIKeyWithPlain(context.Background(), tenant.ID, "manage-key",
		[]string{"admin", "manage", "invoke"})
	if err != nil {
		t.Fatalf("create manage key: %v", err)
	}

	_, invokeKey, err := store.CreateAPIKeyWithPlain(context.Background(), tenant.ID, "invoke-key",
		[]string{"invoke"})
	if err != nil {
		t.Fatalf("create invoke key: %v", err)
	}

	return &testEnv{
		router:    router,
		store:     store,
		tenant:    tenant,
		manageKey: manageKey,
		invokeKey: invokeKey,
	}
}

func configureProfile(t *testing.T, env *testEnv, provider, alias string, isDefault bool) map[string]any {
	t.Helper()
	rec := env.do(t, http.MethodPost, "/v1/integrations/configured", env.manageKey, map[string]any{
		"provider":            provider,
		"alias":               alias,
		"name":                alias + "-profile",
		"credentials_type":    "OAUTH2",
		"oauth_client_id":     "client-id-" + alias,
		"oauth_client_secret": "client-secret-" + alias,
		"is_default":          isDefault,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("configure profile %s: status=%d body=%s", alias, rec.Code, rec.Body.String())
	}
	return decodeJSON(t, rec)
}

// --- Connections CRUD ---

func TestConnections_ListEmpty(t *testing.T) {
	env := setupWithNango(t)
	path := "/v1/tenant/connections"
	rec := env.do(t, http.MethodGet, path, env.manageKey, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200\nbody: %s", rec.Code, rec.Body.String())
	}
	var conns []any
	if err := json.NewDecoder(rec.Body).Decode(&conns); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(conns) != 0 {
		t.Errorf("expected empty list, got %d connections", len(conns))
	}
}

func TestConnections_Record(t *testing.T) {
	env := setupWithNango(t)
	path := "/v1/tenant/connections"
	rec := env.do(t, http.MethodPost, path, env.manageKey, map[string]any{
		"provider": "github",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d; want 201\nbody: %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	if body["provider"] != "github" {
		t.Errorf("provider = %q; want %q", body["provider"], "github")
	}
	if body["id"] == "" {
		t.Error("id must be set")
	}
}

func TestConnections_RecordBackfillsNangoConnectionID(t *testing.T) {
	env := setupWithNango(t)
	path := "/v1/tenant/connections"
	profile := configureProfile(t, env, "github", "default", true)

	rec := env.do(t, http.MethodPost, path, env.manageKey, map[string]any{
		"provider":              "github",
		"credential_profile_id": profile["id"],
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d; want 201\nbody: %s", rec.Code, rec.Body.String())
	}

	body := decodeJSON(t, rec)
	want := "mock-connection-" + profile["nango_provider_config_key"].(string)
	if body["nango_connection_id"] != want {
		t.Errorf("nango_connection_id = %q; want %q", body["nango_connection_id"], want)
	}
}

func TestConnections_ListAfterRecord(t *testing.T) {
	env := setupWithNango(t)
	path := "/v1/tenant/connections"

	// Record a connection first.
	rec := env.do(t, http.MethodPost, path, env.manageKey, map[string]any{
		"provider": "github",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("record connection: status = %d; want 201\nbody: %s", rec.Code, rec.Body.String())
	}

	// List should show it.
	recList := env.do(t, http.MethodGet, path, env.manageKey, nil)
	if recList.Code != http.StatusOK {
		t.Fatalf("list status = %d; want 200", recList.Code)
	}
	var conns []map[string]any
	if err := json.NewDecoder(recList.Body).Decode(&conns); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(conns) == 0 {
		t.Fatal("expected at least one connection after recording")
	}
	found := false
	for _, c := range conns {
		if c["provider"] == "github" {
			found = true
			break
		}
	}
	if !found {
		t.Error("github connection not found in list after recording")
	}
}

func TestConnections_List_SearchAndPagination(t *testing.T) {
	env := setupWithNango(t)
	path := "/v1/tenant/connections"

	recGitHub := env.do(t, http.MethodPost, path, env.manageKey, map[string]any{
		"provider": "github",
	})
	if recGitHub.Code != http.StatusCreated {
		t.Fatalf("record github: status = %d; want 201\nbody: %s", recGitHub.Code, recGitHub.Body.String())
	}

	recFigma := env.do(t, http.MethodPost, path, env.manageKey, map[string]any{
		"provider": "figma",
	})
	if recFigma.Code != http.StatusCreated {
		t.Fatalf("record figma: status = %d; want 201\nbody: %s", recFigma.Code, recFigma.Body.String())
	}

	recList := env.do(t, http.MethodGet, path+"?q=git&limit=1&offset=0", env.manageKey, nil)
	if recList.Code != http.StatusOK {
		t.Fatalf("list filtered: status = %d; want 200\nbody: %s", recList.Code, recList.Body.String())
	}
	var conns []map[string]any
	if err := json.NewDecoder(recList.Body).Decode(&conns); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(conns) != 1 {
		t.Fatalf("len = %d; want 1", len(conns))
	}
	if provider, _ := conns[0]["provider"].(string); !strings.Contains(strings.ToLower(provider), "git") {
		t.Fatalf("provider %q did not match q=git", provider)
	}

	recOffset := env.do(t, http.MethodGet, path+"?q=git&offset=100", env.manageKey, nil)
	if recOffset.Code != http.StatusOK {
		t.Fatalf("list with large offset: status = %d; want 200", recOffset.Code)
	}
	var connsOffset []map[string]any
	if err := json.NewDecoder(recOffset.Body).Decode(&connsOffset); err != nil {
		t.Fatalf("decode offset response: %v", err)
	}
	if len(connsOffset) != 0 {
		t.Fatalf("expected empty result for large offset, got %d", len(connsOffset))
	}
}

func TestConnections_RecordRequiresManageScope(t *testing.T) {
	env := setupWithNango(t)
	path := "/v1/tenant/connections"
	rec := env.do(t, http.MethodPost, path, env.invokeKey, map[string]any{
		"provider": "github",
	})
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d; want 403", rec.Code)
	}
}

func TestConnections_TenantIsolation(t *testing.T) {
	env := setupWithNango(t)
	path := "/v1/tenant/connections"

	// Record a connection under env's tenant.
	rec := env.do(t, http.MethodPost, path, env.manageKey, map[string]any{
		"provider": "github",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("record connection: status = %d; want 201\nbody: %s", rec.Code, rec.Body.String())
	}

	// Create a second tenant and key.
	slug2 := fmt.Sprintf("nango-t2-%d", time.Now().UnixNano())
	tenant2, err := env.store.CreateTenant(context.Background(), "T2", slug2)
	if err != nil {
		t.Fatalf("create tenant2: %v", err)
	}
	_, key2, err := env.store.CreateAPIKeyWithPlain(context.Background(), tenant2.ID, "k2",
		[]string{"invoke", "manage"})
	if err != nil {
		t.Fatalf("create key2: %v", err)
	}

	// Tenant 2's key lists only its own connections (route is not tenant-slug scoped).
	rec2 := env.do(t, http.MethodGet, path, key2, nil)
	if rec2.Code != http.StatusOK {
		t.Errorf("tenant2 list: status = %d; want 200", rec2.Code)
	}
	var connections []any
	if err := json.NewDecoder(rec2.Body).Decode(&connections); err != nil {
		t.Fatalf("decode connections: %v", err)
	}
	if len(connections) != 0 {
		t.Errorf("tenant2 should have 0 connections, got %d", len(connections))
	}
}

// --- Connection session ---

func TestConnections_CreateSession(t *testing.T) {
	env := setupWithNango(t)
	profile := configureProfile(t, env, "github", "default", true)

	path := "/v1/tenant/connections/session"
	rec := env.do(t, http.MethodPost, path, env.manageKey, map[string]any{
		"provider": "github",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200\nbody: %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	if body["session_token"] != "mock-session-token" {
		t.Errorf("session_token = %q; want %q", body["session_token"], "mock-session-token")
	}
	if body["credential_profile_id"] != profile["id"] {
		t.Errorf("credential_profile_id = %q; want %q", body["credential_profile_id"], profile["id"])
	}
}

func TestConnections_CreateSessionRequiresConfiguredProfile(t *testing.T) {
	env := setupWithNango(t)
	path := "/v1/tenant/connections/session"
	rec := env.do(t, http.MethodPost, path, env.manageKey, map[string]any{
		"provider": "github",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400\nbody: %s", rec.Code, rec.Body.String())
	}
}

func TestConnections_CreateSessionRejectsAPIKeyProfile(t *testing.T) {
	env := setupWithNango(t)
	recConfigure := env.do(t, http.MethodPost, "/v1/integrations/configured", env.manageKey, map[string]any{
		"provider":         "google-gemini",
		"alias":            "default",
		"name":             "Gemini",
		"credentials_type": "API_KEY",
		"credentials": map[string]string{
			"apiKey": "gemini-api-key",
		},
		"is_default": true,
	})
	if recConfigure.Code != http.StatusOK {
		t.Fatalf("configure API key profile: status=%d body=%s", recConfigure.Code, recConfigure.Body.String())
	}

	rec := env.do(t, http.MethodPost, "/v1/tenant/connections/session", env.manageKey, map[string]any{
		"provider": "google-gemini",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400\nbody: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "provider does not use OAuth connect") {
		t.Fatalf("body = %s; want non-OAuth connect error", rec.Body.String())
	}
}

func TestConnections_CreateSessionRequiresManageScope(t *testing.T) {
	env := setupWithNango(t)
	path := "/v1/tenant/connections/session"
	rec := env.do(t, http.MethodPost, path, env.invokeKey, map[string]any{
		"provider": "github",
	})
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d; want 403", rec.Code)
	}
}

func TestConnections_CreateSessionMissingProvider(t *testing.T) {
	env := setupWithNango(t)
	path := "/v1/tenant/connections/session"
	rec := env.do(t, http.MethodPost, path, env.manageKey, map[string]any{})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400", rec.Code)
	}
}

// --- Disconnect ---

func TestConnections_Disconnect(t *testing.T) {
	env := setupWithNango(t)
	basePath := "/v1/tenant/connections"

	// Record a connection first.
	rec := env.do(t, http.MethodPost, basePath, env.manageKey, map[string]any{
		"provider": "github",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("record connection: status = %d; want 201\nbody: %s", rec.Code, rec.Body.String())
	}

	// Disconnect it.
	delPath := basePath + "/github"
	recDel := env.do(t, http.MethodDelete, delPath, env.manageKey, nil)
	if recDel.Code != http.StatusNoContent {
		t.Fatalf("disconnect: status = %d; want 204\nbody: %s", recDel.Code, recDel.Body.String())
	}

	// List should now be empty.
	recList := env.do(t, http.MethodGet, basePath, env.manageKey, nil)
	if recList.Code != http.StatusOK {
		t.Fatalf("list after disconnect: status = %d; want 200", recList.Code)
	}
	var conns []any
	if err := json.NewDecoder(recList.Body).Decode(&conns); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(conns) != 0 {
		t.Errorf("expected empty list after disconnect, got %d connections", len(conns))
	}
}

func TestConnections_DisconnectNotFound(t *testing.T) {
	env := setupWithNango(t)
	path := "/v1/tenant/connections/nonexistent-provider-xyz"
	rec := env.do(t, http.MethodDelete, path, env.manageKey, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d; want 404", rec.Code)
	}
}

func TestConnections_DisconnectProviderWithNonDefaultAlias(t *testing.T) {
	env := setupWithNango(t)
	basePath := "/v1/tenant/connections"
	defaultProfile := configureProfile(t, env, "github", "sandbox", false)

	rec := env.do(t, http.MethodPost, basePath, env.manageKey, map[string]any{
		"provider":              "github",
		"alias":                 "sandbox",
		"credential_profile_id": defaultProfile["id"],
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("record connection: status = %d; want 201\nbody: %s", rec.Code, rec.Body.String())
	}

	delPath := basePath + "/github"
	recDel := env.do(t, http.MethodDelete, delPath, env.manageKey, nil)
	if recDel.Code != http.StatusNoContent {
		t.Fatalf("disconnect: status = %d; want 204\nbody: %s", recDel.Code, recDel.Body.String())
	}
}

func TestConnections_DisconnectRequiresManageScope(t *testing.T) {
	env := setupWithNango(t)
	path := "/v1/tenant/connections/github"
	rec := env.do(t, http.MethodDelete, path, env.invokeKey, nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d; want 403", rec.Code)
	}
}

// --- MCP shortcut ---

func TestConnections_ListForToken(t *testing.T) {
	env := setupWithNango(t)
	basePath := "/v1/tenant/connections"

	// Record a connection so the list is non-empty.
	rec := env.do(t, http.MethodPost, basePath, env.manageKey, map[string]any{
		"provider": "github",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("record connection: status = %d; want 201\nbody: %s", rec.Code, rec.Body.String())
	}

	// Hit the slug-free shortcut.
	recList := env.do(t, http.MethodGet, "/v1/connections", env.manageKey, nil)
	if recList.Code != http.StatusOK {
		t.Fatalf("list for token: status = %d; want 200\nbody: %s", recList.Code, recList.Body.String())
	}
	var conns []map[string]any
	if err := json.NewDecoder(recList.Body).Decode(&conns); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, c := range conns {
		if c["provider"] == "github" {
			found = true
			break
		}
	}
	if !found {
		t.Error("github connection not found in /v1/connections response")
	}
}

// --- Proxy ---

func TestProxy_NoConnection(t *testing.T) {
	env := setupWithNango(t)

	// No connection recorded for this tenant — proxy should return 400.
	req := httptest.NewRequest(http.MethodGet, "/v1/proxy/github/user", nil)
	req.Header.Set("X-Velane-Tenant", env.tenant.ID)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400", rec.Code)
	}
}

func TestProxy_ForwardsToNango(t *testing.T) {
	env := setupWithNango(t)
	basePath := "/v1/tenant/connections"
	defaultProfile := configureProfile(t, env, "github", "default", true)

	// Record a github connection via the API.
	rec := env.do(t, http.MethodPost, basePath, env.manageKey, map[string]any{
		"provider":              "github",
		"credential_profile_id": defaultProfile["id"],
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("record connection: status = %d; want 201\nbody: %s", rec.Code, rec.Body.String())
	}

	// Proxy a request — no auth header needed, identified by X-Velane-Tenant.
	req := httptest.NewRequest(http.MethodGet, "/v1/proxy/github/user", nil)
	req.Header.Set("X-Velane-Tenant", env.tenant.ID)
	recProxy := httptest.NewRecorder()
	env.router.ServeHTTP(recProxy, req)

	if recProxy.Code != http.StatusOK {
		t.Fatalf("proxy status = %d; want 200\nbody: %s", recProxy.Code, recProxy.Body.String())
	}

	var body map[string]any
	if err := json.NewDecoder(recProxy.Body).Decode(&body); err != nil {
		t.Fatalf("decode proxy response: %v", err)
	}
	if body["login"] != "mockuser" {
		t.Errorf("login = %q; want %q", body["login"], "mockuser")
	}
	if body["provider_config_key"] != defaultProfile["nango_provider_config_key"] {
		t.Errorf("provider_config_key = %q; want %q", body["provider_config_key"], defaultProfile["nango_provider_config_key"])
	}
}

func TestProxy_UsesAliasHeader(t *testing.T) {
	env := setupWithNango(t)
	basePath := "/v1/tenant/connections"
	defaultProfile := configureProfile(t, env, "github", "default", true)
	sandboxProfile := configureProfile(t, env, "github", "sandbox", false)

	recDefault := env.do(t, http.MethodPost, basePath, env.manageKey, map[string]any{
		"provider":              "github",
		"alias":                 "default",
		"credential_profile_id": defaultProfile["id"],
	})
	if recDefault.Code != http.StatusCreated {
		t.Fatalf("record default connection: status=%d body=%s", recDefault.Code, recDefault.Body.String())
	}
	recSandbox := env.do(t, http.MethodPost, basePath, env.manageKey, map[string]any{
		"provider":              "github",
		"alias":                 "sandbox",
		"credential_profile_id": sandboxProfile["id"],
	})
	if recSandbox.Code != http.StatusCreated {
		t.Fatalf("record sandbox connection: status=%d body=%s", recSandbox.Code, recSandbox.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/proxy/github/user", nil)
	req.Header.Set("X-Velane-Tenant", env.tenant.ID)
	req.Header.Set("X-Velane-Integration-Alias", "sandbox")
	recProxy := httptest.NewRecorder()
	env.router.ServeHTTP(recProxy, req)

	if recProxy.Code != http.StatusOK {
		t.Fatalf("proxy status = %d; want 200\nbody: %s", recProxy.Code, recProxy.Body.String())
	}
	body := decodeJSON(t, recProxy)
	if body["provider_config_key"] != sandboxProfile["nango_provider_config_key"] {
		t.Errorf("provider_config_key = %q; want %q", body["provider_config_key"], sandboxProfile["nango_provider_config_key"])
	}
}

func TestProxy_MissingTenantHeader(t *testing.T) {
	env := setupWithNango(t)

	// No X-Velane-Tenant header — should return 400.
	req := httptest.NewRequest(http.MethodGet, "/v1/proxy/github/user", nil)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400", rec.Code)
	}
}

// --- Integrations catalog ---

func TestIntegrations_ConfiguredProfilesCRUD(t *testing.T) {
	env := setupWithNango(t)

	defaultProfile := configureProfile(t, env, "github", "default", true)
	secondProfile := configureProfile(t, env, "github", "sandbox", false)

	recList := env.do(t, http.MethodGet, "/v1/integrations/configured", env.manageKey, nil)
	if recList.Code != http.StatusOK {
		t.Fatalf("list configured profiles: status = %d body=%s", recList.Code, recList.Body.String())
	}
	var profiles []map[string]any
	if err := json.NewDecoder(recList.Body).Decode(&profiles); err != nil {
		t.Fatalf("decode configured profiles: %v", err)
	}
	if len(profiles) < 2 {
		t.Fatalf("expected at least 2 profiles, got %d", len(profiles))
	}

	delPath := fmt.Sprintf("/v1/integrations/configured/%s", defaultProfile["id"])
	recDel := env.do(t, http.MethodDelete, delPath, env.manageKey, nil)
	if recDel.Code != http.StatusNoContent {
		t.Fatalf("delete configured profile: status = %d body=%s", recDel.Code, recDel.Body.String())
	}

	recList2 := env.do(t, http.MethodGet, "/v1/integrations/configured", env.manageKey, nil)
	if recList2.Code != http.StatusOK {
		t.Fatalf("list configured profiles after delete: status = %d", recList2.Code)
	}
	var profiles2 []map[string]any
	if err := json.NewDecoder(recList2.Body).Decode(&profiles2); err != nil {
		t.Fatalf("decode configured profiles after delete: %v", err)
	}
	foundSecond := false
	for _, p := range profiles2 {
		if p["id"] == secondProfile["id"] {
			foundSecond = true
			if isDefault, _ := p["is_default"].(bool); !isDefault {
				t.Errorf("expected remaining profile to be auto-promoted to default")
			}
		}
	}
	if !foundSecond {
		t.Fatalf("remaining profile %v not found after deleting default", secondProfile["id"])
	}
}

func TestIntegrations_ConfiguredProfiles_SearchAndPagination(t *testing.T) {
	env := setupWithNango(t)
	configureProfile(t, env, "github", "default", true)
	configureProfile(t, env, "github", "sandbox", false)

	recFiltered := env.do(t, http.MethodGet, "/v1/integrations/configured?q=sandbox&limit=1", env.manageKey, nil)
	if recFiltered.Code != http.StatusOK {
		t.Fatalf("filtered list configured: status = %d body=%s", recFiltered.Code, recFiltered.Body.String())
	}
	var filtered []map[string]any
	if err := json.NewDecoder(recFiltered.Body).Decode(&filtered); err != nil {
		t.Fatalf("decode configured filtered: %v", err)
	}
	if len(filtered) != 1 {
		t.Fatalf("len = %d; want 1", len(filtered))
	}
	alias, _ := filtered[0]["alias"].(string)
	if alias != "sandbox" {
		t.Fatalf("alias = %q; want sandbox", alias)
	}

	recOffset := env.do(t, http.MethodGet, "/v1/integrations/configured?offset=100", env.manageKey, nil)
	if recOffset.Code != http.StatusOK {
		t.Fatalf("configured list large offset: status = %d", recOffset.Code)
	}
	var withOffset []map[string]any
	if err := json.NewDecoder(recOffset.Body).Decode(&withOffset); err != nil {
		t.Fatalf("decode configured offset: %v", err)
	}
	if len(withOffset) != 0 {
		t.Fatalf("expected empty configured list for large offset, got %d", len(withOffset))
	}
}

func TestIntegrations_ConfigureProfileResolvesProviderAuthMode(t *testing.T) {
	env := setupWithNango(t)

	rec := env.do(t, http.MethodPost, "/v1/integrations/configured", env.manageKey, map[string]any{
		"provider":         "8x8",
		"alias":            "default",
		"name":             "8x8 default",
		"credentials_type": "OAUTH2", // intentionally mismatched; backend should resolve to OAUTH2_CC
		"credentials": map[string]string{
			"client_id":     "cc-client-id",
			"client_secret": "cc-client-secret",
		},
		"is_default": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("configure 8x8 profile: status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	if body["credentials_type"] != "OAUTH2_CC" {
		t.Fatalf("credentials_type = %v; want OAUTH2_CC", body["credentials_type"])
	}
}

func TestIntegrations_ConfigureAPIKeyProfileCreatesConnection(t *testing.T) {
	env := setupWithNango(t)

	rec := env.do(t, http.MethodPost, "/v1/integrations/configured", env.manageKey, map[string]any{
		"provider":         "google-gemini",
		"alias":            "default",
		"name":             "Gemini",
		"credentials_type": "API_KEY",
		"credentials": map[string]string{
			"apiKey": "gemini-api-key",
		},
		"is_default": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("configure API key profile: status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	if body["credentials_type"] != "API_KEY" {
		t.Fatalf("credentials_type = %v; want API_KEY", body["credentials_type"])
	}

	recList := env.do(t, http.MethodGet, "/v1/tenant/connections", env.manageKey, nil)
	if recList.Code != http.StatusOK {
		t.Fatalf("list connections: status=%d body=%s", recList.Code, recList.Body.String())
	}
	var conns []map[string]any
	if err := json.NewDecoder(recList.Body).Decode(&conns); err != nil {
		t.Fatalf("decode connections: %v", err)
	}
	found := false
	for _, conn := range conns {
		if conn["provider"] == "google-gemini" {
			found = true
			if conn["nango_connection_id"] != body["nango_provider_config_key"] {
				t.Fatalf("nango_connection_id = %v; want %v", conn["nango_connection_id"], body["nango_provider_config_key"])
			}
		}
	}
	if !found {
		t.Fatal("google-gemini connection not found after API key configure")
	}

	recConfigured := env.do(t, http.MethodGet, "/v1/integrations/configured?status=connected", env.manageKey, nil)
	if recConfigured.Code != http.StatusOK {
		t.Fatalf("list connected configured profiles: status=%d body=%s", recConfigured.Code, recConfigured.Body.String())
	}
	var profiles []map[string]any
	if err := json.NewDecoder(recConfigured.Body).Decode(&profiles); err != nil {
		t.Fatalf("decode profiles: %v", err)
	}
	if len(profiles) != 1 || profiles[0]["provider"] != "google-gemini" || profiles[0]["connected"] != true {
		t.Fatalf("connected profiles = %#v; want connected google-gemini", profiles)
	}
}

func TestIntegrations_ConfigureProfileUpdateWithScopes(t *testing.T) {
	env := setupWithNango(t)
	configureProfile(t, env, "github", "default", true)

	rec := env.do(t, http.MethodPost, "/v1/integrations/configured", env.manageKey, map[string]any{
		"provider": "github",
		"alias":    "default",
		"name":     "github default updated",
		"credentials": map[string]string{
			"scopes": "repo,read:user",
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update github profile with scopes: status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	if body["name"] != "github default updated" {
		t.Fatalf("name = %v; want github default updated", body["name"])
	}
	if body["oauth_scopes"] != "repo,read:user" {
		t.Fatalf("oauth_scopes = %v; want repo,read:user", body["oauth_scopes"])
	}
}

func TestIntegrations_ConfigureProfileUpdateWithoutCredentials(t *testing.T) {
	env := setupWithNango(t)
	configureProfile(t, env, "github", "default", true)

	rec := env.do(t, http.MethodPost, "/v1/integrations/configured", env.manageKey, map[string]any{
		"provider": "github",
		"alias":    "default",
		"name":     "github renamed only",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update github profile without credentials: status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	if body["name"] != "github renamed only" {
		t.Fatalf("name = %v; want github renamed only", body["name"])
	}
}

func TestIntegrations_ListProviders(t *testing.T) {
	env := setupWithNango(t)

	// No auth required for the catalog endpoint.
	rec := env.do(t, http.MethodGet, "/v1/integrations", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200\nbody: %s", rec.Code, rec.Body.String())
	}

	var providers []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&providers); err != nil {
		t.Fatalf("decode: %v", err)
	}

	found := false
	for _, p := range providers {
		if p["unique_key"] == "github" {
			found = true
			break
		}
	}
	if !found {
		t.Error("github not found in provider list")
	}
}

func TestIntegrations_ListProviders_SearchAndLimit(t *testing.T) {
	env := setupWithNango(t)

	rec := env.do(t, http.MethodGet, "/v1/integrations?q=git&limit=1", "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200\nbody: %s", rec.Code, rec.Body.String())
	}

	var providers []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&providers); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(providers) != 1 {
		t.Fatalf("len = %d; want 1", len(providers))
	}

	name, _ := providers[0]["name"].(string)
	key, _ := providers[0]["unique_key"].(string)
	lowerName := strings.ToLower(name)
	lowerKey := strings.ToLower(key)
	if !strings.Contains(lowerName, "git") && !strings.Contains(lowerKey, "git") {
		t.Fatalf("expected provider to match query 'git'; got name=%q key=%q", name, key)
	}

	recOffset := env.do(t, http.MethodGet, "/v1/integrations?q=git&offset=100", "", nil)
	if recOffset.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200\nbody: %s", recOffset.Code, recOffset.Body.String())
	}
	var withOffset []map[string]any
	if err := json.NewDecoder(recOffset.Body).Decode(&withOffset); err != nil {
		t.Fatalf("decode offset response: %v", err)
	}
	if len(withOffset) != 0 {
		t.Fatalf("expected empty list for large offset, got %d", len(withOffset))
	}
}

func TestIntegrations_GetBundledDocs(t *testing.T) {
	env := setupWithNango(t)

	// github is in bundled metadata — should return rich docs.
	rec := env.do(t, http.MethodGet, "/v1/integrations/github/docs", env.manageKey, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200\nbody: %s", rec.Code, rec.Body.String())
	}

	body := decodeJSON(t, rec)
	if body["provider"] != "github" {
		t.Errorf("provider = %q; want %q", body["provider"], "github")
	}

	endpoints, ok := body["common_endpoints"].([]any)
	if !ok {
		t.Fatalf("common_endpoints missing or wrong type: %#v", body["common_endpoints"])
	}
	if len(endpoints) == 0 {
		t.Error("expected non-empty common_endpoints for bundled github docs")
	}

	if body["bun_example"] == nil || body["bun_example"] == "" {
		t.Error("bun_example must be set in bundled docs")
	}
}

func TestIntegrations_GetBundledDocsCaseInsensitive(t *testing.T) {
	env := setupWithNango(t)
	rec := env.do(t, http.MethodGet, "/v1/integrations/GitHub/docs", env.manageKey, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200\nbody: %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	if body["provider"] != "github" {
		t.Errorf("provider = %q; want github", body["provider"])
	}
}

func TestIntegrations_ListProvidersIncludesDocs(t *testing.T) {
	env := setupWithNango(t)
	rec := env.do(t, http.MethodGet, "/v1/integrations?q=github&limit=5", env.manageKey, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	var list []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, p := range list {
		if p["unique_key"] == "github" {
			if docs, _ := p["docs"].(string); docs == "" {
				t.Error("expected docs URL for github in list providers")
			}
			return
		}
	}
	t.Fatal("github not found in provider list")
}

func TestIntegrations_GetFallbackDocs(t *testing.T) {
	env := setupWithNango(t)

	// figma is in the mock Nango /providers list but NOT in bundled metadata
	// → should fall through to Nango and return a valid doc structure.
	rec := env.do(t, http.MethodGet, "/v1/integrations/figma/docs", env.manageKey, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200\nbody: %s", rec.Code, rec.Body.String())
	}

	body := decodeJSON(t, rec)
	if body["provider"] != "figma" {
		t.Errorf("provider = %q; want %q", body["provider"], "figma")
	}
	// Fallback docs should still have the required fields.
	if _, ok := body["common_endpoints"]; !ok {
		t.Error("common_endpoints field missing from fallback docs")
	}
	if body["bun_example"] == nil || body["bun_example"] == "" {
		t.Error("bun_example must be set even in fallback docs")
	}
}

func TestIntegrations_UnknownProvider(t *testing.T) {
	env := setupWithNango(t)

	// Provider not in bundled metadata and not in mock Nango → 404.
	rec := env.do(t, http.MethodGet, "/v1/integrations/nonexistent-xyz/docs", env.manageKey, nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d; want 404", rec.Code)
	}
}

func TestIntegrations_DocsRequiresInvokeScope(t *testing.T) {
	env := setupWithNango(t)

	// No auth key → 401.
	rec := env.do(t, http.MethodGet, "/v1/integrations/github/docs", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d; want 401", rec.Code)
	}
}
