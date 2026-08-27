package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/abskrj/velane/services/control-plane/internal/api/middleware"
	"github.com/abskrj/velane/services/control-plane/internal/audit"
	"github.com/abskrj/velane/services/control-plane/internal/callerid"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/abskrj/velane/services/control-plane/internal/nango"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// ConnectionStore is the subset of postgres.Store needed by ConnectionsHandler.
type ConnectionStore interface {
	GetTenantBySlug(ctx context.Context, slug string) (*models.Tenant, error)
	UpsertConnection(ctx context.Context, tenantID, provider, alias, providerConfigKey string, credentialProfileID *string, displayName string) (*models.Connection, error)
	ListConnections(ctx context.Context, tenantID string) ([]*models.Connection, error)
	GetConnection(ctx context.Context, tenantID, provider string) (*models.Connection, error)
	GetConnectionByAlias(ctx context.Context, tenantID, provider, alias string) (*models.Connection, error)
	DeleteConnection(ctx context.Context, tenantID, provider string) error
}

// ConnectionsHandler handles all OAuth connection management endpoints
// and the internal proxy that snippet code calls at runtime.
type ConnectionsHandler struct {
	store           *postgres.Store
	nango           *nango.Client
	log             *zap.Logger
	auditor         *audit.Logger
	callerKey       []byte // verifies signed caller identity tokens at the proxy
	nangoConnectURL string // browser-accessible Connect UI URL, returned with session tokens
	nangoApiURL     string // browser-accessible Nango API URL, returned with session tokens
}

func NewConnectionsHandler(store *postgres.Store, nangoClient *nango.Client, log *zap.Logger, nangoConnectURL, nangoApiURL string) *ConnectionsHandler {
	return &ConnectionsHandler{store: store, nango: nangoClient, log: log, nangoConnectURL: nangoConnectURL, nangoApiURL: nangoApiURL}
}

func (h *ConnectionsHandler) WithAuditor(a *audit.Logger) *ConnectionsHandler {
	h.auditor = a
	return h
}

// WithCallerKey installs the key used to verify the caller identity tokens the
// scheduler mints. Without it the proxy cannot identify callers and keeps the
// pre-RBAC behaviour for unrestricted profiles.
func (h *ConnectionsHandler) WithCallerKey(key []byte) *ConnectionsHandler {
	h.callerKey = append([]byte(nil), key...)
	return h
}

