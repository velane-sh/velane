package middleware

import (
	"net/http"
	"strings"
)

// RejectEmbedCredential blocks sandbox writes even though embed credentials have
// synthetic manage scope. It inspects the original bearer token, not scopes.
func RejectEmbedCredential(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token, ok := bearerToken(r); ok && strings.HasPrefix(token, "et_") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"embed credentials cannot modify sandboxes"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
