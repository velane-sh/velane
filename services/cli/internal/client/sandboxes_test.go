package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCreateSandboxUsesMutationContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/sandboxes" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		if got := request.Header.Get("Idempotency-Key"); got != "request-123" {
			t.Fatalf("Idempotency-Key = %q", got)
		}
		if got := request.Header.Get("X-Tenant"); got != "tenant" {
			t.Fatalf("X-Tenant = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"sandbox":{"id":"sbx-1","profile_version_id":"profile-1"},"operation":{"id":"op-1","state":"queued"}}`))
	}))
	defer server.Close()

	sandbox, operation, err := New(server.URL, "tenant", "key").CreateSandbox(context.Background(), CreateSandboxRequest{Name: "test", RecipeVersionID: "recipe-v1", ProfileVersionID: "profile-1"}, "request-123")
	if err != nil {
		t.Fatalf("CreateSandbox() error = %v", err)
	}
	if sandbox.ID != "sbx-1" || operation.ID != "op-1" {
		t.Fatalf("unexpected response: %#v %#v", sandbox, operation)
	}
}

func TestGetOperationParsesRetryAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Retry-After", "3")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"op-1","state":"waiting"}`))
	}))
	defer server.Close()

	operation, retryAfter, err := New(server.URL, "tenant", "key").GetOperation(context.Background(), "op-1")
	if err != nil {
		t.Fatalf("GetOperation() error = %v", err)
	}
	if operation.State != "waiting" || retryAfter != 3 {
		t.Fatalf("GetOperation() = %#v, %d", operation, retryAfter)
	}
}

func TestGetSandboxAcceptsDetailEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sandbox":{"id":"sbx-1","name":"test"}}`))
	}))
	defer server.Close()

	sandbox, err := New(server.URL, "tenant", "key").GetSandbox(context.Background(), "sbx-1")
	if err != nil {
		t.Fatalf("GetSandbox() error = %v", err)
	}
	if sandbox.ID != "sbx-1" {
		t.Fatalf("unexpected sandbox: %#v", sandbox)
	}
}

func TestStartSandboxUsesGenerationAndOperationEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/sandboxes/sbx-1/start" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		if got := request.Header.Get("Idempotency-Key"); got != "request-123" {
			t.Fatalf("Idempotency-Key = %q", got)
		}
		if got := request.Header.Get("If-Match"); got != `"4"` {
			t.Fatalf("If-Match = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"sandbox":{"id":"sbx-1"},"operation":{"id":"op-1","state":"queued"},"replayed":false}`))
	}))
	defer server.Close()

	operation, err := New(server.URL, "tenant", "key").StartSandbox(context.Background(), "sbx-1", "request-123", "4")
	if err != nil {
		t.Fatalf("StartSandbox() error = %v", err)
	}
	if operation.ID != "op-1" || operation.State != "queued" {
		t.Fatalf("unexpected operation: %#v", operation)
	}
}

func TestStartSandboxRejectsUnexpectedSuccessStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"operation":{"id":"op-1","state":"queued"}}`))
	}))
	defer server.Close()

	_, err := New(server.URL, "tenant", "key").StartSandbox(context.Background(), "sbx-1", "request-123", "")
	if err == nil {
		t.Fatal("StartSandbox() accepted 200; mutations require 202")
	}
}

func TestGetSandboxRejectsBareSandboxPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"sbx-1","name":"test"}`))
	}))
	defer server.Close()

	_, err := New(server.URL, "tenant", "key").GetSandbox(context.Background(), "sbx-1")
	if err == nil {
		t.Fatal("GetSandbox() accepted a payload without the canonical sandbox envelope")
	}
}

func TestRequestReturnsSanitizedAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"Generation is stale.","code":"GENERATION_CONFLICT","retryable":true,"operation_id":"op-1"}`))
	}))
	defer server.Close()

	_, err := New(server.URL, "tenant", "key").GetSandbox(context.Background(), "sbx-1")
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error type = %T, want *APIError", err)
	}
	if apiErr.Code != "GENERATION_CONFLICT" || apiErr.RetryAfter != 7 || !apiErr.Retryable {
		t.Fatalf("unexpected API error: %#v", apiErr)
	}
}

func TestValidateIdempotencyKey(t *testing.T) {
	for _, key := range []string{"", "contains space", "bad\nkey", string(make([]byte, 129))} {
		if err := ValidateIdempotencyKey(key); err == nil {
			t.Errorf("ValidateIdempotencyKey(%q) succeeded", key)
		}
	}
	if err := ValidateIdempotencyKey("key-123_ABC"); err != nil {
		t.Fatalf("valid key rejected: %v", err)
	}
}

func TestRequestTimeoutIsScopedToConfiguredClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		time.Sleep(25 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"completed"}`))
	}))
	defer server.Close()

	timedClient := New(server.URL, "tenant", "key")
	timedClient.SetRequestTimeout(time.Millisecond)
	if _, err := timedClient.Invoke(context.Background(), "tenant", "snippet", "prod", `{}`); err == nil {
		t.Fatal("configured client unexpectedly completed")
	}

	legacyClient := New(server.URL, "tenant", "key")
	if _, err := legacyClient.Invoke(context.Background(), "tenant", "snippet", "prod", `{}`); err != nil {
		t.Fatalf("unconfigured legacy client inherited timeout: %v", err)
	}
}
