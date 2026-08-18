package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateSandboxImageRecipeUsesCanonicalPayloadAndEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/sandbox-image-recipes" {
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
		if got := request.Header.Get("Idempotency-Key"); got != "request-123" {
			t.Fatalf("Idempotency-Key = %q", got)
		}
		var body map[string]string
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["name"] != "Python" || body["slug"] != "python-3" || body["description"] != "Pinned Python image" {
			t.Fatalf("unexpected payload: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"recipe":{"id":"recipe-1","slug":"python-3"},"replayed":false}`))
	}))
	defer server.Close()

	recipe, err := New(server.URL, "tenant", "key").CreateSandboxImageRecipe(context.Background(), "Python", "python-3", "Pinned Python image", "request-123")
	if err != nil {
		t.Fatalf("CreateSandboxImageRecipe() error = %v", err)
	}
	if recipe.ID != "recipe-1" || recipe.Slug != "python-3" {
		t.Fatalf("unexpected recipe: %#v", recipe)
	}
}

func TestDeleteSandboxImageRecipeRequiresNoContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	if err := New(server.URL, "tenant", "key").DeleteSandboxImageRecipe(context.Background(), "recipe-1", "request-123"); err == nil {
		t.Fatal("DeleteSandboxImageRecipe() accepted 200; canonical delete returns 204")
	}
}
