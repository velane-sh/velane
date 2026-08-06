package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// velaneClaims are the JWT claims embedded in access tokens.
type velaneClaims struct {
	Email           string `json:"email"`
	AuthMethod      string `json:"auth_method,omitempty"`
	SSOTenantID     string `json:"sso_tenant_id,omitempty"`
	SSOConnectionID string `json:"sso_connection_id,omitempty"`
	jwt.RegisteredClaims
}

// JWTStore is the store interface required by JWTProvider.
type JWTStore interface {
	PasswordStore // reuse: CreateUser, GetUserByEmail, GetUserByID
	CreateRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (*models.RefreshToken, error)
	GetRefreshTokenByHash(ctx context.Context, tokenHash string) (*models.RefreshToken, error)
	RevokeRefreshToken(ctx context.Context, tokenHash string) error
}

type assuranceJWTStore interface {
	CreateRefreshTokenWithAssurance(ctx context.Context, userID, tokenHash string, expiresAt time.Time, assurance models.SessionAssurance) (*models.RefreshToken, error)
}

// AuthTokenPair is the response returned for login and token refresh.
type AuthTokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"` // access token expiry
	TokenType    string    `json:"token_type"` // "Bearer"
}

// JWTProvider implements Provider using RS256 JWTs (access tokens) + Postgres refresh tokens.
// It embeds PasswordProvider for CreateUser and Authenticate (password check).
type JWTProvider struct {
	passwords *PasswordProvider
	store     JWTStore
	privKey   *rsa.PrivateKey
	pubKey    *rsa.PublicKey
	issuer    string
}

// NewJWTProvider creates a JWTProvider backed by the given store and RSA key pair.
func NewJWTProvider(store JWTStore, privKey *rsa.PrivateKey, issuer string) *JWTProvider {
	return &JWTProvider{
		passwords: NewPasswordProvider(store),
		store:     store,
		privKey:   privKey,
		pubKey:    &privKey.PublicKey,
		issuer:    issuer,
	}
}

// CreateUser delegates to PasswordProvider.
func (p *JWTProvider) CreateUser(ctx context.Context, email, password string) (*models.User, error) {
	return p.passwords.CreateUser(ctx, email, password)
}

// Authenticate verifies the password then issues a JWT access token + refresh token.
// Returns a *models.Session with Token set to the ACCESS token (for API compatibility).
func (p *JWTProvider) Authenticate(ctx context.Context, email, password string) (*models.Session, error) {
	user, err := p.store.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid credentials")
	}

	pair, err := p.issueTokenPair(ctx, user)
	if err != nil {
		return nil, err
	}

	return &models.Session{
		UserID:         user.ID,
		Token:          pair.AccessToken,
		RefreshToken:   pair.RefreshToken,
		ExpiresAt:      pair.ExpiresAt,
		RefreshExpires: time.Now().Add(7 * 24 * time.Hour),
	}, nil
}

// IssueSession issues a new access + refresh token pair for an already-identified
// user (e.g. after OAuth login), bypassing the password check. The returned
// Session has Token set to the access token, matching Authenticate.
func (p *JWTProvider) IssueSession(ctx context.Context, user *models.User) (*models.Session, error) {
	pair, err := p.issueTokenPair(ctx, user)
	if err != nil {
		return nil, err
	}
	return &models.Session{
		UserID:         user.ID,
		Token:          pair.AccessToken,
		RefreshToken:   pair.RefreshToken,
		ExpiresAt:      pair.ExpiresAt,
		RefreshExpires: time.Now().Add(7 * 24 * time.Hour),
	}, nil
}

// IssueOAuthSession marks social-login assurance separately from password login.
func (p *JWTProvider) IssueOAuthSession(ctx context.Context, user *models.User) (*models.Session, error) {
	pair, err := p.issueTokenPairWithAssurance(ctx, user, models.SessionAssurance{AuthMethod: "oauth"})
	if err != nil {
		return nil, err
	}
	return &models.Session{UserID: user.ID, Token: pair.AccessToken, RefreshToken: pair.RefreshToken, ExpiresAt: pair.ExpiresAt, RefreshExpires: time.Now().Add(7 * 24 * time.Hour)}, nil
}

// IssueSSOSession binds a session to exactly one tenant and SSO connection.
func (p *JWTProvider) IssueSSOSession(ctx context.Context, user *models.User, tenantID, connectionID string) (*models.Session, error) {
	pair, err := p.issueTokenPairWithAssurance(ctx, user, models.SessionAssurance{AuthMethod: "sso", SSOTenantID: tenantID, SSOConnectionID: connectionID})
	if err != nil {
		return nil, err
	}
	return &models.Session{UserID: user.ID, Token: pair.AccessToken, RefreshToken: pair.RefreshToken, ExpiresAt: pair.ExpiresAt, RefreshExpires: time.Now().Add(7 * 24 * time.Hour)}, nil
}

// RevokeRefreshToken revokes a raw refresh token value.
func (p *JWTProvider) RevokeRefreshToken(ctx context.Context, rawRefreshToken string) error {
	if strings.TrimSpace(rawRefreshToken) == "" {
		return nil
	}
	return p.store.RevokeRefreshToken(ctx, hashRefreshToken(rawRefreshToken))
}

