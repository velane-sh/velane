package controlplane

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetReturnsStatusError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
	}))
	defer server.Close()

	err := New(server.URL).Get(context.Background(), "Bearer test", "/v1/test", nil)
	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected StatusError, got %T: %v", err, err)
	}
	if statusErr.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d; want %d", statusErr.StatusCode, http.StatusForbidden)
	}
	if statusErr.Body != `{"error":"forbidden"}` {
		t.Fatalf("body = %q", statusErr.Body)
	}
	if got, want := err.Error(), `control-plane error (403): {"error":"forbidden"}`; got != want {
		t.Fatalf("error = %q; want %q", got, want)
	}
}
