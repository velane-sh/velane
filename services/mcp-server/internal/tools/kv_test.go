package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/abskrj/velane/services/mcp-server/internal/controlplane"
)

func TestKVSetForwardsJSONValuesAndRequestContract(t *testing.T) {
	type receivedRequest struct {
		rawQuery string
		value    any
		hasTTL   bool
		ttl      any
	}
	var received []receivedRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertKVHeaders(t, r)
		if r.Method != http.MethodPut || r.URL.Path != "/v1/kv/entry" {
			http.Error(w, "unexpected route", http.StatusNotFound)
			return
		}
		if !strings.Contains(r.URL.RawQuery, "key=user%2F42") {
			http.Error(w, "key was not query encoded", http.StatusBadRequest)
			return
		}

		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var value any
		if err := json.Unmarshal(body["value"], &value); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ttl, hasTTL := body["ttl_seconds"]
		var decodedTTL any
		if hasTTL {
			if err := json.Unmarshal(ttl, &decodedTTL); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		received = append(received, receivedRequest{
			rawQuery: r.URL.RawQuery,
			value:    value,
			hasTTL:   hasTTL,
			ttl:      decodedTTL,
		})
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key":"user/42"}`))
	}))
	defer server.Close()

	registry := NewRegistry(controlplane.New(server.URL))
	tests := []struct {
		name  string
		value any
		ttl   any
	}{
		{name: "object", value: map[string]any{"cursor": "abc"}, ttl: 60},
		{name: "array", value: []any{"a", float64(2), true}},
		{name: "string", value: "hello"},
		{name: "number", value: float64(42.5)},
		{name: "boolean", value: true},
		{name: "null", value: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]any{"key": "user/42", "value": tt.value}
			if tt.ttl != nil {
				args["ttl_seconds"] = tt.ttl
			}
			if _, err := registry.Call(context.Background(), "Bearer test", "kv_set", args); err != nil {
				t.Fatalf("kv_set: %v", err)
			}
		})
	}

	if len(received) != len(tests) {
		t.Fatalf("received %d requests; want %d", len(received), len(tests))
	}
	for i, tt := range tests {
		if !reflect.DeepEqual(received[i].value, tt.value) {
			t.Errorf("%s value = %#v; want %#v", tt.name, received[i].value, tt.value)
		}
	}
	if !received[0].hasTTL || received[0].ttl != float64(60) {
		t.Fatalf("ttl request = %#v; want 60", received[0])
	}
	for _, request := range received[1:] {
		if request.hasTTL {
			t.Fatalf("ttl_seconds should be omitted, got request %#v", request)
		}
	}
}

func TestKVGetDeleteAndListSemantics(t *testing.T) {
	var listQueries []map[string][]string
	getStatus := http.StatusNotFound
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertKVHeaders(t, r)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/kv/entry":
			http.Error(w, `{"error":"permission denied"}`, getStatus)
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/kv/entry":
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/kv/entries":
			listQueries = append(listQueries, r.URL.Query())
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[],"total":0}`))
		default:
			http.Error(w, "unexpected route", http.StatusNotFound)
		}
	}))
	defer server.Close()

	registry := NewRegistry(controlplane.New(server.URL))
	result, err := registry.Call(context.Background(), "Bearer test", "kv_get", map[string]any{"key": "missing"})
	if err != nil || result != nil {
		t.Fatalf("kv_get missing = (%#v, %v); want (nil, nil)", result, err)
	}

	result, err = registry.Call(context.Background(), "Bearer test", "kv_delete", map[string]any{"key": "missing"})
	if err != nil {
		t.Fatalf("kv_delete missing: %v", err)
	}
	if got, want := result, map[string]bool{"deleted": false}; !reflect.DeepEqual(got, want) {
		t.Fatalf("kv_delete result = %#v; want %#v", got, want)
	}

	getStatus = http.StatusForbidden
	if _, err := registry.Call(context.Background(), "Bearer test", "kv_get", map[string]any{"key": "restricted"}); err == nil || !strings.Contains(err.Error(), "permission") {
		t.Fatalf("kv_get 403 error = %v; want permission error", err)
	}
	getStatus = http.StatusInternalServerError
	if _, err := registry.Call(context.Background(), "Bearer test", "kv_get", map[string]any{"key": "broken"}); err == nil {
		t.Fatal("kv_get 500 should return an error")
	}

	if _, err := registry.Call(context.Background(), "Bearer test", "kv_list", map[string]any{}); err != nil {
		t.Fatalf("kv_list all namespaces: %v", err)
	}
	if _, err := registry.Call(context.Background(), "Bearer test", "kv_list", map[string]any{"namespace": "sync", "prefix": "%_"}); err != nil {
		t.Fatalf("kv_list namespace: %v", err)
	}
	if _, err := registry.Call(context.Background(), "Bearer test", "kv_list", map[string]any{"namespace": ""}); err != nil {
		t.Fatalf("kv_list default namespace: %v", err)
	}
	if len(listQueries) != 3 {
		t.Fatalf("list requests = %d; want 3", len(listQueries))
	}
	if _, ok := listQueries[0]["namespace"]; ok {
		t.Fatalf("all-namespaces request included namespace: %#v", listQueries[0])
	}
	if got := listQueries[1]["namespace"]; !reflect.DeepEqual(got, []string{"sync"}) {
		t.Fatalf("namespace = %#v; want sync", got)
	}
	if got := listQueries[1]["prefix"]; !reflect.DeepEqual(got, []string{"%_"}) {
		t.Fatalf("prefix = %#v; want %%_", got)
	}
	if got := listQueries[2]["namespace"]; !reflect.DeepEqual(got, []string{"default"}) {
		t.Fatalf("explicit empty namespace = %#v; want default", got)
	}
}

func assertKVHeaders(t *testing.T, r *http.Request) {
	t.Helper()
	if got, want := r.Header.Get("Authorization"), "Bearer test"; got != want {
		t.Errorf("authorization = %q; want %q", got, want)
	}
	if got := r.Header.Get("X-Velane-Tenant"); got != "" {
		t.Errorf("unexpected X-Velane-Tenant header: %q", got)
	}
}