// ValidateSession validates the RS256 JWT access token (stateless — no DB lookup).
func (p *JWTProvider) ValidateSession(ctx context.Context, rawToken string) (*models.User, error) {
	claims := &velaneClaims{}
	token, err := jwt.ParseWithClaims(rawToken, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return p.pubKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("invalid token: %w", err)
	}
	if !token.Valid {
		return nil, fmt.Errorf("token is not valid")
	}

	issuer, err := claims.GetIssuer()
	if err != nil || issuer != p.issuer {
		return nil, fmt.Errorf("invalid token issuer")
	}

	sub, err := claims.GetSubject()
	if err != nil || sub == "" {
		return nil, fmt.Errorf("token missing subject")
	}

	return p.store.GetUserByID(ctx, sub)
}

// InvalidateSession revokes the refresh token associated with the raw token.
// Access tokens are short-lived and cannot be individually revoked — this is an
// intentional trade-off of stateless JWTs.
// For logout compatibility, we accept the access token here (no-op since it's stateless).
func (p *JWTProvider) InvalidateSession(ctx context.Context, rawToken string) error {
	// Hash the token as a best-effort refresh token lookup.
	// If rawToken is an access JWT, RevokeRefreshToken will simply find no rows.
	hash := hashRefreshToken(rawToken)
	// Ignore errors — the token may already be expired or not be a refresh token.
	_ = p.store.RevokeRefreshToken(ctx, hash)
	return nil
}

// Refresh exchanges a valid refresh token for a new access+refresh token pair.
// The old refresh token is revoked (rotation).
func (p *JWTProvider) Refresh(ctx context.Context, rawRefreshToken string) (*AuthTokenPair, error) {
	hash := hashRefreshToken(rawRefreshToken)

	rt, err := p.store.GetRefreshTokenByHash(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("invalid refresh token: %w", err)
	}

	user, err := p.store.GetUserByID(ctx, rt.UserID)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	// Revoke the old refresh token before issuing a new pair (rotation).
	if err := p.store.RevokeRefreshToken(ctx, hash); err != nil {
		return nil, fmt.Errorf("failed to revoke old refresh token: %w", err)
	}

	return p.issueTokenPairWithAssurance(ctx, user, models.SessionAssurance{AuthMethod: rt.AuthMethod, SSOTenantID: rt.SSOTenantID, SSOConnectionID: rt.SSOConnectionID})
}

// IssueAccessToken creates a signed RS256 JWT with claims: sub=userID, email, iss, iat, exp.
func (p *JWTProvider) IssueAccessToken(user *models.User) (string, time.Time, error) {
	return p.issueAccessTokenWithAssurance(user, models.SessionAssurance{AuthMethod: "local"})
}

func (p *JWTProvider) issueAccessTokenWithAssurance(user *models.User, assurance models.SessionAssurance) (string, time.Time, error) {
	expiresAt := time.Now().Add(15 * time.Minute)
	claims := velaneClaims{
		Email:      user.Email,
		AuthMethod: assurance.AuthMethod, SSOTenantID: assurance.SSOTenantID, SSOConnectionID: assurance.SSOConnectionID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID,
			Issuer:    p.issuer,
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(p.privKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign access token: %w", err)
	}
	return signed, expiresAt, nil
}

// issueTokenPair creates a new access + refresh token pair and persists the refresh token.
func (p *JWTProvider) issueTokenPair(ctx context.Context, user *models.User) (*AuthTokenPair, error) {
	return p.issueTokenPairWithAssurance(ctx, user, models.SessionAssurance{AuthMethod: "local"})
}

func (p *JWTProvider) issueTokenPairWithAssurance(ctx context.Context, user *models.User, assurance models.SessionAssurance) (*AuthTokenPair, error) {
	if assurance.AuthMethod == "" {
		assurance.AuthMethod = "local"
	}
	accessToken, expiresAt, err := p.issueAccessTokenWithAssurance(user, assurance)
	if err != nil {
		return nil, err
	}

	rawRefresh, refreshHash := generateRefreshToken()
	refreshExpiry := time.Now().Add(7 * 24 * time.Hour)

	var rt *models.RefreshToken
	if extended, ok := p.store.(assuranceJWTStore); ok {
		rt, err = extended.CreateRefreshTokenWithAssurance(ctx, user.ID, refreshHash, refreshExpiry, assurance)
	} else {
		rt, err = p.store.CreateRefreshToken(ctx, user.ID, refreshHash, refreshExpiry)
	}
	if err != nil {
		return nil, fmt.Errorf("store refresh token: %w", err)
	}
	rt.Token = rawRefresh

	return &AuthTokenPair{
		AccessToken:  accessToken,
		RefreshToken: rawRefresh,
		ExpiresAt:    expiresAt,
		TokenType:    "Bearer",
	}, nil
}

// SessionAssurance validates a Velane JWT and returns its tenant binding. Legacy
// tokens are treated as local sessions for backwards compatibility.
func (p *JWTProvider) SessionAssurance(rawToken string) (models.SessionAssurance, error) {
	claims := &velaneClaims{}
	token, err := jwt.ParseWithClaims(rawToken, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return p.pubKey, nil
	})
	if err != nil || !token.Valid || claims.Issuer != p.issuer {
		return models.SessionAssurance{}, fmt.Errorf("invalid token")
	}
	method := claims.AuthMethod
	if method == "" {
		method = "local"
	}
	return models.SessionAssurance{AuthMethod: method, SSOTenantID: claims.SSOTenantID, SSOConnectionID: claims.SSOConnectionID}, nil
}

// generateRefreshToken creates a cryptographically random refresh token and its SHA-256 hash.
func generateRefreshToken() (raw, hash string) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	raw = hex.EncodeToString(b)
	hash = hashRefreshToken(raw)
	return
}

// hashRefreshToken returns the hex-encoded SHA-256 hash of a raw token value.
func hashRefreshToken(raw string) string {
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}
