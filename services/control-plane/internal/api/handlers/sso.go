package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/api/middleware"
	"github.com/abskrj/velane/services/control-plane/internal/audit"
	"github.com/abskrj/velane/services/control-plane/internal/auth"
	"github.com/abskrj/velane/services/control-plane/internal/license"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/crewjam/saml/samlsp"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

const ssoFlowCookie = "velane_sso_flow"

type SSOStore interface {
	GetTenantByID(context.Context, string) (*models.Tenant, error)
	GetEnabledSSOConnectionBySlug(context.Context, string) (*models.Tenant, *models.SSOConnection, error)
	GetSSOConnection(context.Context, string) (*models.SSOConnection, error)
	UpsertSSOConnection(context.Context, *models.SSOConnection) (*models.SSOConnection, error)
	DeleteSSOConnection(context.Context, string) error
	ListMembers(context.Context, string) ([]*models.TenantMember, error)
	GetUserBySSOIdentity(context.Context, string, string) (*models.User, error)
	GetUserByEmail(context.Context, string) (*models.User, error)
	CreateUserNoPassword(context.Context, string) (*models.User, error)
	CreateSSOIdentity(context.Context, string, string, string, string, string) error
	AddMemberIfMissing(context.Context, string, string, string) (bool, error)
}

type ssoConfig struct {
	IssuerURL      string   `json:"issuer_url,omitempty"`
	ClientID       string   `json:"client_id,omitempty"`
	ClientSecret   string   `json:"client_secret,omitempty"`
	Scopes         []string `json:"scopes,omitempty"`
	IdPMetadataXML string   `json:"idp_metadata_xml,omitempty"`
}
type ssoPutRequest struct {
	Protocol         string    `json:"protocol"`
	DisplayName      string    `json:"display_name"`
	DefaultRole      string    `json:"default_role"`
	BreakGlassUserID *string   `json:"break_glass_user_id"`
	Config           ssoConfig `json:"config"`
}
type ssoFlow struct {
	Slug, State, Nonce, Verifier string
	ExpiresAt                    time.Time
}

type SSOHandler struct {
	store         SSOStore
	jwt           *auth.JWTProvider
	licenses      *license.Manager
	encKey        []byte
	publicBaseURL string
	log           *zap.Logger
	auditor       *audit.Logger
}

func NewSSOHandler(store SSOStore, jwt *auth.JWTProvider, licenses *license.Manager, encKey []byte, publicBaseURL string, log *zap.Logger, auditor *audit.Logger) *SSOHandler {
	return &SSOHandler{store: store, jwt: jwt, licenses: licenses, encKey: encKey, publicBaseURL: strings.TrimRight(publicBaseURL, "/"), log: log, auditor: auditor}
}

func (h *SSOHandler) licensed(ctx context.Context, tenant *models.Tenant) bool {
	if tenant == nil {
		return false
	}
	full, err := h.store.GetTenantByID(ctx, tenant.ID)
	if err != nil {
		return false
	}
	key := ""
	if full.LicenseKey != nil {
		key = *full.LicenseKey
	}
	return h.licenses.IsEnabled(ctx, license.FeatureSSO, key)
}
func (h *SSOHandler) tenant(w http.ResponseWriter, r *http.Request) (*models.Tenant, bool) {
	t := middleware.TenantFromContext(r.Context())
	if t == nil {
		writeError(w, 403, "access denied")
		return nil, false
	}
	if !h.licensed(r.Context(), t) {
		writeError(w, 402, "enterprise SSO license required")
		return nil, false
	}
	return t, true
}
func (h *SSOHandler) callbackURL(slug string) string {
	return h.publicBaseURL + "/api/v1/admin/auth/sso/" + url.PathEscape(slug) + "/oidc/callback"
}
func (h *SSOHandler) metadataURL(slug string) string {
	return h.publicBaseURL + "/api/v1/admin/auth/sso/" + url.PathEscape(slug) + "/saml/metadata"
}

