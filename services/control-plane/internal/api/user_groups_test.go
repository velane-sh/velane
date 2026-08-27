// Package api_test — integration tests for user groups and group-based
// integration access control. These tests require TEST_DATABASE_URL.
package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/callerid"
	"github.com/abskrj/velane/services/control-plane/internal/models"
)

// decodeInto decodes a recorded JSON response into out.
func decodeInto(t *testing.T, rec *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.NewDecoder(rec.Body).Decode(out); err != nil {
		t.Fatalf("decode JSON body (status %d): %v", rec.Code, err)
	}
}

// groupUser creates a tenant member with the given role and an API key owned by
// that user, returning the user ID and the plain key.
func groupUser(t *testing.T, env *testEnv, role string, scopes ...string) (string, string) {
	t.Helper()
	ctx := context.Background()

	email := fmt.Sprintf("user-%d@example.com", time.Now().UnixNano())
	user, err := env.store.CreateUserNoPassword(ctx, email)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, err := env.store.AddMember(ctx, env.tenant.ID, user.ID, role); err != nil {
		t.Fatalf("add member: %v", err)
	}
	key, plain, err := env.store.CreateAPIKeyWithPlainForUser(ctx, env.tenant.ID, "owned-key", scopes, &user.ID)
	if err != nil {
		t.Fatalf("create owned api key: %v", err)
	}
	if key.UserID == nil || *key.UserID != user.ID {
		t.Fatalf("api key must record its owner")
	}
	return user.ID, plain
}

