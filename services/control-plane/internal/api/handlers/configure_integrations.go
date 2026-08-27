package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/abskrj/velane/services/control-plane/internal/api/middleware"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/abskrj/velane/services/control-plane/internal/nango"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type ConfigureIntegrationsHandler struct {
	nango  *nango.Client
	log    *zap.Logger
	store  *postgres.Store
	encKey []byte
}

type configuredProfileResponse struct {
	models.IntegrationCredentialProfileView
	Connected bool `json:"connected"`
}

func NewConfigureIntegrationsHandler(store *postgres.Store, nangoClient *nango.Client, log *zap.Logger, encKey []byte) *ConfigureIntegrationsHandler {
	return &ConfigureIntegrationsHandler{store: store, nango: nangoClient, log: log, encKey: encKey}
}

// ListConfigured handles GET /v1/integrations/configured.
// Returns all integration credential profiles for the authenticated tenant.
func (h *ConfigureIntegrationsHandler) ListConfigured(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	configs, err := h.store.ListIntegrationCredentialProfiles(r.Context(), tenant.ID, "")
	if err != nil {
		h.log.Error("list integration credential profiles", zap.Error(err))
		writeError(w, http.StatusBadGateway, "failed to list configured integrations")
		return
	}
	if configs == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	searchQuery := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	statusFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("status")))
	limit := parsePositiveInt(r.URL.Query().Get("limit"), 100)
	offset := parseOffset(r.URL.Query().Get("offset"))

	connections, err := h.store.ListConnections(r.Context(), tenant.ID)
	if err != nil {
		h.log.Error("list connections for configured integrations", zap.Error(err))
		writeError(w, http.StatusBadGateway, "failed to list configured integrations")
		return
	}
	connectedProfiles := make(map[string]struct{}, len(connections))
	for _, conn := range connections {
		connectedProfiles[strings.ToLower(conn.Provider)+"::"+strings.ToLower(conn.Alias)] = struct{}{}
	}

	access, ok := callerAccessOrError(w, r, h.store, tenant.ID)
	if !ok {
		return
	}

	filtered := make([]configuredProfileResponse, 0, len(configs))
	for _, c := range configs {
		if c == nil {
			continue
		}
		if !access.CanUseProfile(c.ID) {
			continue
		}
		key := strings.ToLower(c.Provider) + "::" + strings.ToLower(c.Alias)
		_, isConnected := connectedProfiles[key]

		if statusFilter == "connected" && !isConnected {
			continue
		}
		if statusFilter == "configured" && isConnected {
			continue
		}
		if statusFilter == "ready" {
			if isConnected || !isOAuthConnectMode(c.CredentialsType) {
				continue
			}
		}
		if statusFilter != "" && statusFilter != "connected" && statusFilter != "configured" && statusFilter != "ready" && statusFilter != "all" {
			continue
		}
		if searchQuery != "" &&
			!strings.Contains(strings.ToLower(c.Provider), searchQuery) &&
			!strings.Contains(strings.ToLower(c.Alias), searchQuery) &&
			!strings.Contains(strings.ToLower(c.Name), searchQuery) &&
			!strings.Contains(strings.ToLower(c.NangoProviderConfigKey), searchQuery) {
			continue
		}

		filtered = append(filtered, configuredProfileResponse{
			IntegrationCredentialProfileView: *c,
			Connected:                        isConnected,
		})
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

	writeJSON(w, http.StatusOK, filtered)
}

