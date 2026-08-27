package postgres_test

import (
	"context"
	"testing"

	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
)

// seedGroupFixture creates a tenant, a member user and a credential profile.
func seedGroupFixture(t *testing.T, store *postgres.Store) (tenantID, userID, profileID string) {
	t.Helper()
	ctx := context.Background()

	tenant, err := store.CreateTenant(ctx, "Groups Org", uniqueSlug(t, "groups-org"))
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	user, err := store.CreateUserNoPassword(ctx, uniqueSlug(t, "member")+"@example.com")
	if err != nil {
		t.Fatalf("CreateUserNoPassword: %v", err)
	}
	if _, err := store.AddMember(ctx, tenant.ID, user.ID, models.RoleMember); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	profile, err := store.UpsertIntegrationCredentialProfile(
		ctx, tenant.ID, "github", uniqueSlug(t, "alias"), "GitHub", user.ID,
		"OAUTH2", map[string]string{}, "", true, "", []byte("0123456789abcdef0123456789abcdef"),
	)
	if err != nil {
		t.Fatalf("UpsertIntegrationCredentialProfile: %v", err)
	}
	return tenant.ID, user.ID, profile.ID
}

func TestUserGroupLifecycle(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	tenantID, userID, profileID := seedGroupFixture(t, store)

	group, err := store.CreateUserGroup(ctx, tenantID, "support", "Support engineers")
	if err != nil {
		t.Fatalf("CreateUserGroup: %v", err)
	}
	if group.TenantID != tenantID || group.Name != "support" {
		t.Fatalf("unexpected group %+v", group)
	}

	groups, err := store.ListUserGroups(ctx, tenantID)
	if err != nil {
		t.Fatalf("ListUserGroups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("group count = %d; want 1", len(groups))
	}

	if err := store.AddUserGroupMember(ctx, tenantID, group.ID, userID); err != nil {
		t.Fatalf("AddUserGroupMember: %v", err)
	}
	// Adding twice must be idempotent.
	if err := store.AddUserGroupMember(ctx, tenantID, group.ID, userID); err != nil {
		t.Fatalf("AddUserGroupMember (repeat): %v", err)
	}
	members, err := store.ListUserGroupMembers(ctx, tenantID, group.ID)
	if err != nil {
		t.Fatalf("ListUserGroupMembers: %v", err)
	}
	if len(members) != 1 || members[0].UserID != userID {
		t.Fatalf("unexpected members %+v", members)
	}

	userGroups, err := store.ListGroupIDsForUser(ctx, tenantID, userID)
	if err != nil {
		t.Fatalf("ListGroupIDsForUser: %v", err)
	}
	if len(userGroups) != 1 || userGroups[0] != group.ID {
		t.Fatalf("user groups = %v; want [%s]", userGroups, group.ID)
	}

	if err := store.GrantCredentialProfileToGroup(ctx, tenantID, group.ID, profileID); err != nil {
		t.Fatalf("GrantCredentialProfileToGroup: %v", err)
	}

	granted, err := store.CredentialProfileGrantedToGroups(ctx, profileID, []string{group.ID})
	if err != nil {
		t.Fatalf("CredentialProfileGrantedToGroups: %v", err)
	}
	if !granted {
		t.Error("profile must be granted to the group it was granted to")
	}
	granted, err = store.CredentialProfileGrantedToGroups(ctx, profileID, []string{"other-group"})
	if err != nil {
		t.Fatalf("CredentialProfileGrantedToGroups (other): %v", err)
	}
	if granted {
		t.Error("profile must not be granted to an unrelated group")
	}

	restricted, err := store.CredentialProfileHasGrants(ctx, profileID)
	if err != nil {
		t.Fatalf("CredentialProfileHasGrants: %v", err)
	}
	if !restricted {
		t.Error("a profile with grants must be reported as restricted")
	}

	profileIDs, err := store.ListCredentialProfileIDsForGroups(ctx, tenantID, []string{group.ID})
	if err != nil {
		t.Fatalf("ListCredentialProfileIDsForGroups: %v", err)
	}
	if len(profileIDs) != 1 || profileIDs[0] != profileID {
		t.Fatalf("granted profiles = %v; want [%s]", profileIDs, profileID)
	}

	if err := store.RevokeCredentialProfileFromGroup(ctx, tenantID, group.ID, profileID); err != nil {
		t.Fatalf("RevokeCredentialProfileFromGroup: %v", err)
	}
	restricted, err = store.CredentialProfileHasGrants(ctx, profileID)
	if err != nil {
		t.Fatalf("CredentialProfileHasGrants after revoke: %v", err)
	}
	if restricted {
		t.Error("profile must be unrestricted once its last grant is revoked")
	}

	if err := store.DeleteUserGroup(ctx, tenantID, group.ID); err != nil {
		t.Fatalf("DeleteUserGroup: %v", err)
	}
	groups, err = store.ListUserGroups(ctx, tenantID)
	if err != nil {
		t.Fatalf("ListUserGroups after delete: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("group count after delete = %d; want 0", len(groups))
	}
}

func TestUserGroupsAreTenantScoped(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	tenantID, userID, _ := seedGroupFixture(t, store)

	other, err := store.CreateTenant(ctx, "Other Org", uniqueSlug(t, "other-org"))
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}

	group, err := store.CreateUserGroup(ctx, tenantID, "engineers", "")
	if err != nil {
		t.Fatalf("CreateUserGroup: %v", err)
	}

	if err := store.AddUserGroupMember(ctx, other.ID, group.ID, userID); err == nil {
		t.Error("adding a member through a foreign tenant must fail")
	}
	if err := store.DeleteUserGroup(ctx, other.ID, group.ID); err == nil {
		t.Error("deleting a group through a foreign tenant must fail")
	}

	groups, err := store.ListUserGroups(ctx, other.ID)
	if err != nil {
		t.Fatalf("ListUserGroups: %v", err)
	}
	if len(groups) != 0 {
		t.Fatalf("foreign tenant sees %d groups; want 0", len(groups))
	}
}

func TestAPIKeyInheritsCreatorGroups(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	tenantID, userID, _ := seedGroupFixture(t, store)

	group, err := store.CreateUserGroup(ctx, tenantID, "key-owners", "")
	if err != nil {
		t.Fatalf("CreateUserGroup: %v", err)
	}
	if err := store.AddUserGroupMember(ctx, tenantID, group.ID, userID); err != nil {
		t.Fatalf("AddUserGroupMember: %v", err)
	}

	key, _, err := store.CreateAPIKeyWithPlainForUser(ctx, tenantID, "owned", []string{"invoke"}, &userID)
	if err != nil {
		t.Fatalf("CreateAPIKeyWithPlainForUser: %v", err)
	}
	if key.UserID == nil || *key.UserID != userID {
		t.Fatalf("key user_id = %v; want %s", key.UserID, userID)
	}

	groupIDs, err := store.ListGroupIDsForAPIKey(ctx, tenantID, key.ID)
	if err != nil {
		t.Fatalf("ListGroupIDsForAPIKey: %v", err)
	}
	if len(groupIDs) != 1 || groupIDs[0] != group.ID {
		t.Fatalf("key groups = %v; want [%s]", groupIDs, group.ID)
	}

	// Legacy keys have no owner and therefore no group access.
	legacy, _, err := store.CreateAPIKeyWithPlain(ctx, tenantID, "legacy", []string{"invoke"})
	if err != nil {
		t.Fatalf("CreateAPIKeyWithPlain: %v", err)
	}
	if legacy.UserID != nil {
		t.Fatalf("legacy key user_id = %v; want nil", legacy.UserID)
	}
	groupIDs, err = store.ListGroupIDsForAPIKey(ctx, tenantID, legacy.ID)
	if err != nil {
		t.Fatalf("ListGroupIDsForAPIKey (legacy): %v", err)
	}
	if len(groupIDs) != 0 {
		t.Fatalf("legacy key groups = %v; want none", groupIDs)
	}
}

func TestTenantRBACStrictMode(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	tenantID, _, _ := seedGroupFixture(t, store)

	strict, err := store.TenantRBACStrictMode(ctx, tenantID)
	if err != nil {
		t.Fatalf("TenantRBACStrictMode: %v", err)
	}
	if strict {
		t.Error("strict mode must default to false for backward compatibility")
	}

	if err := store.SetTenantRBACStrictMode(ctx, tenantID, true); err != nil {
		t.Fatalf("SetTenantRBACStrictMode: %v", err)
	}
	strict, err = store.TenantRBACStrictMode(ctx, tenantID)
	if err != nil {
		t.Fatalf("TenantRBACStrictMode after set: %v", err)
	}
	if !strict {
		t.Error("strict mode must be true once enabled")
	}
}