func createGroup(t *testing.T, env *testEnv, name string) string {
	t.Helper()
	rec := env.do(t, http.MethodPost, "/v1/tenant/groups", env.manageKey, map[string]any{
		"name":        name,
		"description": "test group",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create group: status=%d body=%s", rec.Code, rec.Body.String())
	}
	group := decodeJSON(t, rec)
	id, _ := group["id"].(string)
	if id == "" {
		t.Fatalf("group id missing in response: %s", rec.Body.String())
	}
	return id
}

func TestUserGroups_CRUDRequiresAdminRole(t *testing.T) {
	env := setupWithNango(t)

	groupID := createGroup(t, env, fmt.Sprintf("support-%d", time.Now().UnixNano()))

	rec := env.do(t, http.MethodGet, "/v1/tenant/groups", env.manageKey, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list groups: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// invoke-only keys map to the member role and must not manage groups.
	rec = env.do(t, http.MethodPost, "/v1/tenant/groups", env.invokeKey, map[string]any{"name": "nope"})
	if rec.Code != http.StatusForbidden {
		t.Errorf("create group with invoke key: status=%d; want 403", rec.Code)
	}

	rec = env.do(t, http.MethodDelete, "/v1/tenant/groups/"+groupID, env.manageKey, nil)
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete group: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUserGroups_MembersAndGrants(t *testing.T) {
	env := setupWithNango(t)
	userID, _ := groupUser(t, env, models.RoleMember, "invoke")
	profile := configureProfile(t, env, "github", fmt.Sprintf("granted-%d", time.Now().UnixNano()), false)
	groupID := createGroup(t, env, fmt.Sprintf("granted-group-%d", time.Now().UnixNano()))

	rec := env.do(t, http.MethodPost, "/v1/tenant/groups/"+groupID+"/members", env.manageKey,
		map[string]any{"user_id": userID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add member: status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = env.do(t, http.MethodPost, "/v1/tenant/groups/"+groupID+"/integrations", env.manageKey,
		map[string]any{"credential_profile_id": profile["id"]})
	if rec.Code != http.StatusCreated {
		t.Fatalf("grant integration: status=%d body=%s", rec.Code, rec.Body.String())
	}

	rec = env.do(t, http.MethodGet, "/v1/tenant/groups", env.manageKey, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list groups: status=%d body=%s", rec.Code, rec.Body.String())
	}
	var groups []struct {
		ID      string `json:"id"`
		Members []struct {
			UserID string `json:"user_id"`
		} `json:"members"`
		Grants []struct {
			CredentialProfileID string `json:"credential_profile_id"`
		} `json:"integration_grants"`
	}
	decodeInto(t, rec, &groups)
	var found bool
	for _, g := range groups {
		if g.ID != groupID {
			continue
		}
		found = true
		if len(g.Members) != 1 || g.Members[0].UserID != userID {
			t.Errorf("members = %+v; want the added user", g.Members)
		}
		if len(g.Grants) != 1 || g.Grants[0].CredentialProfileID != profile["id"] {
			t.Errorf("grants = %+v; want the granted profile", g.Grants)
		}
	}
	if !found {
		t.Fatalf("created group missing from list")
	}

	rec = env.do(t, http.MethodDelete, "/v1/tenant/groups/"+groupID+"/integrations", env.manageKey,
		map[string]any{"credential_profile_id": profile["id"]})
	if rec.Code != http.StatusNoContent {
		t.Errorf("revoke integration: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUserGroups_ListConfiguredFiltersRestrictedProfiles(t *testing.T) {
	env := setupWithNango(t)
	suffix := time.Now().UnixNano()
	open := configureProfile(t, env, "github", fmt.Sprintf("open-%d", suffix), false)
	restricted := configureProfile(t, env, "github", fmt.Sprintf("restricted-%d", suffix), false)

	grantedUser, grantedKey := groupUser(t, env, models.RoleMember, "invoke", "manage")
	_, otherKey := groupUser(t, env, models.RoleMember, "invoke", "manage")

	groupID := createGroup(t, env, fmt.Sprintf("filter-group-%d", suffix))
	if rec := env.do(t, http.MethodPost, "/v1/tenant/groups/"+groupID+"/members", env.manageKey,
		map[string]any{"user_id": grantedUser}); rec.Code != http.StatusCreated {
		t.Fatalf("add member: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := env.do(t, http.MethodPost, "/v1/tenant/groups/"+groupID+"/integrations", env.manageKey,
		map[string]any{"credential_profile_id": restricted["id"]}); rec.Code != http.StatusCreated {
		t.Fatalf("grant integration: status=%d body=%s", rec.Code, rec.Body.String())
	}

	listed := func(key string) map[string]bool {
		rec := env.do(t, http.MethodGet, "/v1/integrations/configured?status=all", key, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("list configured: status=%d body=%s", rec.Code, rec.Body.String())
		}
		var profiles []struct {
			ID string `json:"id"`
		}
		decodeInto(t, rec, &profiles)
		out := map[string]bool{}
		for _, p := range profiles {
			out[p.ID] = true
		}
		return out
	}

	adminView := listed(env.manageKey)
	if !adminView[restricted["id"].(string)] {
		t.Error("admin must see restricted profiles")
	}

	grantedView := listed(grantedKey)
	if !grantedView[restricted["id"].(string)] {
		t.Error("granted group member must see the restricted profile")
	}
	if !grantedView[open["id"].(string)] {
		t.Error("unrestricted profiles stay visible to every member")
	}

	otherView := listed(otherKey)
	if otherView[restricted["id"].(string)] {
		t.Error("member outside the granted group must not see the restricted profile")
	}
	if !otherView[open["id"].(string)] {
		t.Error("unrestricted profiles stay visible to members outside the group")
	}
}

func TestProxy_EnforcesGroupGrants(t *testing.T) {
	env := setupWithNango(t)
	suffix := time.Now().UnixNano()
	profile := configureProfile(t, env, "github", "default", true)

	if rec := env.do(t, http.MethodPost, "/v1/tenant/connections", env.manageKey, map[string]any{
		"provider":              "github",
		"credential_profile_id": profile["id"],
	}); rec.Code != http.StatusCreated {
		t.Fatalf("record connection: status=%d body=%s", rec.Code, rec.Body.String())
	}

	groupID := createGroup(t, env, fmt.Sprintf("proxy-group-%d", suffix))
	if rec := env.do(t, http.MethodPost, "/v1/tenant/groups/"+groupID+"/integrations", env.manageKey,
		map[string]any{"credential_profile_id": profile["id"]}); rec.Code != http.StatusCreated {
		t.Fatalf("grant integration: status=%d body=%s", rec.Code, rec.Body.String())
	}

	proxy := func(callerToken string) int {
		req := httptest.NewRequest(http.MethodGet, "/v1/proxy/github/user", nil)
		req.Header.Set("X-Velane-Tenant", env.tenant.ID)
		if callerToken != "" {
			req.Header.Set(callerid.Header, callerToken)
		}
		rec := httptest.NewRecorder()
		env.router.ServeHTTP(rec, req)
		return rec.Code
	}

	sign := func(claims callerid.Claims) string {
		claims.TenantID = env.tenant.ID
		token, err := callerid.Sign(testEncKey, claims, time.Hour)
		if err != nil {
			t.Fatalf("sign caller token: %v", err)
		}
		return token
	}

	if code := proxy(sign(callerid.Claims{UserID: "user-1", Role: models.RoleMember, GroupIDs: []string{groupID}})); code != http.StatusOK {
		t.Errorf("granted member proxy status = %d; want 200", code)
	}
	if code := proxy(sign(callerid.Claims{UserID: "user-2", Role: models.RoleMember, GroupIDs: []string{"unrelated"}})); code != http.StatusForbidden {
		t.Errorf("ungranted member proxy status = %d; want 403", code)
	}
	if code := proxy(sign(callerid.Claims{Role: models.RoleAdmin})); code != http.StatusOK {
		t.Errorf("admin proxy status = %d; want 200", code)
	}

	// A forged token is not signed with the control plane key and is ignored;
	// with strict mode off the caller falls back to legacy tenant-wide access.
	if code := proxy("not-a-real-token"); code != http.StatusOK {
		t.Errorf("legacy fallback proxy status = %d; want 200", code)
	}
	if code := proxy(""); code != http.StatusOK {
		t.Errorf("tokenless proxy status = %d; want 200", code)
	}
	if code := proxy(sign(callerid.Claims{Role: models.RoleInvoke})); code != http.StatusOK {
		t.Errorf("legacy tenant-wide key proxy status = %d; want 200", code)
	}

	// With strict mode enabled, unidentified callers are denied.
	if err := env.store.SetTenantRBACStrictMode(context.Background(), env.tenant.ID, true); err != nil {
		t.Fatalf("SetTenantRBACStrictMode: %v", err)
	}
	if code := proxy(""); code != http.StatusForbidden {
		t.Errorf("strict-mode tokenless proxy status = %d; want 403", code)
	}
	// A legacy tenant-wide API key mints a token with a role but no user; it
	// keeps tenant-wide access until strict mode is on.
	if code := proxy(sign(callerid.Claims{Role: models.RoleInvoke})); code != http.StatusForbidden {
		t.Errorf("strict-mode legacy-key proxy status = %d; want 403", code)
	}
	if code := proxy(sign(callerid.Claims{UserID: "user-1", Role: models.RoleMember, GroupIDs: []string{groupID}})); code != http.StatusOK {
		t.Errorf("strict-mode granted member proxy status = %d; want 200", code)
	}
}
