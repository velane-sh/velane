// Package api_test contains end-to-end HTTP tests for the full chi router.
// Tests connect to a real Postgres instance (TEST_DATABASE_URL) and use a
// mock executor httptest server — no external executor container is needed.
package api_test

import (
	"bufio"
	"bytes"
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
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/abskrj/velane/services/control-plane/internal/scheduler"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
	redisstore "github.com/abskrj/velane/services/control-plane/internal/store/redis"
	"go.uber.org/zap"
)

// testEncKey is a fixed 32-byte AES key used in all API tests.
var testEncKey = make([]byte, 32)

// --- Test harness ---

type testEnv struct {
	router    http.Handler
	store     *postgres.Store
	tenant    *models.Tenant
	manageKey string // plain key with manage+invoke+admin scopes
	invokeKey string // plain key with invoke scope only
}

// setup wires the full stack against a real test Postgres and a mock executor.
func setup(t *testing.T) *testEnv {
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

	// Mock executor: always succeeds with {"ok":true}.
	mockExec := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"output":"{\"ok\":true}","stderr":"","duration_ms":10,"peak_memory_mb":8,"exit_code":0,"error":""}`))
	}))
	t.Cleanup(mockExec.Close)

	exec := remote.New(mockExec.URL, mockExec.URL)
	sched := scheduler.New(store, exec, testEncKey, nil)
	log := zap.NewNop()
	router := api.NewRouter(store, sched, log, testEncKey, auth.NewPasswordProvider(store), nil, "", "", "", "", "", "", nil, nil)

	// Bootstrap tenant.
	slug := fmt.Sprintf("test-%d", time.Now().UnixNano())
	tenant, err := store.CreateTenant(context.Background(), "Test Tenant", slug)
	if err != nil {
		t.Fatalf("create test tenant: %v", err)
	}

	// Admin/manage key.
	_, manageKey, err := store.CreateAPIKeyWithPlain(context.Background(), tenant.ID, "manage-key",
		[]string{"admin", "manage", "invoke"})
	if err != nil {
		t.Fatalf("create manage key: %v", err)
	}

	// Invoke-only key.
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

func (e *testEnv) do(t *testing.T, method, path, key string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	} else {
		bodyReader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, path, bodyReader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func decodeJSON(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&m); err != nil {
		t.Fatalf("decode JSON body (status %d): %v\nbody: %s", rec.Code, err, rec.Body.String())
	}
	return m
}

// --- Health ---

func TestHealthz(t *testing.T) {
	env := setup(t)
	rec := env.do(t, http.MethodGet, "/healthz", "", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d; want 200", rec.Code)
	}
}

// --- Tenants ---

func TestCreateTenant(t *testing.T) {
	env := setup(t)
	slug := fmt.Sprintf("new-tenant-%d", time.Now().UnixNano())
	rec := env.do(t, http.MethodPost, "/v1/tenants", "", map[string]any{
		"name": "New Tenant",
		"slug": slug,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d; want 201\nbody: %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	if body["slug"] != slug {
		t.Errorf("slug = %q; want %q", body["slug"], slug)
	}
}

func TestCreateAPIKey(t *testing.T) {
	env := setup(t)
	path := "/v1/tenant/api-keys"
	rec := env.do(t, http.MethodPost, path, env.manageKey, map[string]any{
		"name":   "new-key",
		"scopes": []string{"invoke"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d; want 201\nbody: %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	if _, ok := body["key"]; !ok {
		t.Error("response must include key field (one-time plain key)")
	}
}

func TestCreateAPIKey_RequiresAdminScope(t *testing.T) {
	env := setup(t)
	path := "/v1/tenant/api-keys"
	rec := env.do(t, http.MethodPost, path, env.invokeKey, map[string]any{
		"name":   "bad-key",
		"scopes": []string{"invoke"},
	})
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d; want 403", rec.Code)
	}
}

// --- Snippets ---

func TestCreateSnippet(t *testing.T) {
	env := setup(t)
	rec := env.do(t, http.MethodPost, "/v1/snippets", env.manageKey, map[string]any{
		"name":     "My Snippet",
		"language": "bun",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d; want 201\nbody: %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	id, _ := body["id"].(string)
	slug, _ := body["slug"].(string)
	if id == "" {
		t.Fatal("expected non-empty id")
	}
	if slug != id {
		t.Errorf("slug = %q; want %q (slug must equal id)", slug, id)
	}
}

func TestCreateSnippet_RejectsCustomSlug(t *testing.T) {
	env := setup(t)
	rec := env.do(t, http.MethodPost, "/v1/snippets", env.manageKey, map[string]any{
		"name":     "My Snippet",
		"slug":     "custom-slug",
		"language": "bun",
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400\nbody: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateSnippet_RequiresManageScope(t *testing.T) {
	env := setup(t)
	rec := env.do(t, http.MethodPost, "/v1/snippets", env.invokeKey, map[string]any{
		"name": "Bad", "language": "bun",
	})
	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d; want 403", rec.Code)
	}
}

func TestListSnippets(t *testing.T) {
	env := setup(t)

	// Create two snippets.
	for i := 0; i < 2; i++ {
		env.do(t, http.MethodPost, "/v1/snippets", env.manageKey, map[string]any{
			"name": "Snippet", "language": "bun",
		})
	}

	rec := env.do(t, http.MethodGet, "/v1/snippets", env.manageKey, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}

	var snippets []any
	if err := json.NewDecoder(rec.Body).Decode(&snippets); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(snippets) < 2 {
		t.Errorf("expected at least 2 snippets, got %d", len(snippets))
	}
}

func TestGetSnippet_TenantIsolation(t *testing.T) {
	env := setup(t)

	// Create a snippet under env's tenant.
	rec := env.do(t, http.MethodPost, "/v1/snippets", env.manageKey, map[string]any{
		"name": "Isolation", "language": "bun",
	})
	body := decodeJSON(t, rec)
	snippetID, _ := body["id"].(string)

	// Second tenant tries to read it.
	slug2 := fmt.Sprintf("tenant2-%d", time.Now().UnixNano())
	tenant2, _ := env.store.CreateTenant(context.Background(), "T2", slug2)
	_, key2, _ := env.store.CreateAPIKeyWithPlain(context.Background(), tenant2.ID, "k2", []string{"invoke", "manage"})

	rec2 := env.do(t, http.MethodGet, "/v1/snippets/"+snippetID, key2, nil)
	if rec2.Code != http.StatusNotFound {
		t.Errorf("status = %d; want 404 (cross-tenant access must be blocked)", rec2.Code)
	}
}

func TestDeleteSnippet(t *testing.T) {
	env := setup(t)

	rec := env.do(t, http.MethodPost, "/v1/snippets", env.manageKey, map[string]any{
		"name": "Delete Me", "language": "python",
	})
	body := decodeJSON(t, rec)
	snippetID, _ := body["id"].(string)

	rec2 := env.do(t, http.MethodDelete, "/v1/snippets/"+snippetID, env.manageKey, nil)
	if rec2.Code != http.StatusNoContent {
		t.Errorf("status = %d; want 204", rec2.Code)
	}

	rec3 := env.do(t, http.MethodGet, "/v1/snippets/"+snippetID, env.manageKey, nil)
	if rec3.Code != http.StatusNotFound {
		t.Errorf("status = %d; want 404 after deletion", rec3.Code)
	}
}

// --- Versions ---

func createTestSnippet(t *testing.T, env *testEnv) (snippetID string) {
	t.Helper()
	rec := env.do(t, http.MethodPost, "/v1/snippets", env.manageKey, map[string]any{
		"name": "VSN", "language": "bun",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create snippet: status %d", rec.Code)
	}
	body := decodeJSON(t, rec)
	return body["id"].(string)
}

func TestCreateVersion(t *testing.T) {
	env := setup(t)
	snippetID := createTestSnippet(t, env)

	rec := env.do(t, http.MethodPost, "/v1/snippets/"+snippetID+"/versions", env.manageKey, map[string]any{
		"code":            "export async function handler() { return {ok: true} }",
		"input_schema":    "{}",
		"output_schema":   "{}",
		"timeout_ms":      5000,
		"max_memory_mb":   128,
		"max_cpu_percent": 100,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d; want 201\nbody: %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	if body["status"] != "draft" {
		t.Errorf("status = %q; want %q", body["status"], "draft")
	}
}

func TestCreateVersionExceedsTenantLimits(t *testing.T) {
	env := setup(t)
	snippetID := createTestSnippet(t, env)

	rec := env.do(t, http.MethodPost, "/v1/snippets/"+snippetID+"/versions", env.manageKey, map[string]any{
		"code":            "export async function handler() { return {ok: true} }",
		"timeout_ms":      5000,
		"max_memory_mb":   99999,
		"max_cpu_percent": 100,
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; want 400\nbody: %s", rec.Code, rec.Body.String())
	}
}

func TestGetRuntimeLimits(t *testing.T) {
	env := setup(t)

	rec := env.do(t, http.MethodGet, "/v1/tenant/runtime-limits", env.manageKey, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	body := decodeJSON(t, rec)
	if body["max_timeout_ms"] == nil {
		t.Fatal("expected max_timeout_ms in response")
	}
}

func TestPublishVersion(t *testing.T) {
	env := setup(t)
	snippetID := createTestSnippet(t, env)

	// Create a version.
	rec := env.do(t, http.MethodPost, "/v1/snippets/"+snippetID+"/versions", env.manageKey, map[string]any{
		"code": "export async function handler() { return {ok: true} }",
	})
	body := decodeJSON(t, rec)
	versionNum := int(body["version_number"].(float64))

	// Publish it to prod.
	path := fmt.Sprintf("/v1/snippets/%s/versions/%d/publish?env=prod", snippetID, versionNum)
	rec2 := env.do(t, http.MethodPost, path, env.manageKey, nil)
	if rec2.Code != http.StatusOK {
		t.Fatalf("publish status = %d; want 200\nbody: %s", rec2.Code, rec2.Body.String())
	}
	body2 := decodeJSON(t, rec2)
	if body2["status"] != "published" {
		t.Errorf("status = %q; want %q", body2["status"], "published")
	}
}

func TestListVersions(t *testing.T) {
	env := setup(t)
	snippetID := createTestSnippet(t, env)

	for i := 0; i < 3; i++ {
		env.do(t, http.MethodPost, "/v1/snippets/"+snippetID+"/versions", env.manageKey, map[string]any{
			"code": fmt.Sprintf("// version %d", i),
		})
	}

	rec := env.do(t, http.MethodGet, "/v1/snippets/"+snippetID+"/versions", env.manageKey, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}
	var versions []any
	if err := json.NewDecoder(rec.Body).Decode(&versions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(versions) != 3 {
		t.Errorf("got %d versions; want 3", len(versions))
	}
}

// --- Invocations ---

// publishSnippetForInvoke creates a snippet, creates a version, and publishes
// it to prod. Returns the workflow ID for the invoke URL.
func publishSnippetForInvoke(t *testing.T, env *testEnv) (workflowID string) {
	t.Helper()
	rec := env.do(t, http.MethodPost, "/v1/snippets", env.manageKey, map[string]any{
		"name": "Invoke Snippet", "language": "bun",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create snippet: %d", rec.Code)
	}
	body := decodeJSON(t, rec)
	snippetID := body["id"].(string)
	workflowID = snippetID

	rec2 := env.do(t, http.MethodPost, "/v1/snippets/"+snippetID+"/versions", env.manageKey, map[string]any{
		"code": "export async function handler(req) { return {ok: true} }",
	})
	if rec2.Code != http.StatusCreated {
		t.Fatalf("create version: %d", rec2.Code)
	}
	body2 := decodeJSON(t, rec2)
	versionNum := int(body2["version_number"].(float64))

	path := fmt.Sprintf("/v1/snippets/%s/versions/%d/publish?env=prod", snippetID, versionNum)
	rec3 := env.do(t, http.MethodPost, path, env.manageKey, nil)
	if rec3.Code != http.StatusOK {
		t.Fatalf("publish: %d\nbody: %s", rec3.Code, rec3.Body.String())
	}

	return workflowID
}

func TestInvoke_Success(t *testing.T) {
	env := setup(t)
	snippetSlug := publishSnippetForInvoke(t, env)

	path := fmt.Sprintf("/v1/invoke/%s?env=prod", snippetSlug)

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"prompt":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env.invokeKey)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200\nbody: %s", rec.Code, rec.Body.String())
	}

	body := decodeJSON(t, rec)
	if body["invocation_id"] == "" {
		t.Error("response must include invocation_id")
	}
	if body["status"] != string(models.InvocationCompleted) {
		t.Errorf("status = %q; want %q", body["status"], models.InvocationCompleted)
	}

	if rec.Header().Get("X-Invocation-Id") == "" {
		t.Error("X-Invocation-Id header must be set")
	}
	if rec.Header().Get("X-Duration-Ms") == "" {
		t.Error("X-Duration-Ms header must be set")
	}
}

func TestInvoke_RequiresInvokeScope(t *testing.T) {
	env := setup(t)
	snippetSlug := publishSnippetForInvoke(t, env)

	// Create a manage-only key (no invoke scope).
	_, manageOnly, _ := env.store.CreateAPIKeyWithPlain(context.Background(), env.tenant.ID, "manage-only", []string{"manage"})

	path := fmt.Sprintf("/v1/invoke/%s", snippetSlug)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+manageOnly)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d; want 403", rec.Code)
	}
}

func TestInvoke_InvalidJSON(t *testing.T) {
	env := setup(t)
	snippetSlug := publishSnippetForInvoke(t, env)

	path := fmt.Sprintf("/v1/invoke/%s", snippetSlug)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`not json`))
	req.Header.Set("Authorization", "Bearer "+env.invokeKey)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400", rec.Code)
	}
}

func TestInvoke_NoPublishedVersion(t *testing.T) {
	env := setup(t)
	createRec := env.do(t, http.MethodPost, "/v1/snippets", env.manageKey, map[string]any{
		"name": "No Pub", "language": "bun",
	})
	workflowID := decodeJSON(t, createRec)["id"].(string)

	path := fmt.Sprintf("/v1/invoke/%s", workflowID)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env.invokeKey)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d; want 400 (no published version)", rec.Code)
	}
}

func TestInvoke_CrossTenantKeyRejected(t *testing.T) {
	env := setup(t)
	snippetSlug := publishSnippetForInvoke(t, env)

	// A key from a different tenant.
	slug2 := fmt.Sprintf("tenant2-%d", time.Now().UnixNano())
	tenant2, _ := env.store.CreateTenant(context.Background(), "T2", slug2)
	_, key2, _ := env.store.CreateAPIKeyWithPlain(context.Background(), tenant2.ID, "k2", []string{"invoke"})

	path := fmt.Sprintf("/v1/invoke/%s", snippetSlug)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key2)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d; want 403 (cross-tenant key)", rec.Code)
	}
}

func TestGetInvocation(t *testing.T) {
	env := setup(t)
	snippetSlug := publishSnippetForInvoke(t, env)

	// Invoke to get an invocation ID.
	path := fmt.Sprintf("/v1/invoke/%s", snippetSlug)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env.invokeKey)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	invocationID := rec.Header().Get("X-Invocation-Id")
	if invocationID == "" {
		t.Fatal("X-Invocation-Id header not set")
	}

	// Fetch the invocation.
	rec2 := env.do(t, http.MethodGet, "/v1/invocations/"+invocationID, env.manageKey, nil)
	if rec2.Code != http.StatusOK {
		t.Fatalf("GetInvocation status = %d; want 200\nbody: %s", rec2.Code, rec2.Body.String())
	}
	body := decodeJSON(t, rec2)
	if body["id"] != invocationID {
		t.Errorf("id = %q; want %q", body["id"], invocationID)
	}
}

func TestGetInvocation_TenantIsolation(t *testing.T) {
	env := setup(t)
	snippetSlug := publishSnippetForInvoke(t, env)

	path := fmt.Sprintf("/v1/invoke/%s", snippetSlug)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env.invokeKey)
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)
	invocationID := rec.Header().Get("X-Invocation-Id")

	// Tenant 2 tries to read it.
	slug2 := fmt.Sprintf("t2iso-%d", time.Now().UnixNano())
	tenant2, _ := env.store.CreateTenant(context.Background(), "T2", slug2)
	_, key2, _ := env.store.CreateAPIKeyWithPlain(context.Background(), tenant2.ID, "k2", []string{"manage"})

	rec2 := env.do(t, http.MethodGet, "/v1/invocations/"+invocationID, key2, nil)
	if rec2.Code != http.StatusNotFound {
		t.Errorf("status = %d; want 404 (cross-tenant invocation access)", rec2.Code)
	}
}

// --- Auth edge cases ---

func TestUnauthenticated_Returns401(t *testing.T) {
	env := setup(t)
	for _, path := range []string{"/v1/snippets", "/v1/invocations/some-id"} {
		rec := env.do(t, http.MethodGet, path, "", nil)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s: status = %d; want 401", path, rec.Code)
		}
	}
}

// --- Phase 2: Async, Stream, Version Pinning ---

// setupWithStreaming creates a testEnv whose mock executor also handles
// POST /run/stream by returning a single SSE chunk with done:true.
func setupWithStreaming(t *testing.T) *testEnv {
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

	// Mock executor that handles both /run and /run/stream.
	mockExec := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/run/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			flusher, _ := w.(http.Flusher)
			chunk := `{"data":"{\"ok\":true}","done":true}`
			_, _ = fmt.Fprintf(w, "data: %s\n\n", chunk)
			if flusher != nil {
				flusher.Flush()
			}
		default:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"output":"{\"ok\":true}","stderr":"","duration_ms":10,"peak_memory_mb":8,"exit_code":0,"error":""}`))
		}
	}))
	t.Cleanup(mockExec.Close)

	exec := remote.New(mockExec.URL, mockExec.URL)
	sched := scheduler.New(store, exec, testEncKey, nil)
	log := zap.NewNop()
	router := api.NewRouter(store, sched, log, testEncKey, auth.NewPasswordProvider(store), nil, "", "", "", "", "", "", nil, nil)

	slug := fmt.Sprintf("test-stream-%d", time.Now().UnixNano())
	tenant, err := store.CreateTenant(context.Background(), "Stream Tenant", slug)
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

