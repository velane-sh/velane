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
		if strings.HasPrefix(route, "/v1/proxy/") {
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
