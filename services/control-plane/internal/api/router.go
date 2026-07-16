package api

import (
	"crypto/rsa"
	"net/http"

	"github.com/abskrj/velane/services/control-plane/internal/api/handlers"
	"github.com/abskrj/velane/services/control-plane/internal/api/middleware"
	"github.com/abskrj/velane/services/control-plane/internal/api/openapi"
	"github.com/abskrj/velane/services/control-plane/internal/audit"
	"github.com/abskrj/velane/services/control-plane/internal/auth"
	"github.com/abskrj/velane/services/control-plane/internal/auth/oauth"
	"github.com/abskrj/velane/services/control-plane/internal/hub"
	"github.com/abskrj/velane/services/control-plane/internal/license"
	"github.com/abskrj/velane/services/control-plane/internal/nango"
	"github.com/abskrj/velane/services/control-plane/internal/platformlibs"
	"github.com/abskrj/velane/services/control-plane/internal/scheduler"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"go.uber.org/zap"
)

// NewRouter builds and returns the fully configured chi router.
func NewRouter(store *postgres.Store, sched *scheduler.Scheduler, log *zap.Logger, encKey []byte, authProvider auth.Provider, nangoClient *nango.Client, nangoInternalURL, nangoConnectURL, nangoApiURL, nangoWebhookSecret, nangoSecretKey, mcpPublicURL string, platLibs []platformlibs.PlatformLib, licMgr *license.Manager) *chi.Mux {
	return newRouter(store, sched, log, encKey, authProvider, nil, nangoClient, nangoInternalURL, nangoConnectURL, nangoApiURL, nangoWebhookSecret, nangoSecretKey, mcpPublicURL, platLibs, handlers.OAuthConfig{}, licMgr)
}

// NewRouterWithJWT builds the router and wires the RSA public key for the JWKS endpoint.
func NewRouterWithJWT(store *postgres.Store, sched *scheduler.Scheduler, log *zap.Logger, encKey []byte, authProvider auth.Provider, pubKey *rsa.PublicKey, nangoClient *nango.Client, nangoInternalURL, nangoConnectURL, nangoApiURL, nangoWebhookSecret, nangoSecretKey, mcpPublicURL string, platLibs []platformlibs.PlatformLib, oauthCfg handlers.OAuthConfig, licMgr *license.Manager) *chi.Mux {
	return newRouter(store, sched, log, encKey, authProvider, pubKey, nangoClient, nangoInternalURL, nangoConnectURL, nangoApiURL, nangoWebhookSecret, nangoSecretKey, mcpPublicURL, platLibs, oauthCfg, licMgr)
}