func parsePositiveInt(raw string, max int) int {
	if strings.TrimSpace(raw) == "" {
		return 0
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v <= 0 {
		return 0
	}
	if max > 0 && v > max {
		return max
	}
	return v
}

func parseOffset(raw string) int {
	if strings.TrimSpace(raw) == "" {
		return 0
	}
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v < 0 {
		return 0
	}
	return v
}

// Configure handles POST /v1/integrations/configured.
func (h *ConfigureIntegrationsHandler) Configure(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	var req struct {
		Provider          string            `json:"provider"`
		Alias             string            `json:"alias"`
		Name              string            `json:"name"`
		CredentialsType   string            `json:"credentials_type"`
		Credentials       map[string]string `json:"credentials"`
		OAuthClientID     string            `json:"oauth_client_id"`
		OAuthClientSecret string            `json:"oauth_client_secret"`
		OAuthScopes       string            `json:"oauth_scopes"`
		IsDefault         bool              `json:"is_default"`
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
	if req.Name == "" {
		req.Name = req.Alias
	}

	credType := normalizeCredentialType(strings.ToUpper(strings.TrimSpace(req.CredentialsType)))
	var providerMeta *models.NangoProvider
	if np, err := h.nango.GetProvider(r.Context(), req.Provider); err == nil {
		providerMeta = np
		if providerMode := normalizeCredentialType(strings.ToUpper(strings.TrimSpace(np.AuthMode))); providerMode != "" {
			if credType != "" && credType != providerMode {
				h.log.Warn("credentials_type overridden by provider auth_mode",
					zap.String("provider", req.Provider),
					zap.String("requested", credType),
					zap.String("resolved", providerMode),
				)
			}
			credType = providerMode
		}
	} else {
		h.log.Debug("nango provider lookup failed; using request credentials_type",
			zap.String("provider", req.Provider),
			zap.Error(err),
		)
	}
	if credType == "" {
		credType = "OAUTH2"
	}

	plainFields := map[string]string{}
	for k, v := range req.Credentials {
		trimmedKey := strings.TrimSpace(k)
		trimmedValue := strings.TrimSpace(v)
		if trimmedKey == "" || trimmedValue == "" {
			continue
		}
		// `type` is controlled by the backend to avoid invalid/unsupported unions.
		if strings.EqualFold(trimmedKey, "type") {
			continue
		}
		normalizedKey := normalizeCredentialFieldKey(trimmedKey)
		if normalizedKey == "installation_id" {
			// Nango's GitHub App credential unions reject installation_id on setup.
			continue
		}
		plainFields[normalizedKey] = trimmedValue
	}
	if req.OAuthClientID != "" {
		plainFields["oauth_client_id"] = req.OAuthClientID
	}
	if req.OAuthClientSecret != "" {
		plainFields["oauth_client_secret"] = req.OAuthClientSecret
	}
	if req.OAuthScopes != "" {
		plainFields["oauth_scopes"] = req.OAuthScopes
	}

	existing, _ := h.store.GetIntegrationCredentialProfileByAlias(r.Context(), tenant.ID, req.Provider, req.Alias)
	providerConfigKey := ""
	if existing != nil {
		providerConfigKey = existing.NangoProviderConfigKey
		view, existingFields, decErr := h.store.DecryptIntegrationCredentialProfile(existing, h.encKey)
		if decErr != nil {
			h.log.Error("decrypt existing integration profile", zap.Error(decErr))
			writeError(w, http.StatusInternalServerError, "failed to load integration credentials")
			return
		}
		for key, value := range existingFields {
			if strings.TrimSpace(plainFields[key]) == "" {
				plainFields[key] = value
			}
		}
		if strings.TrimSpace(plainFields["oauth_scopes"]) == "" && strings.TrimSpace(plainFields["scopes"]) == "" && strings.TrimSpace(view.OAuthScopes) != "" {
			plainFields["oauth_scopes"] = view.OAuthScopes
		}
	}
	resolvedOAuthScopes := firstNonEmpty(plainFields["oauth_scopes"], plainFields["scopes"])
	connectionConfig := splitConnectionConfig(providerMeta, plainFields)

	nangoCreds := map[string]any{}
	if shouldSendCredentialsType(credType) {
		nangoCreds["type"] = credType
	}
	switch credType {
	case "OAUTH2":
		clientID := firstNonEmpty(plainFields["oauth_client_id"], plainFields["client_id"])
		clientSecret := firstNonEmpty(plainFields["oauth_client_secret"], plainFields["client_secret"])
		if clientID == "" || clientSecret == "" {
			writeError(w, http.StatusBadRequest, "client_id and client_secret are required")
			return
		}
		nangoCreds["client_id"] = clientID
		nangoCreds["client_secret"] = clientSecret
		if scopes := firstNonEmpty(plainFields["oauth_scopes"], plainFields["scopes"]); scopes != "" {
			nangoCreds["scopes"] = formatScopesForNango(scopes)
		}
	case "OAUTH2_CC":
		clientID := firstNonEmpty(plainFields["oauth_client_id"], plainFields["client_id"])
		clientSecret := firstNonEmpty(plainFields["oauth_client_secret"], plainFields["client_secret"])
		if clientID == "" || clientSecret == "" {
			writeError(w, http.StatusBadRequest, "client_id and client_secret are required")
			return
		}
		// Nango expects OAUTH2_CC providers to be created without credentials in
		// /integrations; tenant credentials are still securely stored in Velane.
		nangoCreds = map[string]any{}
	case "OAUTH1":
		clientID := firstNonEmpty(
			plainFields["oauth_client_id"],
			plainFields["client_id"],
			plainFields["consumer_key"],
		)
		clientSecret := firstNonEmpty(
			plainFields["oauth_client_secret"],
			plainFields["client_secret"],
			plainFields["consumer_secret"],
		)
		if clientID == "" || clientSecret == "" {
			writeError(w, http.StatusBadRequest, "client_id and client_secret are required")
			return
		}
		nangoCreds["client_id"] = clientID
		nangoCreds["client_secret"] = clientSecret
	case "MCP_OAUTH2", "MCP_OAUTH2_GENERIC":
		// Nango MCP providers are configured without credentials on /integrations.
		// End-user authorization handles provider credentials during connect.
		nangoCreds = map[string]any{}
	case "API_KEY":
		// API keys belong to the Nango connection, not the provider config. The
		// connection is imported after the profile is stored below.
		if apiKeyValue(plainFields) == "" {
			writeError(w, http.StatusBadRequest, "apiKey is required")
			return
		}
		nangoCreds = map[string]any{}
	default:
		if shouldPassThroughCredentials(credType) {
			for k, v := range plainFields {
				nangoCreds[k] = v
			}
		} else {
			// For most non-OAuth integration modes, Nango validates credentials on the
			// end-user connection step (nango.auth), not on /integrations setup.
			nangoCreds = map[string]any{}
		}
	}

	configKey := chooseProviderConfigKey(providerConfigKey, tenant.ID, req.Provider, req.Alias)
	var nangoErr error
	if existing != nil {
		nangoErr = h.nango.UpdateIntegrationConfig(r.Context(), configKey, nangoCreds)
	} else {
		nangoErr = h.nango.CreateIntegrationConfig(r.Context(), configKey, req.Provider, nangoCreds)
	}
	if nangoErr != nil {
		h.log.Error("configure integration", zap.String("provider", req.Provider), zap.Error(nangoErr))
		writeError(w, http.StatusBadGateway, "failed to configure integration: "+nangoErr.Error())
		return
	}

	profile, err := h.store.UpsertIntegrationCredentialProfile(
		r.Context(),
		tenant.ID,
		req.Provider,
		req.Alias,
		req.Name,
		resolveActorID(r),
		credType,
		plainFields,
		resolvedOAuthScopes,
		req.IsDefault,
		chooseProviderConfigKey(providerConfigKey, tenant.ID, req.Provider, req.Alias),
		h.encKey,
	)
	if err != nil {
		h.log.Error("upsert integration credential profile", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to store integration credentials")
		return
	}

	if credType == "API_KEY" {
		nangoConnID, err := h.nango.ImportConnection(
			r.Context(),
			configKey,
			configKey,
			map[string]any{
				"type":   "API_KEY",
				"apiKey": apiKeyValue(plainFields),
			},
			connectionConfig,
			map[string]any{
				"velane_alias":     req.Alias,
				"velane_tenant_id": tenant.ID,
			},
			map[string]string{
				"end_user_id":     tenant.ID,
				"organization_id": tenant.ID,
				"velane_alias":    req.Alias,
			},
		)
		if err != nil {
			h.log.Error("import api key connection", zap.String("provider", req.Provider), zap.String("provider_config_key", configKey), zap.Error(err))
			writeError(w, http.StatusBadGateway, "failed to connect API key integration: "+err.Error())
			return
		}

		credentialProfileID := &profile.ID
		conn, err := h.store.UpsertConnection(r.Context(), tenant.ID, req.Provider, req.Alias, configKey, credentialProfileID, req.Name)
		if err != nil {
			h.log.Error("record api key connection", zap.String("provider", req.Provider), zap.Error(err))
			writeError(w, http.StatusInternalServerError, "failed to record API key connection")
			return
		}
		if conn.NangoConnectionID == "" || conn.NangoConnectionID != nangoConnID {
			if _, err := h.store.UpdateNangoConnectionIDByProviderConfigKey(r.Context(), tenant.ID, configKey, nangoConnID); err != nil {
				h.log.Error("store api key nango connection id", zap.String("provider", req.Provider), zap.String("nango_connection_id", nangoConnID), zap.Error(err))
				writeError(w, http.StatusInternalServerError, "failed to store API key connection")
				return
			}
		}
	}

	view, _, err := h.store.DecryptIntegrationCredentialProfile(profile, h.encKey)
	if err != nil {
		h.log.Error("decrypt integration profile view", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to return integration profile")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// DeleteConfigured handles DELETE /v1/integrations/configured/{id}.
func (h *ConfigureIntegrationsHandler) DeleteConfigured(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	id := chi.URLParam(r, "providerConfigKey")
	profile, err := h.store.SoftDeleteIntegrationCredentialProfile(r.Context(), tenant.ID, id)
	if err != nil {
		h.log.Error("delete integration credential profile", zap.String("id", id), zap.Error(err))
		writeError(w, http.StatusNotFound, "integration credential profile not found")
		return
	}

	if err := h.nango.DeleteIntegrationConfig(r.Context(), profile.NangoProviderConfigKey); err != nil {
		h.log.Error("delete integration config", zap.String("key", profile.NangoProviderConfigKey), zap.Error(err))
		writeError(w, http.StatusBadGateway, "failed to delete integration config")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func chooseProviderConfigKey(existing, tenantID, provider, alias string) string {
	if strings.TrimSpace(existing) != "" {
		return existing
	}
	return fmt.Sprintf("velane_%s_%s_%s", sanitizeConfigPart(tenantID), sanitizeConfigPart(provider), sanitizeConfigPart(alias))
}

func sanitizeConfigPart(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.ReplaceAll(v, " ", "_")
	v = strings.ReplaceAll(v, "-", "_")
	if v == "" {
		return "default"
	}
	return v
}

func resolveActorID(r *http.Request) string {
	if user := middleware.SessionUserFromContext(r.Context()); user != nil {
		return user.ID
	}
	if key := middleware.APIKeyFromContext(r.Context()); key != nil {
		return key.ID
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// formatScopesForNango normalizes user input into the comma-separated scope
// string Nango expects on /integrations (not a JSON array).
func formatScopesForNango(raw string) string {
	return strings.Join(parseScopeList(raw), ",")
}

func parseScopeList(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		scope := strings.TrimSpace(part)
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	return out
}

func normalizeCredentialType(mode string) string {
	return mode
}

func isOAuthConnectMode(mode string) bool {
	switch normalizeCredentialType(strings.ToUpper(strings.TrimSpace(mode))) {
	case "OAUTH2", "OAUTH2_CC", "OAUTH1", "APP", "MCP_OAUTH2", "MCP_OAUTH2_GENERIC":
		return true
	default:
		return false
	}
}

func shouldSendCredentialsType(mode string) bool {
	switch mode {
	case "OAUTH2", "OAUTH1", "APP", "CUSTOM":
		return true
	default:
		return false
	}
}

func shouldPassThroughCredentials(mode string) bool {
	switch mode {
	case "OAUTH1", "APP", "CUSTOM":
		return true
	default:
		return false
	}
}

func normalizeCredentialFieldKey(key string) string {
	switch key {
	case "clientId":
		return "client_id"
	case "clientSecret":
		return "client_secret"
	case "appId":
		return "app_id"
	case "appPublicLink":
		return "app_link"
	case "privateKey":
		return "private_key"
	default:
		return key
	}
}

func splitConnectionConfig(provider *models.NangoProvider, plainFields map[string]string) map[string]any {
	out := map[string]any{}
	if provider == nil {
		return out
	}
	for key := range provider.ConnectionConfig {
		if value := strings.TrimSpace(plainFields[key]); value != "" {
			out[key] = value
		}
	}
	return out
}

func apiKeyValue(plainFields map[string]string) string {
	return firstNonEmpty(plainFields["apiKey"], plainFields["api_key"])
}
