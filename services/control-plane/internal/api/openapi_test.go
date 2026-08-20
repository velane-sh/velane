package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apicontract "github.com/abskrj/velane/services/control-plane/internal/api/openapi"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

func loadOpenAPIDocument(t *testing.T) *openapi3.T {
	t.Helper()

	doc, err := openapi3.NewLoader().LoadFromData(apicontract.Document())
	if err != nil {
		t.Fatalf("load OpenAPI document: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("validate OpenAPI document: %v", err)
	}
	return doc
}

func TestOpenAPIDocumentIsValid(t *testing.T) {
	loadOpenAPIDocument(t)
}

func TestOpenAPIRoutesStayInSync(t *testing.T) {
	doc := loadOpenAPIDocument(t)
	router := NewRouter(
		nil,
		nil,
		zap.NewNop(),
		make([]byte, 32),
		nil,
		nil,
		"",
		"",
		"",
		"",
		"",
		"",
		nil,
		nil,
	)

	routerOperations := make(map[string]struct{})
	if err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(route, "/v1/proxy/") || strings.HasPrefix(route, "/v1/internal/") {
			return nil
		}
		if route == "/v1/nango-assets/*" {
			route = "/v1/nango-assets/{assetPath}"
		}
		routerOperations[method+" "+route] = struct{}{}
		return nil
	}); err != nil {
		t.Fatalf("walk router: %v", err)
	}

	specOperations := make(map[string]struct{})
	for path, item := range doc.Paths.Map() {
		for method, operation := range map[string]*openapi3.Operation{
			http.MethodGet:    item.Get,
			http.MethodPost:   item.Post,
			http.MethodPut:    item.Put,
			http.MethodPatch:  item.Patch,
			http.MethodDelete: item.Delete,
		} {
			if operation != nil {
				specOperations[method+" "+path] = struct{}{}
			}
		}
	}

	for operation := range routerOperations {
		if _, ok := specOperations[operation]; !ok {
			t.Errorf("router operation missing from OpenAPI document: %s", operation)
		}
	}

	// Social-login routes are registered only when OAuth and JWT auth are
	// configured, but they remain part of the public API contract.
	conditionalOperations := map[string]struct{}{
		"GET /v1/admin/auth/oauth/providers":           {},
		"GET /v1/admin/auth/oauth/{provider}/start":    {},
		"GET /v1/admin/auth/oauth/{provider}/callback": {},
		// Sandbox routes are mounted only when SANDBOX_CONTROL_ENABLED is set.
		"GET /v1/sandboxes":                                                  {},
		"POST /v1/sandboxes":                                                 {},
		"GET /v1/sandboxes/{sandboxID}":                                      {},
		"POST /v1/sandboxes/{sandboxID}/start":                               {},
		"POST /v1/sandboxes/{sandboxID}/stop":                                {},
		"POST /v1/sandboxes/{sandboxID}/restart":                             {},
		"POST /v1/sandboxes/{sandboxID}/retry":                               {},
		"DELETE /v1/sandboxes/{sandboxID}":                                   {},
		"POST /v1/sandboxes/{sandboxID}/snapshots":                           {},
		"GET /v1/sandboxes/{sandboxID}/snapshots":                            {},
		"GET /v1/sandboxes/{sandboxID}/snapshots/{snapshotID}":               {},
		"POST /v1/sandboxes/{sandboxID}/snapshots/{snapshotID}/restore":      {},
		"DELETE /v1/sandboxes/{sandboxID}/snapshots/{snapshotID}":            {},
		"GET /v1/sandbox-operations/{operationID}":                           {},
		"GET /v1/sandbox-profiles":                                           {},
		"GET /v1/sandboxes/{sandboxID}/events":                               {},
		"GET /v1/sandboxes/{sandboxID}/logs":                                 {},
		"GET /v1/sandbox-image-recipes":                                      {},
		"POST /v1/sandbox-image-recipes":                                     {},
		"GET /v1/sandbox-image-recipes/{recipeID}":                           {},
		"DELETE /v1/sandbox-image-recipes/{recipeID}":                        {},
		"GET /v1/sandbox-image-recipes/{recipeID}/versions":                  {},
		"POST /v1/sandbox-image-recipes/{recipeID}/versions":                 {},
		"GET /v1/sandbox-image-recipes/{recipeID}/versions/{version}":        {},
		"GET /v1/sandbox-image-recipes/{recipeID}/versions/{version}/events": {},
		"GET /v1/sandbox-image-recipes/{recipeID}/versions/{version}/logs":   {},
	}
	for operation := range specOperations {
		if _, ok := routerOperations[operation]; ok {
			continue
		}
		if _, ok := conditionalOperations[operation]; ok {
			continue
		}
		t.Errorf("OpenAPI operation missing from router: %s", operation)
	}
}