// setupWithRedis creates a testEnv wired to a real Redis (TEST_REDIS_URL).
func setupWithRedis(t *testing.T) (*testEnv, *redisstore.Client) {
	t.Helper()
	redisAddr := os.Getenv("TEST_REDIS_URL")
	if redisAddr == "" {
		t.Skip("TEST_REDIS_URL not set — skipping async integration test")
	}

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

	rc, err := redisstore.New(redisAddr)
	if err != nil {
		t.Fatalf("connect redis: %v", err)
	}
	t.Cleanup(func() { rc.Close() })

	mockExec := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"output":"{\"ok\":true}","stderr":"","duration_ms":10,"peak_memory_mb":8,"exit_code":0,"error":""}`))
	}))
	t.Cleanup(mockExec.Close)

	exec := remote.New(mockExec.URL, mockExec.URL)
	sched := scheduler.NewWithQueue(store, exec, rc, testEncKey, nil)
	log := zap.NewNop()
	router := api.NewRouter(store, sched, log, testEncKey, auth.NewPasswordProvider(store), nil, "", "", "", "", "", "", nil, nil)

	slug := fmt.Sprintf("test-async-%d", time.Now().UnixNano())
	tenant, err := store.CreateTenant(context.Background(), "Async Tenant", slug)
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
	}, rc
}

func TestInvoke_AsyncMode(t *testing.T) {
	env, _ := setupWithRedis(t)
	snippetSlug := publishSnippetForInvoke(t, env)

	path := fmt.Sprintf("/v1/invoke/%s?env=prod", snippetSlug)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env.invokeKey)
	req.Header.Set("X-Invoke-Mode", "async")
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d; want 202\nbody: %s", rec.Code, rec.Body.String())
	}

	body := decodeJSON(t, rec)
	invocationID, _ := body["invocation_id"].(string)
	if invocationID == "" {
		t.Fatal("response must include invocation_id")
	}

	statusVal, _ := body["status"].(string)
	if statusVal != string(models.InvocationPending) {
		t.Errorf("status = %q; want %q", statusVal, models.InvocationPending)
	}

	statusURL, _ := body["status_url"].(string)
	if statusURL == "" {
		t.Error("response must include status_url")
	}

	// Poll GET /v1/invocations/{id} — the invocation should exist in the DB.
	rec2 := env.do(t, http.MethodGet, "/v1/invocations/"+invocationID, env.manageKey, nil)
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET invocation status = %d; want 200\nbody: %s", rec2.Code, rec2.Body.String())
	}
	body2 := decodeJSON(t, rec2)
	if body2["id"] != invocationID {
		t.Errorf("invocation id = %q; want %q", body2["id"], invocationID)
	}
}

func TestInvoke_VersionPinning(t *testing.T) {
	env := setup(t)

	// Create snippet.
	rec := env.do(t, http.MethodPost, "/v1/snippets", env.manageKey, map[string]any{
		"name": "Pin Snippet", "language": "bun",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create snippet: %d", rec.Code)
	}
	snippetID := decodeJSON(t, rec)["id"].(string)
	snippetSlug := snippetID

	// Create version 1 with distinctive code.
	rec2 := env.do(t, http.MethodPost, "/v1/snippets/"+snippetID+"/versions", env.manageKey, map[string]any{
		"code": "export async function handler() { return {version: 1} }",
	})
	if rec2.Code != http.StatusCreated {
		t.Fatalf("create version 1: %d", rec2.Code)
	}
	v1Num := int(decodeJSON(t, rec2)["version_number"].(float64))

	// Publish version 1.
	pub1Path := fmt.Sprintf("/v1/snippets/%s/versions/%d/publish?env=prod", snippetID, v1Num)
	env.do(t, http.MethodPost, pub1Path, env.manageKey, nil)

	// Create version 2.
	rec3 := env.do(t, http.MethodPost, "/v1/snippets/"+snippetID+"/versions", env.manageKey, map[string]any{
		"code": "export async function handler() { return {version: 2} }",
	})
	if rec3.Code != http.StatusCreated {
		t.Fatalf("create version 2: %d", rec3.Code)
	}
	v2Num := int(decodeJSON(t, rec3)["version_number"].(float64))

	// Publish version 2 (now active).
	pub2Path := fmt.Sprintf("/v1/snippets/%s/versions/%d/publish?env=prod", snippetID, v2Num)
	env.do(t, http.MethodPost, pub2Path, env.manageKey, nil)

	// Invoke with ?version=v1 — should use version 1 (pinned).
	invPath := fmt.Sprintf("/v1/invoke/%s?env=prod&version=v%d", snippetSlug, v1Num)
	req := httptest.NewRequest(http.MethodPost, invPath, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env.invokeKey)
	rec4 := httptest.NewRecorder()
	env.router.ServeHTTP(rec4, req)

	if rec4.Code != http.StatusOK {
		t.Fatalf("pin invoke status = %d; want 200\nbody: %s", rec4.Code, rec4.Body.String())
	}

	// The invocation should record version 1's ID.
	invocationID := rec4.Header().Get("X-Invocation-Id")
	if invocationID == "" {
		t.Fatal("X-Invocation-Id not set")
	}

	inv, err := env.store.GetInvocation(context.Background(), invocationID)
	if err != nil {
		t.Fatalf("GetInvocation: %v", err)
	}

	// Verify the version recorded matches v1, not v2.
	v1, _ := env.store.GetVersionByNumber(context.Background(), snippetID, v1Num)
	if inv.VersionID != v1.ID {
		t.Errorf("VersionID = %q; want v1 ID %q (pinned)", inv.VersionID, v1.ID)
	}
}

func TestInvoke_StreamMode(t *testing.T) {
	env := setupWithStreaming(t)
	snippetSlug := publishSnippetForInvoke(t, env)

	path := fmt.Sprintf("/v1/invoke/%s?env=prod", snippetSlug)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env.invokeKey)
	req.Header.Set("X-Invoke-Mode", "stream")
	rec := httptest.NewRecorder()
	env.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200\nbody: %s", rec.Code, rec.Body.String())
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q; want text/event-stream", ct)
	}

	if rec.Header().Get("X-Invocation-Id") == "" {
		t.Error("X-Invocation-Id header must be set for stream response")
	}

	// Parse SSE events from the body.
	scanner := bufio.NewScanner(rec.Body)
	var events []string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			events = append(events, strings.TrimPrefix(line, "data: "))
		}
	}

	if len(events) == 0 {
		t.Fatal("expected at least one SSE data event, got none")
	}

	// The last event should be the final done event.
	lastEvent := events[len(events)-1]
	var chunk map[string]any
	if err := json.Unmarshal([]byte(lastEvent), &chunk); err != nil {
		t.Fatalf("unmarshal last SSE event %q: %v", lastEvent, err)
	}
	if done, _ := chunk["done"].(bool); !done {
		t.Errorf("last SSE event done = %v; want true. event: %s", done, lastEvent)
	}
}

// --- Phase 3: Secrets ---

func TestCreateSecret(t *testing.T) {
	env := setup(t)

	rec := env.do(t, http.MethodPost, "/v1/secrets", env.manageKey, map[string]any{
		"name":         "MY_API_KEY",
		"value":        "super-secret-value",
		"environments": []string{"prod"},
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d; want 201\nbody: %s", rec.Code, rec.Body.String())
	}

	body := decodeJSON(t, rec)
	if body["id"] == "" {
		t.Error("id must be set")
	}
	if body["name"] != "MY_API_KEY" {
		t.Errorf("name = %q; want %q", body["name"], "MY_API_KEY")
	}
	// Value must never appear in the response.
	if _, ok := body["value"]; ok {
		t.Error("value must not appear in response")
	}
	if _, ok := body["value_encrypted"]; ok {
		t.Error("value_encrypted must not appear in response")
	}
}

func TestListSecrets(t *testing.T) {
	env := setup(t)

	// Create two secrets.
	env.do(t, http.MethodPost, "/v1/secrets", env.manageKey, map[string]any{
		"name": "SECRET_A", "value": "val-a",
	})
	env.do(t, http.MethodPost, "/v1/secrets", env.manageKey, map[string]any{
		"name": "SECRET_B", "value": "val-b",
	})

	rec := env.do(t, http.MethodGet, "/v1/secrets", env.manageKey, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200", rec.Code)
	}

	var secrets []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&secrets); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(secrets) < 2 {
		t.Errorf("expected at least 2 secrets, got %d", len(secrets))
	}

	// Values must never appear.
	for _, s := range secrets {
		if _, ok := s["value"]; ok {
			t.Error("value must not appear in list response")
		}
		if _, ok := s["value_encrypted"]; ok {
			t.Error("value_encrypted must not appear in list response")
		}
	}
}

func TestDeleteSecret(t *testing.T) {
	env := setup(t)

	// Create a secret.
	recCreate := env.do(t, http.MethodPost, "/v1/secrets", env.manageKey, map[string]any{
		"name": "TO_DELETE", "value": "delete-me",
	})
	if recCreate.Code != http.StatusCreated {
		t.Fatalf("create: %d", recCreate.Code)
	}
	secretID := decodeJSON(t, recCreate)["id"].(string)

	// Delete it.
	recDel := env.do(t, http.MethodDelete, "/v1/secrets/"+secretID, env.manageKey, nil)
	if recDel.Code != http.StatusNoContent {
		t.Fatalf("delete: %d; want 204\nbody: %s", recDel.Code, recDel.Body.String())
	}

	// Verify gone from list.
	recList := env.do(t, http.MethodGet, "/v1/secrets", env.manageKey, nil)
	var secrets []map[string]any
	_ = json.NewDecoder(recList.Body).Decode(&secrets)
	for _, s := range secrets {
		if s["id"] == secretID {
			t.Error("deleted secret still appears in list")
		}
	}
}

// --- Phase 3: Canary ---

func TestSetCanary(t *testing.T) {
	env := setup(t)
	snippetID := createTestSnippet(t, env)

	// Create and publish version 1.
	recV1 := env.do(t, http.MethodPost, "/v1/snippets/"+snippetID+"/versions", env.manageKey, map[string]any{
		"code": "// v1",
	})
	v1Body := decodeJSON(t, recV1)
	v1Num := int(v1Body["version_number"].(float64))
	v1ID := v1Body["id"].(string)
	env.do(t, http.MethodPost,
		fmt.Sprintf("/v1/snippets/%s/versions/%d/publish?env=prod", snippetID, v1Num),
		env.manageKey, nil)

	// Create version 2 (canary candidate).
	recV2 := env.do(t, http.MethodPost, "/v1/snippets/"+snippetID+"/versions", env.manageKey, map[string]any{
		"code": "// v2",
	})
	v2Body := decodeJSON(t, recV2)
	v2ID := v2Body["id"].(string)

	_ = v1ID // used for context

	// Set canary.
	recCanary := env.do(t, http.MethodPost,
		"/v1/snippets/"+snippetID+"/canary?env=prod",
		env.manageKey,
		map[string]any{
			"version_id": v2ID,
			"percent":    25,
		},
	)
	if recCanary.Code != http.StatusOK {
		t.Fatalf("SetCanary status = %d; want 200\nbody: %s", recCanary.Code, recCanary.Body.String())
	}

	canaryBody := decodeJSON(t, recCanary)
	if canaryBody["canary_version_id"] != v2ID {
		t.Errorf("canary_version_id = %v; want %q", canaryBody["canary_version_id"], v2ID)
	}
	if int(canaryBody["canary_pct"].(float64)) != 25 {
		t.Errorf("canary_pct = %v; want 25", canaryBody["canary_pct"])
	}
}

func TestClearCanary(t *testing.T) {
	env := setup(t)
	snippetID := createTestSnippet(t, env)

	// Create and publish v1.
	recV1 := env.do(t, http.MethodPost, "/v1/snippets/"+snippetID+"/versions", env.manageKey, map[string]any{
		"code": "// v1",
	})
	v1Body := decodeJSON(t, recV1)
	v1Num := int(v1Body["version_number"].(float64))
	env.do(t, http.MethodPost,
		fmt.Sprintf("/v1/snippets/%s/versions/%d/publish?env=prod", snippetID, v1Num),
		env.manageKey, nil)

	// Create v2 and set as canary.
	recV2 := env.do(t, http.MethodPost, "/v1/snippets/"+snippetID+"/versions", env.manageKey, map[string]any{
		"code": "// v2",
	})
	v2ID := decodeJSON(t, recV2)["id"].(string)

	env.do(t, http.MethodPost, "/v1/snippets/"+snippetID+"/canary?env=prod", env.manageKey,
		map[string]any{"version_id": v2ID, "percent": 50})

	// Clear canary.
	recClear := env.do(t, http.MethodDelete,
		"/v1/snippets/"+snippetID+"/canary?env=prod",
		env.manageKey, nil)
	if recClear.Code != http.StatusNoContent {
		t.Fatalf("ClearCanary status = %d; want 204\nbody: %s", recClear.Code, recClear.Body.String())
	}
}

// --- Phase 3: Egress policy ---

func TestUpdateEgressPolicy(t *testing.T) {
	env := setup(t)

	// GET current policy.
	recGet := env.do(t, http.MethodGet,
		"/v1/tenant/egress",
		env.manageKey, nil)
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET egress status = %d; want 200\nbody: %s", recGet.Code, recGet.Body.String())
	}
	getBody := decodeJSON(t, recGet)
	// Default policy should have blocked_cidrs.
	if _, ok := getBody["blocked_cidrs"]; !ok {
		t.Error("default policy should have blocked_cidrs field")
	}

	// PUT a new policy.
	recPut := env.do(t, http.MethodPut,
		"/v1/tenant/egress",
		env.manageKey,
		map[string]any{
			"blocked_cidrs":   []string{"192.168.1.0/24"},
			"blocked_domains": []string{"evil.example.com"},
		},
	)
	if recPut.Code != http.StatusOK {
		t.Fatalf("PUT egress status = %d; want 200\nbody: %s", recPut.Code, recPut.Body.String())
	}

	putBody := decodeJSON(t, recPut)
	cidrs, _ := putBody["blocked_cidrs"].([]any)
	if len(cidrs) != 1 || cidrs[0] != "192.168.1.0/24" {
		t.Errorf("blocked_cidrs = %v; want [192.168.1.0/24]", cidrs)
	}
	domains, _ := putBody["blocked_domains"].([]any)
	if len(domains) != 1 || domains[0] != "evil.example.com" {
		t.Errorf("blocked_domains = %v; want [evil.example.com]", domains)
	}

	// GET again and verify persistence.
	recGet2 := env.do(t, http.MethodGet,
		"/v1/tenant/egress",
		env.manageKey, nil)
	if recGet2.Code != http.StatusOK {
		t.Fatalf("GET egress after update status = %d", recGet2.Code)
	}
	getBody2 := decodeJSON(t, recGet2)
	cidrs2, _ := getBody2["blocked_cidrs"].([]any)
	if len(cidrs2) != 1 || cidrs2[0] != "192.168.1.0/24" {
		t.Errorf("persisted blocked_cidrs = %v; want [192.168.1.0/24]", cidrs2)
	}
}

// --- Phase 5: logs, metrics, replay ---

func TestGetSnippetLogs(t *testing.T) {
	env := setup(t)
	snippetID := createTestSnippet(t, env)

	// Create invocation entries so logs endpoint has data.
	vRec := env.do(t, http.MethodPost, "/v1/snippets/"+snippetID+"/versions", env.manageKey, map[string]any{
		"code": "export async function handler() { return {ok:true} }",
	})
	vNum := int(decodeJSON(t, vRec)["version_number"].(float64))
	env.do(t, http.MethodPost, fmt.Sprintf("/v1/snippets/%s/versions/%d/publish?env=prod", snippetID, vNum), env.manageKey, nil)
	invokePath := fmt.Sprintf("/v1/invoke/%s?env=prod", snippetID)
	req := httptest.NewRequest(http.MethodPost, invokePath, strings.NewReader(`{"a":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env.invokeKey)
	recInvoke := httptest.NewRecorder()
	env.router.ServeHTTP(recInvoke, req)
	if recInvoke.Code != http.StatusOK {
		t.Fatalf("invoke status = %d; want 200\nbody: %s", recInvoke.Code, recInvoke.Body.String())
	}

	rec := env.do(t, http.MethodGet, "/v1/logs/snippets/"+snippetID+"?limit=10&env=prod", env.manageKey, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200\nbody: %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	items, ok := body["items"].([]any)
	if !ok {
		t.Fatalf("items missing or invalid type: %#v", body["items"])
	}
	if len(items) == 0 {
		t.Fatal("expected at least one log item")
	}
}