func (h *SSOHandler) Get(w http.ResponseWriter, r *http.Request) {
	t, ok := h.tenant(w, r)
	if !ok {
		return
	}
	c, err := h.store.GetSSOConnection(r.Context(), t.ID)
	if err != nil {
		if postgres.IsNotFound(err) {
			w.WriteHeader(204)
			return
		}
		writeError(w, 500, "failed to load SSO")
		return
	}
	h.writeConnection(w, r, t, c)
}
func (h *SSOHandler) writeConnection(w http.ResponseWriter, r *http.Request, t *models.Tenant, c *models.SSOConnection) {
	cfg, _ := h.decrypt(c)
	if cfg.ClientSecret != "" {
		cfg.ClientSecret = "********"
	}
	if cfg.IdPMetadataXML != "" {
		cfg.IdPMetadataXML = "configured"
	}
	writeJSON(w, 200, map[string]any{"id": c.ID, "protocol": c.Protocol, "display_name": c.DisplayName, "default_role": c.DefaultRole, "enabled": c.Enabled, "enforced": c.Enforced, "tested_at": c.TestedAt, "break_glass_user_id": c.BreakGlassUserID, "config": cfg, "oidc_callback_url": h.callbackURL(t.Slug), "saml_metadata_url": h.metadataURL(t.Slug), "saml_acs_url": h.publicBaseURL + "/api/v1/admin/auth/sso/" + url.PathEscape(t.Slug) + "/saml/acs"})
}

func (h *SSOHandler) Put(w http.ResponseWriter, r *http.Request) {
	t, ok := h.tenant(w, r)
	if !ok {
		return
	}
	var req ssoPutRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req) != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	if req.DefaultRole == "" {
		req.DefaultRole = "invoke"
	}
	if err := validateSSORequest(req); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	existing, _ := h.store.GetSSOConnection(r.Context(), t.ID)
	if existing != nil {
		old, _ := h.decrypt(existing)
		if req.Config.ClientSecret == "" || req.Config.ClientSecret == "********" {
			req.Config.ClientSecret = old.ClientSecret
		}
		if req.Config.IdPMetadataXML == "configured" {
			req.Config.IdPMetadataXML = old.IdPMetadataXML
		}
	}
	raw, _ := json.Marshal(req.Config)
	enc, err := postgres.EncryptValue(h.encKey, string(raw))
	if err != nil {
		writeError(w, 500, "failed to encrypt SSO configuration")
		return
	}
	c := &models.SSOConnection{TenantID: t.ID, Protocol: req.Protocol, DisplayName: req.DisplayName, DefaultRole: req.DefaultRole, BreakGlassUserID: req.BreakGlassUserID, ConfigEncrypted: enc}
	if existing != nil {
		c.ID = existing.ID
	}
	saved, err := h.store.UpsertSSOConnection(r.Context(), c)
	if err != nil {
		h.log.Error("save SSO", zap.Error(err))
		writeError(w, 500, "failed to save SSO")
		return
	}
	h.audit(r, t.ID, "sso_config_update", saved.ID, nil)
	h.writeConnection(w, r, t, saved)
}
func validateSSORequest(r ssoPutRequest) error {
	if r.Protocol != "oidc" && r.Protocol != "saml" {
		return errors.New("protocol must be oidc or saml")
	}
	if strings.TrimSpace(r.DisplayName) == "" {
		return errors.New("display_name is required")
	}
	if r.DefaultRole == "" {
		r.DefaultRole = "invoke"
	}
	if r.DefaultRole != "invoke" && r.DefaultRole != "manage" {
		return errors.New("default_role must be invoke or manage")
	}
	if r.Protocol == "oidc" && (r.Config.IssuerURL == "" || r.Config.ClientID == "") {
		return errors.New("issuer_url and client_id are required")
	}
	if r.Protocol == "saml" && r.Config.IdPMetadataXML == "" {
		return errors.New("idp_metadata_xml is required")
	}
	return nil
}
func (h *SSOHandler) Delete(w http.ResponseWriter, r *http.Request) {
	t, ok := h.tenant(w, r)
	if !ok {
		return
	}
	c, _ := h.store.GetSSOConnection(r.Context(), t.ID)
	if err := h.store.DeleteSSOConnection(r.Context(), t.ID); err != nil {
		writeError(w, 500, "failed to delete SSO")
		return
	}
	id := ""
	if c != nil {
		id = c.ID
	}
	h.audit(r, t.ID, "sso_config_delete", id, nil)
	w.WriteHeader(204)
}
func (h *SSOHandler) decrypt(c *models.SSOConnection) (ssoConfig, error) {
	plain, err := postgres.DecryptValue(h.encKey, c.ConfigEncrypted)
	if err != nil {
		return ssoConfig{}, err
	}
	var cfg ssoConfig
	err = json.Unmarshal([]byte(plain), &cfg)
	return cfg, err
}

