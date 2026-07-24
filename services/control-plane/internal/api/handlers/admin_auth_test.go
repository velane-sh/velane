package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/api/handlers"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"go.uber.org/zap"
)

// --- mock auth provider ---

type mockAuthProvider struct {
	createUserFn   func(ctx context.Context, email, password string) (*models.User, error)
	authenticateFn func(ctx context.Context, email, password string) (*models.Session, error)
	validateFn     func(ctx context.Context, rawToken string) (*models.User, error)
	invalidateFn   func(ctx context.Context, rawToken string) error
}

func (m *mockAuthProvider) CreateUser(ctx context.Context, email, password string) (*models.User, error) {
	return m.createUserFn(ctx, email, password)
}
func (m *mockAuthProvider) Authenticate(ctx context.Context, email, password string) (*models.Session, error) {
	return m.authenticateFn(ctx, email, password)
}
func (m *mockAuthProvider) ValidateSession(ctx context.Context, rawToken string) (*models.User, error) {
	return m.validateFn(ctx, rawToken)
}
func (m *mockAuthProvider) InvalidateSession(ctx context.Context, rawToken string) error {
	return m.invalidateFn(ctx, rawToken)
}

// --- mock admin auth store ---

type mockAdminAuthStore struct {
	getUserByEmailFn            func(ctx context.Context, email string) (*models.User, error)
	getInviteByTokenHashFn      func(ctx context.Context, hash string) (*models.InviteToken, error)
	acceptInviteFn              func(ctx context.Context, id string) error
	addMemberFn                 func(ctx context.Context, tenantID, userID, role string) (*models.TenantMember, error)
	getUserPrimaryTenantSlugFn  func(ctx context.Context, userID string) (string, error)
	createTenantFn              func(ctx context.Context, name, slug string) (*models.Tenant, error)
	listUserTenantMembershipsFn func(ctx context.Context, userID string) ([]*models.UserTenantMembership, error)
}

func (m *mockAdminAuthStore) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	return m.getUserByEmailFn(ctx, email)
}
func (m *mockAdminAuthStore) GetInviteByTokenHash(ctx context.Context, hash string) (*models.InviteToken, error) {
	return m.getInviteByTokenHashFn(ctx, hash)
}
func (m *mockAdminAuthStore) AcceptInvite(ctx context.Context, id string) error {
	return m.acceptInviteFn(ctx, id)
}
func (m *mockAdminAuthStore) AddMember(ctx context.Context, tenantID, userID, role string) (*models.TenantMember, error) {
	return m.addMemberFn(ctx, tenantID, userID, role)
}
func (m *mockAdminAuthStore) GetUserPrimaryTenantSlug(ctx context.Context, userID string) (string, error) {
	if m.getUserPrimaryTenantSlugFn != nil {
		return m.getUserPrimaryTenantSlugFn(ctx, userID)
	}
	return "", nil
}
func (m *mockAdminAuthStore) CreateTenant(ctx context.Context, name, slug string) (*models.Tenant, error) {
	return m.createTenantFn(ctx, name, slug)
}
func (m *mockAdminAuthStore) ListUserTenantMemberships(ctx context.Context, userID string) ([]*models.UserTenantMembership, error) {
	if m.listUserTenantMembershipsFn != nil {
		return m.listUserTenantMembershipsFn(ctx, userID)
	}
	return []*models.UserTenantMembership{}, nil
}

func newTestUser() *models.User {
	return &models.User{ID: "u1", Email: "alice@example.com", CreatedAt: time.Now(), UpdatedAt: time.Now()}
}

func newTestSession() *models.Session {
	return &models.Session{
		ID:        "sess1",
		UserID:    "u1",
		Token:     "rawtoken123",
		ExpiresAt: time.Now().Add(24 * time.Hour),
		CreatedAt: time.Now(),
	}
}