func TestGetSnippetMetrics(t *testing.T) {
	env := setup(t)
	snippetID := createTestSnippet(t, env)

	// Create invocation data.
	vRec := env.do(t, http.MethodPost, "/v1/snippets/"+snippetID+"/versions", env.manageKey, map[string]any{
		"code": "export async function handler() { return {ok:true} }",
	})
	vNum := int(decodeJSON(t, vRec)["version_number"].(float64))
	env.do(t, http.MethodPost, fmt.Sprintf("/v1/snippets/%s/versions/%d/publish?env=prod", snippetID, vNum), env.manageKey, nil)
	invokePath := fmt.Sprintf("/v1/invoke/%s?env=prod", snippetID)
	req := httptest.NewRequest(http.MethodPost, invokePath, strings.NewReader(`{"a":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env.invokeKey)
	recInvoke := httptest.NewRecorder()
	env.router.ServeHTTP(recInvoke, req)
	if recInvoke.Code != http.StatusOK {
		t.Fatalf("invoke status = %d; want 200\nbody: %s", recInvoke.Code, recInvoke.Body.String())
	}

	rec := env.do(t, http.MethodGet, "/v1/metrics/snippets/"+snippetID+"?window=24h", env.manageKey, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200\nbody: %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	if body["aggregates"] == nil {
		t.Fatal("aggregates must be present")
	}
}

func TestReplayInvocation(t *testing.T) {
	env := setup(t)
	snippetSlug := publishSnippetForInvoke(t, env)

	// Create an invocation so replay route can resolve the ID.
	path := fmt.Sprintf("/v1/invoke/%s", snippetSlug)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env.invokeKey)
	recInvoke := httptest.NewRecorder()
	env.router.ServeHTTP(recInvoke, req)
	if recInvoke.Code != http.StatusOK {
		t.Fatalf("invoke status = %d; want 200\nbody: %s", recInvoke.Code, recInvoke.Body.String())
	}
	invocationID := recInvoke.Header().Get("X-Invocation-Id")
	if invocationID == "" {
		t.Fatal("X-Invocation-Id header not set")
	}

	_, err := env.store.UpdateReplayEnabled(context.Background(), env.tenant.ID, true)
	if err != nil {
		t.Fatalf("enable replay: %v", err)
	}

	rec := env.do(t, http.MethodPost, "/v1/invocations/"+invocationID+"/replay", env.manageKey, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; want 200\nbody: %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	if body["replay_invocation_id"] == "" {
		t.Fatal("replay_invocation_id must be set")
	}
}

func TestReplayInvocation_RequiresManageScope(t *testing.T) {
	env := setup(t)
	snippetSlug := publishSnippetForInvoke(t, env)

	path := fmt.Sprintf("/v1/invoke/%s", snippetSlug)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env.invokeKey)
	recInvoke := httptest.NewRecorder()
	env.router.ServeHTTP(recInvoke, req)
	invocationID := recInvoke.Header().Get("X-Invocation-Id")
	if invocationID == "" {
		t.Fatal("X-Invocation-Id header not set")
	}

	_, err := env.store.UpdateReplayEnabled(context.Background(), env.tenant.ID, true)
	if err != nil {
		t.Fatalf("enable replay: %v", err)
	}

	// invoke-only key should be rejected by manage-scope middleware.
	rec := env.do(t, http.MethodPost, "/v1/invocations/"+invocationID+"/replay", env.invokeKey, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d; want 403\nbody: %s", rec.Code, rec.Body.String())
	}
}