// CreateSession handles POST /v1/tenant/connections/session.
// Returns a short-lived Nango Connect session token the frontend uses to open
// the OAuth popup for a specific provider.
func (h *ConnectionsHandler) CreateSession(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	var req struct {
		Provider            string `json:"provider"`
		Alias               string `json:"alias"`
		CredentialProfileID string `json:"credential_profile_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Provider == "" {
		writeError(w, http.StatusBadRequest, "provider is required")
		return
	}
	if req.Alias == "" {
		req.Alias = "default"
	}

	profile, err := h.store.GetDefaultIntegrationCredentialProfile(r.Context(), tenant.ID, req.Provider)
	if req.CredentialProfileID != "" {
		profile, err = h.store.GetIntegrationCredentialProfileByID(r.Context(), tenant.ID, req.CredentialProfileID)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "integration credentials not configured for provider")
		return
	}
	if profile.Provider != req.Provider {
		writeError(w, http.StatusBadRequest, "credential profile/provider mismatch")
		return
	}
	if provider, err := h.nango.GetProvider(r.Context(), req.Provider); err == nil {
		if !isOAuthConnectMode(provider.AuthMode) {
			writeError(w, http.StatusBadRequest, "provider does not use OAuth connect")
			return
		}
	} else {
		h.log.Debug("nango provider lookup failed before connect session", zap.String("provider", req.Provider), zap.Error(err))
	}
	alias := req.Alias
	if alias == "default" && profile.Alias != "" {
		alias = profile.Alias
	}

	token, err := h.nango.CreateConnectSession(r.Context(), tenant.ID, tenant.Name, profile.NangoProviderConfigKey, alias)
	if err != nil {
		h.log.Error("create nango connect session", zap.String("provider", req.Provider), zap.Error(err))
		writeError(w, http.StatusBadGateway, "failed to create connect session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"session_token":         token,
		"connect_url":           h.nangoConnectURL,
		"api_url":               h.nangoApiURL,
		"credential_profile_id": profile.ID,
		"alias":                 alias,
	})
}

// RecordConnection handles POST /v1/tenant/connections.
// Called by the frontend after the Nango OAuth popup completes successfully.
func (h *ConnectionsHandler) RecordConnection(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	var req struct {
		Provider            string `json:"provider"`
		DisplayName         string `json:"display_name"`
		Alias               string `json:"alias"`
		CredentialProfileID string `json:"credential_profile_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Provider == "" {
		writeError(w, http.StatusBadRequest, "provider is required")
		return
	}
	if req.Alias == "" {
		req.Alias = "default"
	}

	providerConfigKey := req.Provider
	var credentialProfileID *string
	if req.CredentialProfileID != "" {
		profile, err := h.store.GetIntegrationCredentialProfileByID(r.Context(), tenant.ID, req.CredentialProfileID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid credential_profile_id")
			return
		}
		providerConfigKey = profile.NangoProviderConfigKey
		credentialProfileID = &profile.ID
	}
	conn, err := h.store.UpsertConnection(r.Context(), tenant.ID, req.Provider, req.Alias, providerConfigKey, credentialProfileID, req.DisplayName)
	if err != nil {
		h.log.Error("record connection", zap.String("provider", req.Provider), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to record connection")
		return
	}
	if conn.NangoConnectionID == "" && h.nango != nil {
		nangoConnID, err := h.nango.FindConnectionID(r.Context(), tenant.ID, providerConfigKey, req.Alias)
		if err != nil {
			h.log.Warn("record connection: failed to reconcile nango connection id",
				zap.String("provider", req.Provider),
				zap.String("provider_config_key", providerConfigKey),
				zap.String("alias", req.Alias),
				zap.Error(err),
			)
		} else if nangoConnID != "" {
			if updated, err := h.store.UpdateNangoConnectionIDByProviderConfigKey(r.Context(), tenant.ID, providerConfigKey, nangoConnID); err != nil {
				h.log.Warn("record connection: failed to store reconciled nango connection id",
					zap.String("provider", req.Provider),
					zap.String("provider_config_key", providerConfigKey),
					zap.String("alias", req.Alias),
					zap.String("nango_connection_id", nangoConnID),
					zap.Error(err),
				)
			} else {
				conn = updated
				if err := h.nango.PatchConnectionMetadata(r.Context(), nangoConnID, providerConfigKey, map[string]any{
					"velane_alias":     conn.Alias,
					"velane_tenant_id": tenant.ID,
				}); err != nil {
					h.log.Warn("record connection: failed to patch nango connection metadata (non-fatal)",
						zap.String("nango_connection_id", nangoConnID),
						zap.Error(err),
					)
				}
			}
		} else {
			h.log.Warn("record connection: nango connection id not found during reconciliation",
				zap.String("provider", req.Provider),
				zap.String("provider_config_key", providerConfigKey),
				zap.String("alias", req.Alias),
			)
		}
	}

	if h.auditor != nil {
		actorID, actorType := resolveActor(r)
		h.auditor.Log(r.Context(), models.AuditEntry{
			TenantID:   tenant.ID,
			ActorID:    actorID,
			ActorType:  actorType,
			Action:     "connection_connect",
			ResourceID: conn.ID,
			Metadata:   auditMeta(map[string]any{"provider": req.Provider}),
		})
	}

	writeJSON(w, http.StatusCreated, conn)
}

// ListConnections handles GET /v1/tenant/connections.
func (h *ConnectionsHandler) ListConnections(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	conns, err := h.store.ListConnections(r.Context(), tenant.ID)
	if err != nil {
		h.log.Error("list connections", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to list connections")
		return
	}

	access, ok := callerAccessOrError(w, r, h.store, tenant.ID)
	if !ok {
		return
	}
	conns = filterConnectionsByAccess(conns, access)

	if len(conns) == 0 {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	writeJSON(w, http.StatusOK, filterAndPaginateConnections(conns, r))
}

// DisconnectProvider handles DELETE /v1/tenant/connections/{provider}.
func (h *ConnectionsHandler) DisconnectProvider(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	provider := chi.URLParam(r, "provider")

	conn, err := h.store.GetConnection(r.Context(), tenant.ID, provider)
	if err != nil {
		writeError(w, http.StatusNotFound, "connection not found")
		return
	}

	// Delete from Nango first (best-effort — don't block if Nango is down).
	if err := h.nango.DeleteConnection(r.Context(), conn.NangoConnectionID, provider); err != nil {
		h.log.Warn("nango delete connection failed (continuing)", zap.String("provider", provider), zap.Error(err))
	}

	if err := h.store.DeleteConnection(r.Context(), tenant.ID, provider); err != nil {
		h.log.Error("delete connection", zap.String("provider", provider), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to delete connection")
		return
	}

	if h.auditor != nil {
		actorID, actorType := resolveActor(r)
		h.auditor.Log(r.Context(), models.AuditEntry{
			TenantID:   tenant.ID,
			ActorID:    actorID,
			ActorType:  actorType,
			Action:     "connection_disconnect",
			ResourceID: conn.ID,
			Metadata:   auditMeta(map[string]any{"provider": provider}),
		})
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListConnectionsForToken handles GET /v1/connections (no tenant slug in path).
// Used by the MCP server, which authenticates with an API key and does not
// need to know the tenant slug in advance.
func (h *ConnectionsHandler) ListConnectionsForToken(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	conns, err := h.store.ListConnections(r.Context(), tenant.ID)
	if err != nil {
		h.log.Error("list connections for token", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to list connections")
		return
	}

	access, ok := callerAccessOrError(w, r, h.store, tenant.ID)
	if !ok {
		return
	}
	conns = filterConnectionsByAccess(conns, access)

	if len(conns) == 0 {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, filterAndPaginateConnections(conns, r))
}

// filterConnectionsByAccess drops connections whose credential profile is
// restricted to groups the caller does not belong to.
func filterConnectionsByAccess(conns []*models.Connection, access *CallerAccess) []*models.Connection {
	if access == nil || access.SeesAll {
		return conns
	}
	visible := make([]*models.Connection, 0, len(conns))
	for _, c := range conns {
		profileID := ""
		if c.CredentialProfileID != nil {
			profileID = *c.CredentialProfileID
		}
		if access.CanUseProfile(profileID) {
			visible = append(visible, c)
		}
	}
	return visible
}

func filterAndPaginateConnections(conns []*models.Connection, r *http.Request) []*models.Connection {
	searchQuery := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 100)
	offset := parseOffset(r.URL.Query().Get("offset"))

	filtered := conns[:0]
	if searchQuery == "" {
		filtered = append(filtered, conns...)
	} else {
		for _, c := range conns {
			if strings.Contains(strings.ToLower(c.Provider), searchQuery) ||
				strings.Contains(strings.ToLower(c.Alias), searchQuery) ||
				strings.Contains(strings.ToLower(c.DisplayName), searchQuery) ||
				strings.Contains(strings.ToLower(c.ProviderConfigKey), searchQuery) {
				filtered = append(filtered, c)
			}
		}
	}

	if offset > 0 {
		if offset >= len(filtered) {
			filtered = filtered[:0]
		} else {
			filtered = filtered[offset:]
		}
	}
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

// Proxy handles all methods on /v1/proxy/{provider}/*.
// This endpoint is intentionally unauthenticated via the public middleware stack
// and trusts the X-Velane-Tenant header. It is externally reachable in shipped
// topologies, so callers that reach this port can name any tenant. Moving
// trust-header routes to a second, never-published listener is tracked separately.
//
// Group membership, unlike the tenant header, is never taken on trust: it comes
// from the signed caller token the scheduler injects into the sandbox, so a
// snippet cannot widen its own integration access.
func (h *ConnectionsHandler) Proxy(w http.ResponseWriter, r *http.Request) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Velane-Tenant"))
	if tenantID == "" {
		writeError(w, http.StatusBadRequest, "X-Velane-Tenant header required")
		return
	}

	provider := chi.URLParam(r, "provider")
	alias := strings.TrimSpace(r.URL.Query().Get("alias"))
	if alias == "" {
		alias = strings.TrimSpace(r.Header.Get("X-Velane-Integration-Alias"))
	}
	if alias == "" {
		alias = "default"
	}
	// chi wildcard gives us the path after {provider}/ — re-add leading slash.
	path := "/" + chi.URLParam(r, "*")

	conn, err := h.store.GetConnectionByAlias(r.Context(), tenantID, provider, alias)
	if err != nil {
		writeError(w, http.StatusBadRequest, "no connection found for provider: "+provider+" alias: "+alias)
		return
	}
	providerConfigKey := conn.ProviderConfigKey
	if providerConfigKey == "" {
		providerConfigKey = provider
	}
	if conn.NangoConnectionID == "" {
		writeError(w, http.StatusBadRequest, "connection is not fully linked; reconnect provider")
		return
	}
	if !h.callerMayUseConnection(r, tenantID, conn) {
		writeError(w, http.StatusForbidden, "integration not granted to caller's groups")
		return
	}
	h.nango.Proxy(w, r, conn.NangoConnectionID, providerConfigKey, path)
}

// callerMayUseConnection verifies the signed caller token against the group
// grants on the connection's credential profile. A missing or unverifiable
// token keeps the pre-RBAC behaviour unless the tenant enabled strict mode.
func (h *ConnectionsHandler) callerMayUseConnection(r *http.Request, tenantID string, conn *models.Connection) bool {
	profileID := ""
	if conn.CredentialProfileID != nil {
		profileID = *conn.CredentialProfileID
	}

	restricted := false
	if profileID != "" {
		var err error
		restricted, err = h.store.CredentialProfileHasGrants(r.Context(), profileID)
		if err != nil {
			h.log.Error("proxy: check credential profile grants", zap.Error(err))
			return false
		}
	}

	claims := h.verifiedCallerClaims(r, tenantID)
	if claims == nil {
		if !restricted {
			return true
		}
		strict, err := h.store.TenantRBACStrictMode(r.Context(), tenantID)
		if err != nil {
			h.log.Error("proxy: read tenant strict mode", zap.Error(err))
			return false
		}
		return !strict
	}

	if models.RoleSeesAllIntegrations(claims.Role) || !restricted {
		return true
	}

	granted, err := h.store.CredentialProfileGrantedToGroups(r.Context(), profileID, claims.GroupIDs)
	if err != nil {
		h.log.Error("proxy: check group grant", zap.Error(err))
		return false
	}
	return granted
}

// verifiedCallerClaims returns the caller identity only when the token is
// present, correctly signed and issued for this tenant.
func (h *ConnectionsHandler) verifiedCallerClaims(r *http.Request, tenantID string) *callerid.Claims {
	token := strings.TrimSpace(r.Header.Get(callerid.Header))
	if token == "" || len(h.callerKey) == 0 {
		return nil
	}
	claims, err := callerid.Verify(h.callerKey, token)
	if err != nil {
		h.log.Warn("proxy: rejecting unverifiable caller token", zap.Error(err))
		return nil
	}
	if claims.TenantID != tenantID {
		h.log.Warn("proxy: caller token tenant mismatch",
			zap.String("token_tenant", claims.TenantID),
			zap.String("header_tenant", tenantID),
		)
		return nil
	}
	return claims
}
