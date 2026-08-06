package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/auth"
	"github.com/abskrj/velane/services/control-plane/internal/auth/oauth"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

const oauthStateCookieName = "velane_oauth_state"

// OAuthConfig carries the social-login settings needed to build the OAuth handler.
type OAuthConfig struct {
	PublicBaseURL           string
	GoogleOAuthClientID     string
	GoogleOAuthClientSecret string
	GitHubOAuthClientID     string
	GitHubOAuthClientSecret string
}

// Enabled reports whether at least one provider is configured.
func (c OAuthConfig) Enabled() bool {
	return (c.GoogleOAuthClientID != "" && c.GoogleOAuthClientSecret != "") ||
		(c.GitHubOAuthClientID != "" && c.GitHubOAuthClientSecret != "")
}

// OAuthStore is the subset of *postgres.Store the OAuth handler needs.
type OAuthStore interface {
	GetUserByOAuthIdentity(ctx context.Context, provider, subject string) (*models.User, error)
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	CreateUserNoPassword(ctx context.Context, email string) (*models.User, error)
	CreateOAuthIdentity(ctx context.Context, userID, provider, subject, email string) error
}

// OAuthHandler handles Google/GitHub social login for the admin portal.
type OAuthHandler struct {
	manager       *oauth.Manager
	jwt           *auth.JWTProvider
	store         OAuthStore
	log           *zap.Logger
	publicBaseURL string
}

// NewOAuthHandler constructs an OAuthHandler. The manager must be pre-populated
// with the enabled providers.
func NewOAuthHandler(manager *oauth.Manager, jwt *auth.JWTProvider, store OAuthStore, log *zap.Logger, publicBaseURL string) *OAuthHandler {
	return &OAuthHandler{
		manager:       manager,
		jwt:           jwt,
		store:         store,
		log:           log,
		publicBaseURL: strings.TrimRight(publicBaseURL, "/"),
	}
}

// ListProviders handles GET /v1/admin/auth/oauth/providers.
// Returns the slugs of the configured providers so the UI can render buttons.
func (h *OAuthHandler) ListProviders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"providers": h.manager.Names()})
}

// Start handles GET /v1/admin/auth/oauth/{provider}/start.
func (h *OAuthHandler) Start(w http.ResponseWriter, r *http.Request) {
	providerName := chi.URLParam(r, "provider")
	provider := h.manager.Get(providerName)
	if provider == nil {
		writeError(w, http.StatusNotFound, "unknown oauth provider")
		return
	}

	state, err := randomState()
	if err != nil {
		h.log.Error("generate oauth state failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to start oauth")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookieName,
		Value:    providerName + ":" + state,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPSRequest(r),
		MaxAge:   600,
		Expires:  time.Now().Add(10 * time.Minute),
	})

	http.Redirect(w, r, provider.AuthCodeURL(state), http.StatusFound)
}

// Callback handles GET /v1/admin/auth/oauth/{provider}/callback.
func (h *OAuthHandler) Callback(w http.ResponseWriter, r *http.Request) {
	providerName := chi.URLParam(r, "provider")
	provider := h.manager.Get(providerName)
	if provider == nil {
		writeError(w, http.StatusNotFound, "unknown oauth provider")
		return
	}

	h.clearStateCookie(w, r)

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		h.log.Info("oauth provider returned error", zap.String("provider", providerName), zap.String("error", errParam))
		h.redirectError(w, r, "access_denied")
		return
	}

	if !h.validState(r, providerName) {
		h.redirectError(w, r, "invalid_state")
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		h.redirectError(w, r, "missing_code")
		return
	}

	info, err := provider.Exchange(r.Context(), code)
	if err != nil {
		h.log.Error("oauth exchange failed", zap.String("provider", providerName), zap.Error(err))
		h.redirectError(w, r, "exchange_failed")
		return
	}

	user, err := h.findOrCreateUser(r.Context(), providerName, info)
	if err != nil {
		h.log.Error("oauth find-or-create user failed", zap.String("provider", providerName), zap.Error(err))
		h.redirectError(w, r, "account_error")
		return
	}

	sess, err := h.jwt.IssueOAuthSession(r.Context(), user)
	if err != nil {
		h.log.Error("oauth issue session failed", zap.String("provider", providerName), zap.Error(err))
		h.redirectError(w, r, "session_error")
		return
	}

	writeAuthCookies(w, r, sess)
	http.Redirect(w, r, h.publicBaseURL+"/dashboard/overview", http.StatusFound)
}

var errNoEmail = errors.New("oauth provider did not return an email address")

func (h *OAuthHandler) findOrCreateUser(ctx context.Context, providerName string, info *oauth.UserInfo) (*models.User, error) {
	// 1. Existing identity → that user.
	if user, err := h.store.GetUserByOAuthIdentity(ctx, providerName, info.Subject); err == nil {
		return user, nil
	}

	if info.Email == "" {
		return nil, errNoEmail
	}

	// 2. Verified email matches an existing account → auto-link.
	if info.EmailVerified {
		if user, err := h.store.GetUserByEmail(ctx, info.Email); err == nil {
			if err := h.store.CreateOAuthIdentity(ctx, user.ID, providerName, info.Subject, info.Email); err != nil {
				return nil, err
			}
			return user, nil
		}
	}

	// 3. New user with no password.
	user, err := h.store.CreateUserNoPassword(ctx, info.Email)
	if err != nil {
		return nil, err
	}
	if err := h.store.CreateOAuthIdentity(ctx, user.ID, providerName, info.Subject, info.Email); err != nil {
		return nil, err
	}
	return user, nil
}

func (h *OAuthHandler) validState(r *http.Request, providerName string) bool {
	cookie, err := r.Cookie(oauthStateCookieName)
	if err != nil {
		return false
	}
	want := r.URL.Query().Get("state")
	if want == "" {
		return false
	}
	return cookie.Value == providerName+":"+want
}

func (h *OAuthHandler) clearStateCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   isHTTPSRequest(r),
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func (h *OAuthHandler) redirectError(w http.ResponseWriter, r *http.Request, reason string) {
	http.Redirect(w, r, h.publicBaseURL+"/login?auth_error="+url.QueryEscape(reason), http.StatusFound)
}

func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