func newRouter(store *postgres.Store, sched *scheduler.Scheduler, log *zap.Logger, encKey []byte, authProvider auth.Provider, pubKey *rsa.PublicKey, nangoClient *nango.Client, nangoInternalURL, nangoConnectURL, nangoApiURL, nangoWebhookSecret, nangoSecretKey, mcpPublicURL string, platLibs []platformlibs.PlatformLib, oauthCfg handlers.OAuthConfig, licMgr *license.Manager) *chi.Mux {
	r := chi.NewRouter()

	// Global middleware.
	r.Use(chimiddleware.Recoverer)
	r.Use(chimiddleware.RealIP)
	r.Use(middleware.Logger(log))

	// Audit logger (shared across handlers).
	auditor := audit.New(store, log)

	// Instantiate handlers.
	tenantsH := handlers.NewTenantsHandler(store, log).WithAuditor(auditor)
	snippetsH := handlers.NewSnippetsHandler(store, log)
	snippetHub := hub.New()
	versionsH := handlers.NewVersionsHandler(store, log).WithAuditor(auditor).WithHub(snippetHub)
	invocationsH := handlers.NewInvocationsHandler(store, sched, log).WithAuthProvider(authProvider)
	secretsH := handlers.NewSecretsHandler(store, log, encKey).WithAuditor(auditor)
	gitIntH := handlers.NewGitIntegrationHandler(store, log)
	webhookH := handlers.NewWebhookHandler(store, sched, log)
	nangoWebhookH := handlers.NewNangoWebhookHandler(store, nangoClient, nangoWebhookSecret, nangoSecretKey, log)
	logsH := handlers.NewLogsHandler(store, log)
	metricsH := handlers.NewMetricsHandler(store, log)
	replayH := handlers.NewReplayHandler(store, sched, log)
	embedH := handlers.NewEmbedHandler(store, log)
	connectionsH := handlers.NewConnectionsHandler(store, nangoClient, log, nangoConnectURL, nangoApiURL).WithAuditor(auditor)
	integrationsH := handlers.NewIntegrationsHandler(nangoClient, log, nangoInternalURL, nangoApiURL, mcpPublicURL)
	configureIntH := handlers.NewConfigureIntegrationsHandler(store, nangoClient, log, encKey)
	adminAuthH := handlers.NewAdminAuthHandler(authProvider, store, log)
	if pubKey != nil {
		adminAuthH = adminAuthH.WithPublicKey(pubKey)
	}
	brandingH := handlers.NewBrandingHandler(store, log).WithAuditor(auditor)
	membersH := handlers.NewMembersHandler(store, log).WithAuditor(auditor)
	usageH := handlers.NewUsageHandler(store, log)
	apikeysH := handlers.NewAPIKeysHandler(store, log).WithAuditor(auditor)
	auditH := handlers.NewAuditHandler(store, log)
	instanceH := handlers.NewInstanceHandler(licMgr, log)
	billingH := handlers.NewBillingHandler(store, licMgr, log)

	// Health check — no auth.
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Friendly public entry points for the machine-readable API contract.
	r.Get("/", redirectToOpenAPI)
	r.Get("/docs", redirectToOpenAPI)

	// Machine-readable API contract — public, no auth.
	r.Get("/openapi.json", openapi.ServeHTTP)

	// Instance info — public, no auth. Used by the frontend to determine deployment mode and licensed features.
	r.Get("/v1/instance/info", instanceH.GetInfo)

	// Bootstrap endpoint — intentionally unauthenticated.
	r.Post("/v1/tenants", tenantsH.CreateTenant)

	// JWKS — used by third parties to verify Velane JWTs (no auth).
	r.Get("/.well-known/jwks.json", adminAuthH.JWKS)

	// Admin auth — no API key required, session-based.
	r.Post("/v1/admin/auth/register", adminAuthH.Register)
	r.Post("/v1/admin/auth/login", adminAuthH.Login)
	r.Post("/v1/admin/auth/logout", adminAuthH.Logout)
	r.Post("/v1/admin/auth/refresh", adminAuthH.RefreshToken)

	// Social login (Google / GitHub) — public, session-based. Only wired when at
	// least one provider is configured and JWT auth is in use.
	if jwtProvider, ok := authProvider.(*auth.JWTProvider); ok && oauthCfg.Enabled() {
		oauthMgr := oauth.NewManager(oauthCfg.PublicBaseURL)
		if oauthCfg.GoogleOAuthClientID != "" && oauthCfg.GoogleOAuthClientSecret != "" {
			oauthMgr.AddGoogle(oauthCfg.GoogleOAuthClientID, oauthCfg.GoogleOAuthClientSecret)
		}
		if oauthCfg.GitHubOAuthClientID != "" && oauthCfg.GitHubOAuthClientSecret != "" {
			oauthMgr.AddGitHub(oauthCfg.GitHubOAuthClientID, oauthCfg.GitHubOAuthClientSecret)
		}
		oauthH := handlers.NewOAuthHandler(oauthMgr, jwtProvider, store, log, oauthCfg.PublicBaseURL)
		r.Get("/v1/admin/auth/oauth/providers", oauthH.ListProviders)
		r.Get("/v1/admin/auth/oauth/{provider}/start", oauthH.Start)
		r.Get("/v1/admin/auth/oauth/{provider}/callback", oauthH.Callback)
		log.Info("social login enabled", zap.Strings("providers", oauthMgr.Names()))
	}

	r.Group(func(r chi.Router) {
		r.Use(middleware.SessionAuth(authProvider, store, log))
		r.Get("/v1/admin/auth/me", adminAuthH.Me)
		r.Get("/v1/admin/auth/orgs", adminAuthH.ListMyTenants)
		r.Post("/v1/admin/auth/orgs", adminAuthH.CreateMyTenant)
		r.Get("/v1/admin/auth/orgs/active", adminAuthH.GetActiveTenant)
		r.Post("/v1/admin/auth/orgs/active", adminAuthH.SetActiveTenant)
	})

	// Provider catalog and connect info — public, no auth.
	r.Get("/v1/integrations", integrationsH.ListProviders)
	r.Get("/v1/connect/info", integrationsH.ConnectInfo)
	r.Get("/v1/mcp/info", integrationsH.MCPInfo)

	// Nango asset proxy — serves Nango's logo images through the control plane.
	// Nango is never exposed directly to the browser; all static assets go through here.
	r.Get("/v1/nango-assets/*", integrationsH.ProxyAsset)

	// Internal proxy — no public auth middleware; network isolation is the boundary.
	// Executor containers call /v1/proxy/{provider}/{path} with X-Velane-Tenant header.
	// This path must NOT be added to the authenticated group below.
	r.HandleFunc("/v1/proxy/{provider}/*", connectionsH.Proxy)

	// Authenticated routes.
	r.Group(func(r chi.Router) {
		r.Use(middleware.SessionAuth(authProvider, store, log))
		authMw := middleware.Auth(store, log)
		r.Use(authMw)

		// API key management.
		r.With(middleware.RequireScope("admin", log)).
			Post("/v1/tenant/api-keys", tenantsH.CreateAPIKey)
		r.With(middleware.RequireScope("admin", log)).
			Get("/v1/tenant/api-keys", apikeysH.ListAPIKeys)
		r.With(middleware.RequireScope("admin", log)).
			Delete("/v1/tenant/api-keys/{keyID}", apikeysH.DeleteAPIKey)

		// Egress policy.
		r.Get("/v1/tenant/egress", tenantsH.GetEgressPolicy)
		r.With(middleware.RequireScope("admin", log)).
			Put("/v1/tenant/egress", tenantsH.UpdateEgressPolicy)
		r.Get("/v1/tenant/runtime-limits", tenantsH.GetRuntimeLimits)

		// Branding.
		r.With(middleware.RequireScope("invoke", log)).
			Get("/v1/tenant/branding", brandingH.GetBranding)
		r.With(middleware.RequireScope("admin", log)).
			Put("/v1/tenant/branding", brandingH.UpdateBranding)

		// Team members and invites.
		r.With(middleware.RequireScope("admin", log)).
			Get("/v1/tenant/members", membersH.ListMembers)
		r.With(middleware.RequireScope("admin", log)).
			Post("/v1/tenant/members/invite", membersH.InviteMember)
		r.With(middleware.RequireScope("admin", log)).
			Delete("/v1/tenant/members/{userID}", membersH.RemoveMember)
		r.With(middleware.RequireScope("admin", log)).
			Get("/v1/tenant/members/invites", membersH.ListInvites)

		// Plan / billing (cloud mode only — returns free plan when VELANE_CLOUD is not set).
		r.Get("/v1/tenant/plan", billingH.GetPlan)

		// Usage aggregation.
		r.With(middleware.RequireScope("admin", log)).
			Get("/v1/tenant/usage", usageH.GetUsage)

		// Audit log.
		r.With(middleware.RequireScope("admin", log)).
			Get("/v1/tenant/audit-log", auditH.ListAuditLog)

		// Connections (OAuth integrations).
		r.With(middleware.RequireScope("invoke", log)).
			Get("/v1/tenant/connections", connectionsH.ListConnections)
		r.With(middleware.RequireScope("manage", log)).
			Post("/v1/tenant/connections/session", connectionsH.CreateSession)
		r.With(middleware.RequireScope("manage", log)).
			Post("/v1/tenant/connections", connectionsH.RecordConnection)
		r.With(middleware.RequireScope("manage", log)).
			Delete("/v1/tenant/connections/{provider}", connectionsH.DisconnectProvider)

		// Provider docs.
		r.With(middleware.RequireScope("invoke", log)).
			Get("/v1/integrations/{provider}/docs", integrationsH.GetProviderDocs)

		// Integration credential profile management (tenant-scoped).
		r.With(middleware.RequireScope("invoke", log)).
			Get("/v1/integrations/configured", configureIntH.ListConfigured)
		r.With(middleware.RequireScope("manage", log)).
			Post("/v1/integrations/configured", configureIntH.Configure)
		r.With(middleware.RequireScope("manage", log)).
			Delete("/v1/integrations/configured/{providerConfigKey}", configureIntH.DeleteConfigured)

		// Connection shortcuts — slug-free paths used by the MCP server
		// (auth middleware resolves tenant from the Bearer token).
		r.With(middleware.RequireScope("invoke", log)).
			Get("/v1/connections", connectionsH.ListConnectionsForToken)

		// Snippets.
		r.Get("/v1/snippets", snippetsH.ListSnippets)
		r.With(middleware.RequireScope("manage", log)).
			Post("/v1/snippets", snippetsH.CreateSnippet)
		r.Get("/v1/snippets/{snippetID}", snippetsH.GetSnippet)
		r.With(middleware.RequireScope("manage", log)).
			Delete("/v1/snippets/{snippetID}", snippetsH.DeleteSnippet)

		// Versions.
		r.Get("/v1/snippets/{snippetID}/environments", versionsH.ListEnvironments)
		r.Get("/v1/snippets/{snippetID}/versions", versionsH.ListVersions)
		r.With(middleware.RequireScope("manage", log)).
			Post("/v1/snippets/{snippetID}/versions", versionsH.CreateVersion)
		r.Get("/v1/snippets/{snippetID}/versions/{num}", versionsH.GetVersion)
		r.With(middleware.RequireScope("manage", log)).
			Post("/v1/snippets/{snippetID}/versions/{num}/publish", versionsH.PublishVersion)
		r.With(middleware.RequireScope("invoke", log)).
			Get("/v1/snippets/{snippetID}/watch", versionsH.WatchVersions)

		// Canary traffic splitting.
		r.With(middleware.RequireScope("manage", log)).
			Post("/v1/snippets/{snippetID}/canary", versionsH.SetCanary)
		r.With(middleware.RequireScope("manage", log)).
			Delete("/v1/snippets/{snippetID}/canary", versionsH.ClearCanary)

		// Secrets / Variables.
		r.Get("/v1/secrets", secretsH.ListSecrets)
		r.With(middleware.RequireScope("manage", log)).
			Post("/v1/secrets", secretsH.CreateSecret)
		r.With(middleware.RequireScope("manage", log)).
			Patch("/v1/secrets/{secretID}", secretsH.UpdateSecret)
		r.With(middleware.RequireScope("manage", log)).
			Delete("/v1/secrets/{secretID}", secretsH.DeleteSecret)

		// Git integrations.
		r.With(middleware.RequireScope("manage", log)).
			Post("/v1/snippets/{snippetID}/git-integration", gitIntH.Create)
		r.With(middleware.RequireScope("manage", log)).
			Get("/v1/snippets/{snippetID}/git-integration", gitIntH.Get)
		r.With(middleware.RequireScope("manage", log)).
			Delete("/v1/snippets/{snippetID}/git-integration", gitIntH.Delete)

		// Invocation detail lookup.
		r.Get("/v1/invocations/{id}", invocationsH.GetInvocation)

		// Observability endpoints.
		r.Get("/v1/logs/snippets/{snippetID}", logsH.GetSnippetLogs)
		r.Get("/v1/metrics/snippets/{snippetID}", metricsH.GetSnippetMetrics)
		r.With(middleware.RequireScope("manage", log)).
			Post("/v1/invocations/{id}/replay", replayH.ReplayInvocation)

		// Embed token management.
		r.With(middleware.RequireScope("manage", log)).
			Get("/v1/embed/tokens", embedH.ListTokens)
		r.With(middleware.RequireScope("manage", log)).
			Post("/v1/embed/tokens", embedH.CreateToken)
		r.With(middleware.RequireScope("manage", log)).
			Delete("/v1/embed/tokens/{tokenID}", embedH.RevokeToken)
	})

	// Invoke endpoints perform their own auth inline.
	// Tenant is resolved from the authenticated API key/session.
	r.Post("/v1/invoke/{snippetSlug}", invocationsH.InvokeByToken)

	// Git webhook endpoint — HMAC signature verified inline.
	r.Post("/v1/webhooks/git/{snippetID}", webhookH.GitWebhook)

	// Nango webhook — receives auth events and stores the real Nango connection UUID.
	r.Post("/v1/webhooks/nango", nangoWebhookH.HandleNangoEvent)

	// Embed read routes authenticated by opaque embed token.
	r.Group(func(r chi.Router) {
		r.Use(middleware.AuthEmbed(store, log))
		r.Get("/v1/embed/bootstrap", embedH.Bootstrap)
		r.Get("/v1/embed/snippets", embedH.ListSnippets)
		r.Get("/v1/embed/snippets/{snippetID}", embedH.GetSnippet)
		r.Get("/v1/embed/snippets/{snippetID}/metrics", embedH.GetSnippetMetrics)
		r.Get("/v1/embed/snippets/{snippetID}/logs", embedH.GetSnippetLogs)
	})

	return r
}

func redirectToOpenAPI(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/openapi.json", http.StatusTemporaryRedirect)
}