// TestRegister_Success verifies that a valid register request creates a user and returns a session.
func TestRegister_Success(t *testing.T) {
	provider := &mockAuthProvider{
		createUserFn:   func(_ context.Context, email, _ string) (*models.User, error) { return newTestUser(), nil },
		authenticateFn: func(_ context.Context, _, _ string) (*models.Session, error) { return newTestSession(), nil },
	}
	store := &mockAdminAuthStore{
		getUserByEmailFn: func(_ context.Context, _ string) (*models.User, error) {
			return nil, errors.New("not found")
		},
	}
	h := handlers.NewAdminAuthHandler(provider, store, zap.NewNop())

	body, _ := json.Marshal(map[string]string{"email": "alice@example.com", "password": "pass"})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.Register(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["session_token"] == nil {
		t.Error("expected session_token in response")
	}
	if len(rr.Result().Cookies()) == 0 {
		t.Error("expected session cookie to be set")
	}
}

// TestRegister_WithInvite verifies that register with a valid invite token accepts the invite and adds member.
func TestRegister_WithInvite(t *testing.T) {
	inviteAccepted := false
	memberAdded := false
	expire := time.Now().Add(72 * time.Hour)

	provider := &mockAuthProvider{
		createUserFn:   func(_ context.Context, email, _ string) (*models.User, error) { return newTestUser(), nil },
		authenticateFn: func(_ context.Context, _, _ string) (*models.Session, error) { return newTestSession(), nil },
	}
	store := &mockAdminAuthStore{
		getUserByEmailFn: func(_ context.Context, _ string) (*models.User, error) {
			return nil, errors.New("not found")
		},
		getInviteByTokenHashFn: func(_ context.Context, _ string) (*models.InviteToken, error) {
			return &models.InviteToken{
				ID:        "inv1",
				TenantID:  "t1",
				Role:      "manage",
				ExpiresAt: expire,
			}, nil
		},
		acceptInviteFn: func(_ context.Context, _ string) error {
			inviteAccepted = true
			return nil
		},
		addMemberFn: func(_ context.Context, _, _, _ string) (*models.TenantMember, error) {
			memberAdded = true
			return &models.TenantMember{}, nil
		},
	}
	h := handlers.NewAdminAuthHandler(provider, store, zap.NewNop())

	body, _ := json.Marshal(map[string]string{
		"email":        "alice@example.com",
		"password":     "pass",
		"invite_token": "rawtoken",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.Register(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	if !inviteAccepted {
		t.Error("expected AcceptInvite to be called")
	}
	if !memberAdded {
		t.Error("expected AddMember to be called")
	}
}

// TestLogin_Success verifies that a valid login request returns a session token.
func TestLogin_Success(t *testing.T) {
	provider := &mockAuthProvider{
		authenticateFn: func(_ context.Context, _, _ string) (*models.Session, error) { return newTestSession(), nil },
	}
	h := handlers.NewAdminAuthHandler(provider, &mockAdminAuthStore{}, zap.NewNop())

	body, _ := json.Marshal(map[string]string{"email": "alice@example.com", "password": "pass"})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.Login(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["session_token"] == nil {
		t.Error("expected session_token in response")
	}
	if len(rr.Result().Cookies()) == 0 {
		t.Error("expected session cookie to be set")
	}
}

// TestLogin_WrongPassword verifies that wrong credentials return 401.
func TestLogin_WrongPassword(t *testing.T) {
	provider := &mockAuthProvider{
		authenticateFn: func(_ context.Context, _, _ string) (*models.Session, error) {
			return nil, errors.New("invalid credentials")
		},
	}
	h := handlers.NewAdminAuthHandler(provider, &mockAdminAuthStore{}, zap.NewNop())

	body, _ := json.Marshal(map[string]string{"email": "alice@example.com", "password": "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.Login(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

// TestLogout verifies that the logout endpoint calls InvalidateSession, clears the cookie, and returns 204.
func TestLogout(t *testing.T) {
	invalidated := false
	provider := &mockAuthProvider{
		invalidateFn: func(_ context.Context, raw string) error {
			if raw != "rawtoken123" {
				t.Fatalf("invalidated token = %q; want rawtoken123", raw)
			}
			invalidated = true
			return nil
		},
	}
	h := handlers.NewAdminAuthHandler(provider, &mockAdminAuthStore{}, zap.NewNop())

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: "velane_session", Value: "rawtoken123"})
	rr := httptest.NewRecorder()

	h.Logout(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rr.Code)
	}
	if !invalidated {
		t.Error("expected InvalidateSession to be called")
	}
	cookies := rr.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != "velane_session" || cookies[0].MaxAge != -1 {
		t.Fatalf("expected cleared session cookie, got %+v", cookies)
	}
}

func TestRefreshToken_AcceptsCookieWithEmptyBody(t *testing.T) {
	h := handlers.NewAdminAuthHandler(&mockAuthProvider{}, &mockAdminAuthStore{}, zap.NewNop())

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "velane_refresh", Value: "refresh-token"})
	rr := httptest.NewRecorder()

	h.RefreshToken(rr, req)

	// A non-JWT provider cannot rotate the token, but reaching this response proves
	// the cookie was accepted instead of the empty body being rejected as invalid.
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestRefreshToken_RejectsMalformedJSON(t *testing.T) {
	h := handlers.NewAdminAuthHandler(&mockAuthProvider{}, &mockAdminAuthStore{}, zap.NewNop())

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/auth/refresh", bytes.NewBufferString("{"))
	req.AddCookie(&http.Cookie{Name: "velane_refresh", Value: "refresh-token"})
	rr := httptest.NewRecorder()

	h.RefreshToken(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestMe_ValidSession verifies that GET /me returns the user from context.
func TestMe_ValidSession(t *testing.T) {
	h := handlers.NewAdminAuthHandler(&mockAuthProvider{}, &mockAdminAuthStore{}, zap.NewNop())

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/auth/me", nil)
	// Simulate SessionAuth having set the user in context.
	ctx := setSessionUser(req.Context(), newTestUser())
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()

	h.Me(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp["email"] == nil {
		t.Error("expected email in response")
	}
}

// TestMe_NoSession verifies that GET /me without a session returns 401.
func TestMe_NoSession(t *testing.T) {
	h := handlers.NewAdminAuthHandler(&mockAuthProvider{}, &mockAdminAuthStore{}, zap.NewNop())

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/auth/me", nil)
	rr := httptest.NewRecorder()

	h.Me(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}
}

func TestListMyTenants(t *testing.T) {
	h := handlers.NewAdminAuthHandler(&mockAuthProvider{}, &mockAdminAuthStore{
		listUserTenantMembershipsFn: func(_ context.Context, userID string) ([]*models.UserTenantMembership, error) {
			if userID != "u1" {
				t.Fatalf("userID = %q; want u1", userID)
			}
			return []*models.UserTenantMembership{
				{TenantID: "t1", Slug: "acme", Name: "Acme", Role: "admin"},
				{TenantID: "t2", Slug: "beta", Name: "Beta", Role: "manage"},
			}, nil
		},
	}, zap.NewNop())

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/auth/orgs", nil)
	req = req.WithContext(setSessionUser(req.Context(), newTestUser()))
	rr := httptest.NewRecorder()

	h.ListMyTenants(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var resp []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected 2 orgs, got %d", len(resp))
	}
	if resp[0]["slug"] != "acme" {
		t.Fatalf("expected first slug acme, got %v", resp[0]["slug"])
	}
}

func TestCreateMyTenant(t *testing.T) {
	created := false
	memberAdded := false
	h := handlers.NewAdminAuthHandler(&mockAuthProvider{}, &mockAdminAuthStore{
		createTenantFn: func(_ context.Context, name, slug string) (*models.Tenant, error) {
			created = true
			if name != "Acme" || slug != "acme-org" {
				t.Fatalf("unexpected tenant payload: %q %q", name, slug)
			}
			return &models.Tenant{ID: "t1", Name: name, Slug: slug}, nil
		},
		addMemberFn: func(_ context.Context, tenantID, userID, role string) (*models.TenantMember, error) {
			memberAdded = true
			if tenantID != "t1" || userID != "u1" || role != "admin" {
				t.Fatalf("unexpected membership payload: %q %q %q", tenantID, userID, role)
			}
			return &models.TenantMember{}, nil
		},
	}, zap.NewNop())

	body, _ := json.Marshal(map[string]string{"name": "Acme", "slug": "acme-org"})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/auth/orgs", bytes.NewReader(body))
	req = req.WithContext(setSessionUser(req.Context(), newTestUser()))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.CreateMyTenant(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rr.Code, rr.Body.String())
	}
	if !created || !memberAdded {
		t.Fatalf("expected tenant creation and membership attachment, created=%v memberAdded=%v", created, memberAdded)
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if resp["role"] != "admin" || resp["slug"] != "acme-org" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

func TestCreateMyTenant_InvalidSlug(t *testing.T) {
	h := handlers.NewAdminAuthHandler(&mockAuthProvider{}, &mockAdminAuthStore{}, zap.NewNop())

	body, _ := json.Marshal(map[string]string{"name": "Acme", "slug": "Bad Slug"})
	req := httptest.NewRequest(http.MethodPost, "/v1/admin/auth/orgs", bytes.NewReader(body))
	req = req.WithContext(setSessionUser(req.Context(), newTestUser()))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.CreateMyTenant(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
}
