package middleware

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/abskrj/velane/services/control-plane/internal/audit"
	"github.com/abskrj/velane/services/control-plane/internal/license"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
)

type SSOEnforcementStore interface {
	GetTenantByID(context.Context, string) (*models.Tenant, error)
	GetSSOConnection(context.Context, string) (*models.SSOConnection, error)
}

// EnforceSSO confines SSO sessions to their asserted tenant and, for licensed
// enforced tenants, permits only the active connection or password break glass.
func EnforceSSO(store SSOEnforcementStore, licenses *license.Manager, auditor *audit.Logger, log *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user := SessionUserFromContext(r.Context())
			tenant := TenantFromContext(r.Context())
			if user == nil || tenant == nil {
				next.ServeHTTP(w, r)
				return
			}
			a := SessionAssuranceFromContext(r.Context())
			if a.SSOTenantID != "" && a.SSOTenantID != tenant.ID {
				http.Error(w, `{"error":"SSO session is confined to another tenant"}`, http.StatusForbidden)
				return
			}
			conn, err := store.GetSSOConnection(r.Context(), tenant.ID)
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					next.ServeHTTP(w, r)
					return
				}
				log.Error("load SSO enforcement", zap.Error(err))
				http.Error(w, `{"error":"failed to verify SSO policy"}`, 500)
				return
			}
			if !connEnforced(conn) {
				next.ServeHTTP(w, r)
				return
			}
			full, err := store.GetTenantByID(r.Context(), tenant.ID)
			if err != nil {
				http.Error(w, `{"error":"failed to verify SSO policy"}`, 500)
				return
			}
			key := ""
			if full.LicenseKey != nil {
				key = *full.LicenseKey
			}
			if !licenses.IsEnabled(r.Context(), license.FeatureSSO, key) {
				next.ServeHTTP(w, r)
				return
			}
			if a.AuthMethod == "sso" && a.SSOConnectionID == conn.ID {
				next.ServeHTTP(w, r)
				return
			}
			if a.AuthMethod == "local" && conn.BreakGlassUserID != nil && *conn.BreakGlassUserID == user.ID {
				if auditor != nil {
					meta, _ := json.Marshal(map[string]any{"connection_id": conn.ID})
					auditor.Log(r.Context(), models.AuditEntry{TenantID: tenant.ID, ActorID: user.ID, ActorType: "user", Action: "sso_break_glass", ResourceID: conn.ID, Metadata: meta})
				}
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, `{"error":"organization requires enterprise SSO"}`, http.StatusForbidden)
		})
	}
}
func connEnforced(c *models.SSOConnection) bool { return c != nil && c.Enabled && c.Enforced }