func TestKVOpenAPIContract(t *testing.T) {
	doc := loadOpenAPIDocument(t)

	expectedScopes := map[string]string{
		"GET /v1/kv/entries":    "invoke",
		"GET /v1/kv/entry":      "invoke",
		"PUT /v1/kv/entry":      "manage",
		"DELETE /v1/kv/entry":   "manage",
		"GET /v1/kv/namespaces": "invoke",
		"POST /v1/kv/reveal":    "admin",
	}
	for operation, wantScope := range expectedScopes {
		parts := strings.SplitN(operation, " ", 2)
		item := doc.Paths.Find(parts[1])
		if item == nil {
			t.Errorf("OpenAPI path %s is missing", parts[1])
			continue
		}

		var actual *openapi3.Operation
		switch parts[0] {
		case http.MethodGet:
			actual = item.Get
		case http.MethodPut:
			actual = item.Put
		case http.MethodDelete:
			actual = item.Delete
		case http.MethodPost:
			actual = item.Post
		}
		if actual == nil {
			t.Errorf("OpenAPI operation %s is missing", operation)
			continue
		}
		if got, _ := actual.Extensions["x-required-scope"].(string); got != wantScope {
			t.Errorf("%s x-required-scope = %q, want %q", operation, got, wantScope)
		}
	}

	entries := doc.Paths.Find("/v1/kv/entries").Get
	var namespace *openapi3.Parameter
	for _, parameter := range entries.Parameters {
		if parameter.Value.Name == "namespace" {
			namespace = parameter.Value
			break
		}
	}
	if namespace == nil || namespace.Required {
		t.Error("GET /v1/kv/entries namespace parameter must be optional")
	} else if !strings.Contains(namespace.Description, "all namespaces") || !strings.Contains(namespace.Description, "default") {
		t.Errorf("GET /v1/kv/entries namespace description = %q, want all-namespace omission semantics", namespace.Description)
	}

	put := doc.Paths.Find("/v1/kv/entry").Put
	if _, ok := put.Responses.Map()["409"]; !ok {
		t.Error("PUT /v1/kv/entry must document 409 quota conflicts")
	}
	if response, ok := put.Responses.Map()["413"]; !ok {
		t.Error("PUT /v1/kv/entry must document 413 size limits")
	} else if description := response.Value.Description; description == nil || !strings.Contains(*description, "raw") || !strings.Contains(*description, "canonical") {
		t.Errorf("PUT /v1/kv/entry 413 description = %v, want raw and canonical size limits", description)
	}
	if !strings.Contains(put.Description, "clears any existing expiry") {
		t.Errorf("PUT /v1/kv/entry description = %q, want TTL-clearing semantics", put.Description)
	}

	reveal := doc.Paths.Find("/v1/kv/reveal").Post
	for _, phrase := range []string{"plaintext", "auditing and UX", "not a confidentiality"} {
		if !strings.Contains(reveal.Description, phrase) {
			t.Errorf("POST /v1/kv/reveal description = %q, missing %q", reveal.Description, phrase)
		}
	}
	if response, ok := reveal.Responses.Map()["413"]; !ok {
		t.Error("POST /v1/kv/reveal must document its 8 KiB request-body limit as 413")
	} else if description := response.Value.Description; description == nil || !strings.Contains(*description, "8 KiB") {
		t.Errorf("POST /v1/kv/reveal 413 description = %v, want the 8 KiB request-body limit", description)
	}

	entry, ok := doc.Components.Schemas["KVEntry"]
	if !ok {
		t.Error("KVEntry schema is missing")
	} else {
		valueRequired := false
		for _, property := range entry.Value.Required {
			if property == "value" {
				valueRequired = true
				break
			}
		}
		if !valueRequired {
			t.Error("KVEntry schema must require value, including explicit JSON null")
		}
	}

	meta, ok := doc.Components.Schemas["KVEntryMeta"]
	if !ok {
		t.Error("KVEntryMeta schema is missing")
	} else if _, hasValue := meta.Value.Properties["value"]; hasValue {
		t.Error("KVEntryMeta must structurally omit value")
	}
	if list := doc.Components.Schemas["KVEntryList"]; list == nil || list.Value.Properties["items"].Value.Items.Ref != "#/components/schemas/KVEntryMeta" {
		t.Error("KVEntryList items must use the redacted KVEntryMeta schema")
	}

	for path := range doc.Paths.Map() {
		if strings.HasPrefix(path, "/v1/internal/") {
			t.Errorf("internal path %s must not be documented in OpenAPI", path)
		}
	}
}

func TestOpenAPIEndpointIsPublic(t *testing.T) {
	router := NewRouter(
		nil,
		nil,
		zap.NewNop(),
		make([]byte, 32),
		nil,
		nil,
		"",
		"",
		"",
		"",
		"",
		"",
		nil,
		nil,
	)

	req := httptest.NewRequest(http.MethodGet, "/openapi.json", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /openapi.json status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("GET /openapi.json Content-Type = %q, want application/json", got)
	}
	if body := rec.Body.String(); body != string(apicontract.Document()) {
		t.Fatal("GET /openapi.json response differs from embedded document")
	}

	for _, path := range []string{"/", "/docs"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusTemporaryRedirect {
			t.Errorf("GET %s status = %d, want %d", path, rec.Code, http.StatusTemporaryRedirect)
		}
		if location := rec.Header().Get("Location"); location != "/openapi.json" {
			t.Errorf("GET %s Location = %q, want /openapi.json", path, location)
		}
	}
}
