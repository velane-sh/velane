package handlers

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/api/middleware"
	"github.com/abskrj/velane/services/control-plane/internal/auth"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"go.uber.org/zap"
)

// AdminAuthStore is the subset of *postgres.Store that admin auth handlers need.
type AdminAuthStore interface {
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	GetInviteByTokenHash(ctx context.Context, hash string) (*models.InviteToken, error)
	AcceptInvite(ctx context.Context, id string) error
	AddMember(ctx context.Context, tenantID, userID, role string) (*models.TenantMember, error)
	CreateTenant(ctx context.Context, name, slug string) (*models.Tenant, error)
	ListUserTenantMemberships(ctx context.Context, userID string) ([]*models.UserTenantMembership, error)
}

// AdminAuthHandler handles email/password auth for the admin portal.
type AdminAuthHandler struct {
	provider auth.Provider
	store    AdminAuthStore
	log      *zap.Logger
	pubKey   *rsa.PublicKey // optional; used for JWKS endpoint
}

// NewAdminAuthHandler constructs an AdminAuthHandler.
func NewAdminAuthHandler(provider auth.Provider, store AdminAuthStore, log *zap.Logger) *AdminAuthHandler {
	return &AdminAuthHandler{provider: provider, store: store, log: log}
}

// WithPublicKey sets the RSA public key used to serve the JWKS endpoint.
// Call this when using JWTProvider.
func (h *AdminAuthHandler) WithPublicKey(pub *rsa.PublicKey) *AdminAuthHandler {
	h.pubKey = pub
	return h
}

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	InviteToken string `json:"invite_token,omitempty"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Register handles POST /v1/admin/auth/register.
func (h *AdminAuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	user, err := h.provider.CreateUser(r.Context(), req.Email, req.Password)
	if err != nil {
		h.log.Error("create user failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	// If an invite token was supplied, validate it and accept.
	if req.InviteToken != "" {
		inviteHash := hashInviteToken(req.InviteToken)
		invite, err := h.store.GetInviteByTokenHash(r.Context(), inviteHash)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid or expired invite token")
			return
		}
		if invite.AcceptedAt != nil {
			writeError(w, http.StatusBadRequest, "invite token already used")
			return
		}
		if invite.ExpiresAt.Before(time.Now()) {
			writeError(w, http.StatusBadRequest, "invite token expired")
			return
		}
		if err := h.store.AcceptInvite(r.Context(), invite.ID); err != nil {
			h.log.Error("accept invite failed", zap.Error(err))
			writeError(w, http.StatusInternalServerError, "failed to accept invite")
			return
		}
		if _, err := h.store.AddMember(r.Context(), invite.TenantID, user.ID, invite.Role); err != nil {
			h.log.Error("add member failed", zap.Error(err))
			writeError(w, http.StatusInternalServerError, "failed to add member")
			return
		}
	}

	// Authenticate immediately to return a session.
	sess, err := h.provider.Authenticate(r.Context(), req.Email, req.Password)
	if err != nil {
		h.log.Error("post-register authenticate failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	writeAuthCookies(w, r, sess)

	writeJSON(w, http.StatusCreated, map[string]any{
		"user":          user,
		"session_token": sess.Token,
		"expires_at":    sess.ExpiresAt,
	})
}

// Login handles POST /v1/admin/auth/login.
func (h *AdminAuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	sess, err := h.provider.Authenticate(r.Context(), req.Email, req.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	writeAuthCookies(w, r, sess)

	writeJSON(w, http.StatusOK, map[string]any{
		"session_token": sess.Token,
		"expires_at":    sess.ExpiresAt,
	})
}

// Logout handles POST /v1/admin/auth/logout.
func (h *AdminAuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if raw, ok := sessionTokenFromRequest(r); ok {
		if err := h.provider.InvalidateSession(r.Context(), raw); err != nil {
			h.log.Debug("invalidate session failed", zap.Error(err))
		}
	}
	if refreshRaw, ok := refreshTokenFromRequest(r); ok {
		if jwtProvider, ok := h.provider.(*auth.JWTProvider); ok {
			if err := jwtProvider.RevokeRefreshToken(r.Context(), refreshRaw); err != nil {
				h.log.Debug("revoke refresh token failed", zap.Error(err))
			}
		}
	}
	clearSessionCookie(w, r)
	clearRefreshCookie(w, r)
	clearActiveOrgCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// Me handles GET /v1/admin/auth/me.
func (h *AdminAuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	user := middleware.SessionUserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	writeJSON(w, http.StatusOK, user)
}

// ListMyTenants handles GET /v1/admin/auth/orgs.
func (h *AdminAuthHandler) ListMyTenants(w http.ResponseWriter, r *http.Request) {
	user := middleware.SessionUserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	memberships, err := h.store.ListUserTenantMemberships(r.Context(), user.ID)
	if err != nil {
		h.log.Error("list user tenants failed", zap.String("user_id", user.ID), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to list orgs")
		return
	}

	writeJSON(w, http.StatusOK, memberships)
}

// CreateMyTenant handles POST /v1/admin/auth/orgs.
func (h *AdminAuthHandler) CreateMyTenant(w http.ResponseWriter, r *http.Request) {
	user := middleware.SessionUserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	var req createTenantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" || req.Slug == "" {
		writeError(w, http.StatusBadRequest, "name and slug are required")
		return
	}
	if !slugRe.MatchString(req.Slug) {
		writeError(w, http.StatusBadRequest, "slug must be 3-63 lowercase alphanumeric characters or hyphens, and cannot start or end with a hyphen")
		return
	}

	tenant, err := h.store.CreateTenant(r.Context(), req.Name, req.Slug)
	if err != nil {
		h.log.Error("create tenant for session user failed", zap.String("user_id", user.ID), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to create org")
		return
	}
	if _, err := h.store.AddMember(r.Context(), tenant.ID, user.ID, "admin"); err != nil {
		h.log.Error("attach creator to tenant failed", zap.String("tenant_id", tenant.ID), zap.String("user_id", user.ID), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to attach org membership")
		return
	}

	writeJSON(w, http.StatusCreated, &models.UserTenantMembership{
		TenantID: tenant.ID,
		Slug:     tenant.Slug,
		Name:     tenant.Name,
		Role:     "admin",
	})
}

// GetActiveTenant handles GET /v1/admin/auth/orgs/active.
// Returns the currently active org membership for this session user.
func (h *AdminAuthHandler) GetActiveTenant(w http.ResponseWriter, r *http.Request) {
	user := middleware.SessionUserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	memberships, err := h.store.ListUserTenantMemberships(r.Context(), user.ID)
	if err != nil {
		h.log.Error("list user tenants failed", zap.String("user_id", user.ID), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to list orgs")
		return
	}
	if len(memberships) == 0 {
		writeError(w, http.StatusNotFound, "no org memberships")
		return
	}

	selected := memberships[0]
	if cookie, err := r.Cookie(middleware.ActiveOrgCookieName); err == nil {
		slug := strings.TrimSpace(cookie.Value)
		for _, membership := range memberships {
			if membership.Slug == slug {
				selected = membership
				break
			}
		}
	}
	writeActiveOrgCookie(w, r, selected.Slug)
	writeJSON(w, http.StatusOK, selected)
}

// SetActiveTenant handles POST /v1/admin/auth/orgs/active.
// Body: { "slug": "myorg" } and sets the active org cookie for session requests.
func (h *AdminAuthHandler) SetActiveTenant(w http.ResponseWriter, r *http.Request) {
	user := middleware.SessionUserFromContext(r.Context())
	if user == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	var req struct {
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Slug) == "" {
		writeError(w, http.StatusBadRequest, "slug is required")
		return
	}

	memberships, err := h.store.ListUserTenantMemberships(r.Context(), user.ID)
	if err != nil {
		h.log.Error("list user tenants failed", zap.String("user_id", user.ID), zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to list orgs")
		return
	}

	for _, membership := range memberships {
		if membership.Slug == req.Slug {
			writeActiveOrgCookie(w, r, membership.Slug)
			writeJSON(w, http.StatusOK, membership)
			return
		}
	}

	writeError(w, http.StatusNotFound, "org not found")
}

// JWKS handles GET /.well-known/jwks.json.
// Returns the RS256 public key in JWK Set format so third parties can verify Velane JWTs.
func (h *AdminAuthHandler) JWKS(w http.ResponseWriter, r *http.Request) {
	if h.pubKey == nil {
		writeError(w, http.StatusNotFound, "JWKS not available — JWT auth not configured")
		return
	}

	// Encode the RSA public key modulus (N) and exponent (E) in base64url without padding.
	nBytes := h.pubKey.N.Bytes()
	nB64 := base64.RawURLEncoding.EncodeToString(nBytes)

	eBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(eBuf, uint32(h.pubKey.E))
	// Trim leading zero bytes.
	i := 0
	for i < len(eBuf)-1 && eBuf[i] == 0 {
		i++
	}
	eB64 := base64.RawURLEncoding.EncodeToString(eBuf[i:])

	jwks := map[string]any{
		"keys": []map[string]any{
			{
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"kid": "velane-1",
				"n":   nB64,
				"e":   eB64,
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(jwks)
}

type refreshTokenRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// RefreshToken handles POST /v1/admin/auth/refresh.
// Body: { "refresh_token": "..." }
// Returns a new AuthTokenPair. The old refresh token is revoked (rotation).
func (h *AdminAuthHandler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	var req refreshTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	refreshToken := strings.TrimSpace(req.RefreshToken)
	if refreshToken == "" {
		if raw, ok := refreshTokenFromRequest(r); ok {
			refreshToken = raw
		}
	}
	if refreshToken == "" {
		writeError(w, http.StatusBadRequest, "refresh_token is required")
		return
	}

	jwtProvider, ok := h.provider.(*auth.JWTProvider)
	if !ok {
		writeError(w, http.StatusNotImplemented, "token refresh not supported by current auth provider")
		return
	}

	pair, err := jwtProvider.Refresh(r.Context(), refreshToken)
	if err != nil {
		h.log.Debug("refresh token failed", zap.Error(err))
		writeError(w, http.StatusUnauthorized, "invalid or expired refresh token")
		return
	}

	writeAuthCookies(w, r, &models.Session{
		Token:          pair.AccessToken,
		RefreshToken:   pair.RefreshToken,
		ExpiresAt:      pair.ExpiresAt,
		RefreshExpires: time.Now().Add(7 * 24 * time.Hour),
	})
	writeJSON(w, http.StatusOK, pair)
}

// hashInviteToken hashes a raw invite token using SHA-256 (same algorithm as auth package).
func hashInviteToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func writeAuthCookies(w http.ResponseWriter, r *http.Request, sess *models.Session) {
	writeSessionCookie(w, r, sess)
	if strings.TrimSpace(sess.RefreshToken) != "" {
		writeRefreshCookie(w, r, sess.RefreshToken, sess.RefreshExpires)
	}
}

func writeSessionCookie(w http.ResponseWriter, r *http.Request, sess *models.Session) {
	maxAge := int(time.Until(sess.ExpiresAt).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	http.SetCookie(w, &http.Cookie{
		Name:     middleware.SessionCookieName,
		Value:    sess.Token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPSRequest(r),
		Expires:  sess.ExpiresAt,
		MaxAge:   maxAge,
	})
}

func writeRefreshCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt time.Time) {
	maxAge := int(time.Until(expiresAt).Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	http.SetCookie(w, &http.Cookie{
		Name:     middleware.RefreshCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPSRequest(r),
		Expires:  expiresAt,
		MaxAge:   maxAge,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     middleware.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPSRequest(r),
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func clearRefreshCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     middleware.RefreshCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPSRequest(r),
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func clearActiveOrgCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     middleware.ActiveOrgCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPSRequest(r),
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func writeActiveOrgCookie(w http.ResponseWriter, r *http.Request, slug string) {
	http.SetCookie(w, &http.Cookie{
		Name:     middleware.ActiveOrgCookieName,
		Value:    strings.TrimSpace(slug),
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPSRequest(r),
	})
}

func refreshTokenFromRequest(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(middleware.RefreshCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return "", false
	}
	return strings.TrimSpace(cookie.Value), true
}

func sessionTokenFromRequest(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	if strings.HasPrefix(header, "Bearer ") {
		raw := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		if raw != "" {
			return raw, true
		}
	}

	cookie, err := r.Cookie(middleware.SessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return "", false
	}
	return strings.TrimSpace(cookie.Value), true
}

func isHTTPSRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// Ensure big.Int is imported via the RSA key operations (used indirectly via N.Bytes()).
var _ = new(big.Int)
