package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/abskrj/velane/services/control-plane/internal/models"
	"go.uber.org/zap"
)

// AuthStore is the subset of *postgres.Store that the auth middleware needs.
type AuthStore interface {
	ValidateAPIKey(ctx context.Context, plain string) (*models.APIKey, error)
	ValidateEmbedToken(ctx context.Context, plain string) (*models.EmbedToken, error)
	GetTenantByID(ctx context.Context, id string) (*models.Tenant, error)
}

// contextKey is a package-private type to avoid key collisions in context values.
type contextKey string

const (
	tenantKey contextKey = "tenant"
	apikeyKey contextKey = "apikey"
)

// TenantFromContext retrieves the authenticated tenant from the request context.
func TenantFromContext(ctx context.Context) *models.Tenant {
	v, _ := ctx.Value(tenantKey).(*models.Tenant)
	return v
}

// APIKeyFromContext retrieves the authenticated API key from the request context.
func APIKeyFromContext(ctx context.Context) *models.APIKey {
	v, _ := ctx.Value(apikeyKey).(*models.APIKey)
	return v
}

// ExportedTenantKey returns the context key used to store the authenticated tenant.
// Intended for use in tests that need to inject a tenant into the context.
func ExportedTenantKey() any { return tenantKey }

// ExportedAPIKeyKey returns the context key used to store the authenticated API key.
// Intended for use in tests that need to inject an API key into the context.
func ExportedAPIKeyKey() any { return apikeyKey }

// Auth returns a middleware that validates the Bearer token in the Authorization header and
// attaches the resolved Tenant and APIKey to the request context. If SessionAuth has already
// authenticated the request via JWT (session user in context), this middleware passes through
// so that RequireScope can use the session role instead.
func Auth(store AuthStore, log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// If SessionAuth already authenticated this request via JWT, skip API key validation.
			if SessionUserFromContext(r.Context()) != nil {
				next.ServeHTTP(w, r)
				return
			}

			plain, ok := bearerToken(r)
			if !ok {
				writeUnauthorized(w, "missing or malformed Authorization header")
				return
			}

			// Embed tokens (et_) are accepted as admin-scoped credentials so that
			// the embedded admin UI can perform full CRUD operations.
			if strings.HasPrefix(plain, "et_") {
				embedTok, err := store.ValidateEmbedToken(r.Context(), plain)
				if err != nil {
					log.Debug("embed token validation failed", zap.Error(err))
					writeUnauthorized(w, "invalid embed token")
					return
				}
				tenant, err := store.GetTenantByID(r.Context(), embedTok.TenantID)
				if err != nil {
					log.Error("tenant lookup failed for embed token", zap.String("tenant_id", embedTok.TenantID), zap.Error(err))
					writeUnauthorized(w, "invalid embed token")
					return
				}
				syntheticKey := &models.APIKey{
					TenantID: embedTok.TenantID,
					Scopes:   []string{"invoke", "manage"},
				}
				ctx := context.WithValue(r.Context(), tenantKey, tenant)
				ctx = context.WithValue(ctx, apikeyKey, syntheticKey)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			key, err := store.ValidateAPIKey(r.Context(), plain)
			if err != nil {
				log.Debug("api key validation failed", zap.Error(err))
				writeUnauthorized(w, "invalid api key")
				return
			}

			tenant, err := store.GetTenantByID(r.Context(), key.TenantID)
			if err != nil {
				log.Error("tenant lookup failed", zap.String("tenant_id", key.TenantID), zap.Error(err))
				writeUnauthorized(w, "invalid api key")
				return
			}

			ctx := context.WithValue(r.Context(), tenantKey, tenant)
			ctx = context.WithValue(ctx, apikeyKey, key)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireScope returns a middleware that enforces that the caller has the specified scope.
// Accepts both API key auth (checked via key.HasScope) and session JWT auth (checked via
// the tenant membership role stored by SessionAuth). Must be applied after Auth/SessionAuth.
func RequireScope(scope string, log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// API key path.
			if key := APIKeyFromContext(r.Context()); key != nil {
				if !key.HasScope(scope) {
					http.Error(w, `{"error":"forbidden: missing scope `+scope+`"}`, http.StatusForbidden)
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			// Session (JWT) path — derive scopes from tenant membership role.
			if role := SessionRoleFromContext(r.Context()); role != "" {
				if roleHasScope(role, scope) {
					next.ServeHTTP(w, r)
					return
				}
				http.Error(w, `{"error":"forbidden: missing scope `+scope+`"}`, http.StatusForbidden)
				return
			}
			writeUnauthorized(w, "missing or malformed Authorization header")
		})
	}
}

// RequireRole returns a middleware that restricts a route to the given tenant
// member roles. API keys are matched against the equivalent scope-derived role
// so that admin-scoped keys keep working on admin-only routes.
func RequireRole(log *zap.Logger, roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(roles))
	for _, role := range roles {
		allowed[role] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			role := CallerRole(r.Context())
			if role == "" {
				writeUnauthorized(w, "missing or malformed Authorization header")
				return
			}
			if !allowed[role] {
				http.Error(w, `{"error":"forbidden: insufficient role"}`, http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CallerRole reports the effective tenant role of the request, whether it was
// authenticated with a session JWT or an API key. API keys have scopes rather
// than roles, so their broadest scope is mapped back onto a role.
func CallerRole(ctx context.Context) string {
	if role := SessionRoleFromContext(ctx); role != "" {
		return role
	}
	if key := APIKeyFromContext(ctx); key != nil {
		switch {
		case key.HasScope(models.RoleAdmin):
			return models.RoleAdmin
		case key.HasScope(models.RoleManage):
			return models.RoleManage
		case key.HasScope(models.RoleInvoke):
			return models.RoleInvoke
		}
	}
	return ""
}

// roleHasScope maps a tenant member role to the scopes it grants.
// owner, admin          → invoke + manage + admin
// integration_manager   → invoke + manage
// manage                → invoke + manage
// member, invoke        → invoke only
// viewer                → read-only, no scope
func roleHasScope(role, scope string) bool {
	return models.RoleGrantsScope(role, scope)
}

// bearerToken extracts the token from "Authorization: Bearer <token>".
func bearerToken(r *http.Request) (string, bool) {
	hdr := r.Header.Get("Authorization")
	if hdr == "" {
		return "", false
	}
	parts := strings.SplitN(hdr, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	if token == "" {
		return "", false
	}
	return token, true
}

func writeUnauthorized(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"` + msg + `"}`))
}