func (h *SSOHandler) Test(w http.ResponseWriter, r *http.Request) {
	t, ok := h.tenant(w, r)
	if !ok {
		return
	}
	c, err := h.store.GetSSOConnection(r.Context(), t.ID)
	if err != nil {
		writeError(w, 404, "SSO configuration not found")
		return
	}
	cfg, err := h.decrypt(c)
	if err == nil && c.Protocol == "oidc" {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		_, err = oidc.NewProvider(ctx, cfg.IssuerURL)
	} else if err == nil {
		_, err = samlsp.ParseMetadata([]byte(cfg.IdPMetadataXML))
	}
	if err != nil {
		writeError(w, 400, "connection test failed: "+err.Error())
		return
	}
	now := time.Now()
	c.TestedAt = &now
	c.Enforced = false
	c.Enabled = false
	saved, err := h.store.UpsertSSOConnection(r.Context(), c)
	if err != nil {
		writeError(w, 500, "failed to save test status")
		return
	}
	h.audit(r, t.ID, "sso_test", c.ID, nil)
	h.writeConnection(w, r, t, saved)
}
func (h *SSOHandler) Activate(w http.ResponseWriter, r *http.Request) {
	t, ok := h.tenant(w, r)
	if !ok {
		return
	}
	c, err := h.store.GetSSOConnection(r.Context(), t.ID)
	if err != nil || c.TestedAt == nil {
		writeError(w, 409, "connection must be tested before activation")
		return
	}
	c.Enabled = true
	saved, err := h.store.UpsertSSOConnection(r.Context(), c)
	if err != nil {
		writeError(w, 500, "failed to activate SSO")
		return
	}
	h.audit(r, t.ID, "sso_activate", c.ID, nil)
	h.writeConnection(w, r, t, saved)
}
func (h *SSOHandler) Enforce(w http.ResponseWriter, r *http.Request) {
	t, ok := h.tenant(w, r)
	if !ok {
		return
	}
	var req struct {
		Enforced         bool   `json:"enforced"`
		BreakGlassUserID string `json:"break_glass_user_id"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	c, err := h.store.GetSSOConnection(r.Context(), t.ID)
	if err != nil || !c.Enabled || c.TestedAt == nil {
		writeError(w, 409, "active tested connection required")
		return
	}
	if req.Enforced {
		valid := false
		members, _ := h.store.ListMembers(r.Context(), t.ID)
		for _, m := range members {
			if m.UserID == req.BreakGlassUserID && m.Role == "admin" {
				u, e := h.store.GetUserByEmail(r.Context(), m.Email)
				valid = e == nil && u.PasswordHash != ""
			}
		}
		if !valid {
			writeError(w, 400, "break-glass user must be a password-bearing tenant admin")
			return
		}
		c.BreakGlassUserID = &req.BreakGlassUserID
	} else {
		c.BreakGlassUserID = nil
	}
	c.Enforced = req.Enforced
	saved, err := h.store.UpsertSSOConnection(r.Context(), c)
	if err != nil {
		writeError(w, 500, "failed to update enforcement")
		return
	}
	h.audit(r, t.ID, "sso_enforcement_update", c.ID, map[string]any{"enforced": req.Enforced})
	h.writeConnection(w, r, t, saved)
}

func (h *SSOHandler) Discover(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(r.URL.Query().Get("org"))
	t, c, err := h.store.GetEnabledSSOConnectionBySlug(r.Context(), slug)
	if err != nil || !h.licensed(r.Context(), t) {
		writeError(w, 404, "SSO is not available for this organization")
		return
	}
	writeJSON(w, 200, map[string]any{"available": true, "protocol": c.Protocol, "display_name": c.DisplayName, "start_url": "/api/v1/admin/auth/sso/" + url.PathEscape(slug) + "/start"})
}
func (h *SSOHandler) Start(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if slug == "" {
		slug = pathParam(r, "slug")
	}
	t, c, err := h.store.GetEnabledSSOConnectionBySlug(r.Context(), slug)
	if err != nil || !h.licensed(r.Context(), t) {
		writeError(w, 404, "SSO unavailable")
		return
	}
	if c.Protocol != "oidc" {
		writeError(w, 501, "SAML login is not configured on this server")
		return
	}
	cfg, err := h.decrypt(c)
	if err != nil {
		writeError(w, 500, "invalid SSO configuration")
		return
	}
	provider, err := oidc.NewProvider(r.Context(), cfg.IssuerURL)
	if err != nil {
		writeError(w, 502, "identity provider unavailable")
		return
	}
	state := randomURLToken(32)
	nonce := randomURLToken(32)
	verifier := oauth2.GenerateVerifier()
	flow := ssoFlow{Slug: slug, State: state, Nonce: nonce, Verifier: verifier, ExpiresAt: time.Now().Add(10 * time.Minute)}
	raw, _ := json.Marshal(flow)
	payload := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, h.encKey)
	_, _ = mac.Write([]byte(payload))
	signed := payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	http.SetCookie(w, &http.Cookie{Name: ssoFlowCookie, Value: signed, Path: "/", HttpOnly: true, Secure: isHTTPSRequest(r), SameSite: http.SameSiteLaxMode, MaxAge: 600})
	scopes := append([]string{"openid", "email", "profile"}, cfg.Scopes...)
	oc := oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, Endpoint: provider.Endpoint(), RedirectURL: h.callbackURL(slug), Scopes: uniqueStrings(scopes)}
	http.Redirect(w, r, oc.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), 302)
}
func (h *SSOHandler) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	if h.jwt == nil {
		h.redirectError(w, r, "sso_unavailable")
		return
	}
	slug := pathParam(r, "slug")
	flow, ok := h.readSSOFlow(r)
	h.clearFlow(w, r)
	if !ok || flow.Slug != slug || flow.State != r.URL.Query().Get("state") || time.Now().After(flow.ExpiresAt) {
		h.redirectError(w, r, "invalid_state")
		return
	}
	t, c, err := h.store.GetEnabledSSOConnectionBySlug(r.Context(), slug)
	if err != nil || !h.licensed(r.Context(), t) {
		h.redirectError(w, r, "sso_unavailable")
		return
	}
	cfg, _ := h.decrypt(c)
	provider, err := oidc.NewProvider(r.Context(), cfg.IssuerURL)
	if err != nil {
		h.redirectError(w, r, "provider_error")
		return
	}
	oc := oauth2.Config{ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, Endpoint: provider.Endpoint(), RedirectURL: h.callbackURL(slug)}
	token, err := oc.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		h.redirectError(w, r, "exchange_failed")
		return
	}
	rawID, _ := token.Extra("id_token").(string)
	idToken, err := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}).Verify(r.Context(), rawID)
	if err != nil {
		h.redirectError(w, r, "invalid_token")
		return
	}
	var claims struct {
		Subject       string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Nonce         string `json:"nonce"`
	}
	if idToken.Claims(&claims) != nil || claims.Nonce != flow.Nonce || !claims.EmailVerified {
		h.redirectError(w, r, "unverified_identity")
		return
	}
	user, created, err := h.jit(r.Context(), t, c, claims.Subject, claims.Email)
	if err != nil {
		h.redirectError(w, r, "account_error")
		return
	}
	sess, err := h.jwt.IssueSSOSession(r.Context(), user, t.ID, c.ID)
	if err != nil {
		h.redirectError(w, r, "session_error")
		return
	}
	if created {
		h.auditSystem(r.Context(), t.ID, "sso_jit_member", user.ID, map[string]any{"role": c.DefaultRole})
	}
	writeAuthCookies(w, r, sess)
	http.SetCookie(w, &http.Cookie{Name: middleware.ActiveOrgCookieName, Value: t.Slug, Path: "/", HttpOnly: true, Secure: isHTTPSRequest(r), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, h.publicBaseURL+"/dashboard/overview", 302)
}
func (h *SSOHandler) jit(ctx context.Context, t *models.Tenant, c *models.SSOConnection, subject, email string) (*models.User, bool, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || subject == "" {
		return nil, false, errors.New("missing identity")
	}
	if u, e := h.store.GetUserBySSOIdentity(ctx, c.ID, subject); e == nil {
		return u, false, nil
	}
	u, err := h.store.GetUserByEmail(ctx, email)
	if err != nil {
		u, err = h.store.CreateUserNoPassword(ctx, email)
	}
	if err != nil {
		return nil, false, err
	}
	if err = h.store.CreateSSOIdentity(ctx, t.ID, c.ID, u.ID, subject, email); err != nil {
		return nil, false, err
	}
	added, err := h.store.AddMemberIfMissing(ctx, t.ID, u.ID, c.DefaultRole)
	return u, added, err
}
func randomURLToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
func (h *SSOHandler) readSSOFlow(r *http.Request) (ssoFlow, bool) {
	c, e := r.Cookie(ssoFlowCookie)
	if e != nil {
		return ssoFlow{}, false
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 2 {
		return ssoFlow{}, false
	}
	sig, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return ssoFlow{}, false
	}
	mac := hmac.New(sha256.New, h.encKey)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return ssoFlow{}, false
	}
	b, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil {
		return ssoFlow{}, false
	}
	var f ssoFlow
	e = json.Unmarshal(b, &f)
	return f, e == nil
}
func (h *SSOHandler) clearFlow(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: ssoFlowCookie, Path: "/", MaxAge: -1, HttpOnly: true, Secure: isHTTPSRequest(r)})
}
func (h *SSOHandler) redirectError(w http.ResponseWriter, r *http.Request, reason string) {
	http.Redirect(w, r, h.publicBaseURL+"/login?auth_error="+url.QueryEscape(reason), 302)
}
func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
func pathParam(r *http.Request, name string) string {
	v := r.PathValue(name)
	if v != "" {
		return v
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	for i, p := range parts {
		if p == "sso" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}
func (h *SSOHandler) audit(r *http.Request, tenant, action, resource string, meta map[string]any) {
	if h.auditor == nil {
		return
	}
	actorID, actorType := resolveActor(r)
	h.auditor.Log(r.Context(), models.AuditEntry{TenantID: tenant, ActorID: actorID, ActorType: actorType, Action: action, ResourceID: resource, Metadata: auditMeta(meta)})
}
func (h *SSOHandler) auditSystem(ctx context.Context, tenant, action, resource string, meta map[string]any) {
	if h.auditor != nil {
		h.auditor.Log(ctx, models.AuditEntry{TenantID: tenant, ActorType: "sso", Action: action, ResourceID: resource, Metadata: auditMeta(meta)})
	}
}
