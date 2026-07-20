// Package openapi exposes the control-plane OpenAPI contract.
package openapi

import (
	_ "embed"
	"net/http"
)

//go:embed openapi.json
var document []byte

// Document returns a copy of the embedded OpenAPI document.
func Document() []byte {
	return append([]byte(nil), document...)
}

// ServeHTTP serves the public OpenAPI document.
func ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(document)
}
