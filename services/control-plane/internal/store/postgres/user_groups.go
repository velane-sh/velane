package postgres

import (
	"context"
	"fmt"

	"github.com/abskrj/velane/services/control-plane/internal/ids"
	"github.com/abskrj/velane/services/control-plane/internal/models"
)

// CreateUserGroup inserts a tenant-scoped group. Group names are unique per tenant.
func (s *Store) CreateUserGroup(ctx context.Context, tenantID, name, description string) (*models.UserGroup, error) {
	row := s.pool.QueryRow(ctx,
		`INSERT INTO user_groups (id, tenant_id, name, description)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, tenant_id, name, description, created_at, updated_at`,
		ids.New(), tenantID, name, description,
	)
	g, err := scanUserGroup(row)
	if err != nil {
		return nil, fmt.Errorf("CreateUserGroup: %w", err)
	}
	return g, nil
}

// GetUserGroup returns a single group scoped to its tenant.
func (s *Store) GetUserGroup(ctx context.Context, tenantID, groupID string) (*models.UserGroup, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, name, description, created_at, updated_at
		 FROM user_groups WHERE tenant_id = $1 AND id = $2`,
		tenantID, groupID,
	)
	g, err := scanUserGroup(row)
	if err != nil {
		return nil, fmt.Errorf("GetUserGroup: %w", err)
	}
	return g, nil
}

// ListUserGroups returns all groups for a tenant, oldest first.
func (s *Store) ListUserGroups(ctx context.Context, tenantID string) ([]*models.UserGroup, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, name, description, created_at, updated_at
		 FROM user_groups WHERE tenant_id = $1 ORDER BY created_at ASC`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("ListUserGroups query: %w", err)
	}
	defer rows.Close()

	groups := []*models.UserGroup{}
	for rows.Next() {
		g, err := scanUserGroup(rows)
		if err != nil {
			return nil, fmt.Errorf("ListUserGroups scan: %w", err)
		}
		groups = append(groups, g)
	}
	return groups, rows.Err()
}

// DeleteUserGroup removes a group and, by cascade, its members and grants.
func (s *Store) DeleteUserGroup(ctx context.Context, tenantID, groupID string) error {
	result, err := s.pool.Exec(ctx,
		`DELETE FROM user_groups WHERE tenant_id = $1 AND id = $2`,
		tenantID, groupID,
	)
	if err != nil {
		return fmt.Errorf("DeleteUserGroup: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("user group not found")
	}
	return nil
}

// AddUserGroupMember adds a user to a group. It is idempotent.
func (s *Store) AddUserGroupMember(ctx context.Context, tenantID, groupID, userID string) error {
	result, err := s.pool.Exec(ctx,
		`INSERT INTO user_group_members (group_id, user_id)
		 SELECT g.id, $3 FROM user_groups g WHERE g.id = $2 AND g.tenant_id = $1
		 ON CONFLICT (group_id, user_id) DO NOTHING`,
		tenantID, groupID, userID,
	)
	if err != nil {
		return fmt.Errorf("AddUserGroupMember: %w", err)
	}
	if result.RowsAffected() == 0 {
		// Either the group does not belong to the tenant or the row already exists.
		if _, err := s.GetUserGroup(ctx, tenantID, groupID); err != nil {
			return fmt.Errorf("user group not found")
		}
	}
	return nil
}

// RemoveUserGroupMember removes a user from a group.
func (s *Store) RemoveUserGroupMember(ctx context.Context, tenantID, groupID, userID string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM user_group_members m
		 USING user_groups g
		 WHERE m.group_id = g.id AND g.tenant_id = $1 AND m.group_id = $2 AND m.user_id = $3`,
		tenantID, groupID, userID,
	)
	if err != nil {
		return fmt.Errorf("RemoveUserGroupMember: %w", err)
	}
	return nil
}

// ListUserGroupMembers returns the members of a group with their email addresses.
func (s *Store) ListUserGroupMembers(ctx context.Context, tenantID, groupID string) ([]*models.UserGroupMember, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT m.group_id, m.user_id, u.email, m.added_at
		 FROM user_group_members m
		 JOIN user_groups g ON g.id = m.group_id
		 JOIN users u ON u.id = m.user_id
		 WHERE g.tenant_id = $1 AND m.group_id = $2
		 ORDER BY m.added_at ASC`,
		tenantID, groupID,
	)
	if err != nil {
		return nil, fmt.Errorf("ListUserGroupMembers query: %w", err)
	}
	defer rows.Close()

	members := []*models.UserGroupMember{}
	for rows.Next() {
		var m models.UserGroupMember
		if err := rows.Scan(&m.GroupID, &m.UserID, &m.Email, &m.AddedAt); err != nil {
			return nil, fmt.Errorf("ListUserGroupMembers scan: %w", err)
		}
		members = append(members, &m)
	}
	return members, rows.Err()
}

// GrantCredentialProfileToGroup grants a group access to a credential profile.
// Both the group and the profile must belong to the tenant.
func (s *Store) GrantCredentialProfileToGroup(ctx context.Context, tenantID, groupID, profileID string) error {
	result, err := s.pool.Exec(ctx,
		`INSERT INTO integration_group_grants (group_id, credential_profile_id)
		 SELECT g.id, p.id
		 FROM user_groups g
		 JOIN integration_credential_profiles p ON p.tenant_id = g.tenant_id
		 WHERE g.tenant_id = $1 AND g.id = $2 AND p.id = $3 AND p.deleted_at IS NULL
		 ON CONFLICT (group_id, credential_profile_id) DO NOTHING`,
		tenantID, groupID, profileID,
	)
	if err != nil {
		return fmt.Errorf("GrantCredentialProfileToGroup: %w", err)
	}
	if result.RowsAffected() == 0 {
		granted, err := s.CredentialProfileGrantedToGroups(ctx, profileID, []string{groupID})
		if err != nil {
			return err
		}
		if !granted {
			return fmt.Errorf("user group or credential profile not found")
		}
	}
	return nil
}

// RevokeCredentialProfileFromGroup removes a grant.
func (s *Store) RevokeCredentialProfileFromGroup(ctx context.Context, tenantID, groupID, profileID string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM integration_group_grants gr
		 USING user_groups g
		 WHERE gr.group_id = g.id AND g.tenant_id = $1
		   AND gr.group_id = $2 AND gr.credential_profile_id = $3`,
		tenantID, groupID, profileID,
	)
	if err != nil {
		return fmt.Errorf("RevokeCredentialProfileFromGroup: %w", err)
	}
	return nil
}

// ListGrantsForCredentialProfile returns every group grant on a credential profile.
func (s *Store) ListGrantsForCredentialProfile(ctx context.Context, tenantID, profileID string) ([]*models.IntegrationGroupGrant, error) {
	return s.listGrants(ctx,
		`SELECT gr.group_id, g.name, gr.credential_profile_id, gr.granted_at
		 FROM integration_group_grants gr
		 JOIN user_groups g ON g.id = gr.group_id
		 WHERE g.tenant_id = $1 AND gr.credential_profile_id = $2
		 ORDER BY gr.granted_at ASC`,
		tenantID, profileID)
}

// ListGrantsForGroup returns every credential profile granted to a group.
func (s *Store) ListGrantsForGroup(ctx context.Context, tenantID, groupID string) ([]*models.IntegrationGroupGrant, error) {
	return s.listGrants(ctx,
		`SELECT gr.group_id, g.name, gr.credential_profile_id, gr.granted_at
		 FROM integration_group_grants gr
		 JOIN user_groups g ON g.id = gr.group_id
		 WHERE g.tenant_id = $1 AND gr.group_id = $2
		 ORDER BY gr.granted_at ASC`,
		tenantID, groupID)
}

func (s *Store) listGrants(ctx context.Context, query, tenantID, id string) ([]*models.IntegrationGroupGrant, error) {
	rows, err := s.pool.Query(ctx, query, tenantID, id)
	if err != nil {
		return nil, fmt.Errorf("list integration group grants query: %w", err)
	}
	defer rows.Close()

	grants := []*models.IntegrationGroupGrant{}
	for rows.Next() {
		var g models.IntegrationGroupGrant
		if err := rows.Scan(&g.GroupID, &g.GroupName, &g.CredentialProfileID, &g.GrantedAt); err != nil {
			return nil, fmt.Errorf("list integration group grants scan: %w", err)
		}
		grants = append(grants, &g)
	}
	return grants, rows.Err()
}

// ListGroupIDsForUser returns the IDs of the tenant's groups the user belongs to.
func (s *Store) ListGroupIDsForUser(ctx context.Context, tenantID, userID string) ([]string, error) {
	if userID == "" {
		return []string{}, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT m.group_id
		 FROM user_group_members m
		 JOIN user_groups g ON g.id = m.group_id
		 WHERE g.tenant_id = $1 AND m.user_id = $2
		 ORDER BY m.group_id`,
		tenantID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("ListGroupIDsForUser query: %w", err)
	}
	defer rows.Close()
	return scanIDs(rows, "ListGroupIDsForUser")
}

// ListGroupIDsForAPIKey returns the group IDs an API key inherits from the user
// that created it. Legacy keys without a user return an empty slice.
func (s *Store) ListGroupIDsForAPIKey(ctx context.Context, tenantID, apiKeyID string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT m.group_id
		 FROM api_keys k
		 JOIN user_group_members m ON m.user_id = k.user_id
		 JOIN user_groups g ON g.id = m.group_id AND g.tenant_id = k.tenant_id
		 WHERE k.tenant_id = $1 AND k.id = $2
		 ORDER BY m.group_id`,
		tenantID, apiKeyID,
	)
	if err != nil {
		return nil, fmt.Errorf("ListGroupIDsForAPIKey query: %w", err)
	}
	defer rows.Close()
	return scanIDs(rows, "ListGroupIDsForAPIKey")
}

// ListCredentialProfileIDsForGroups returns the credential profiles granted to
// any of the given groups.
func (s *Store) ListCredentialProfileIDsForGroups(ctx context.Context, tenantID string, groupIDs []string) ([]string, error) {
	if len(groupIDs) == 0 {
		return []string{}, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT gr.credential_profile_id
		 FROM integration_group_grants gr
		 JOIN user_groups g ON g.id = gr.group_id
		 WHERE g.tenant_id = $1 AND gr.group_id = ANY($2)`,
		tenantID, groupIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("ListCredentialProfileIDsForGroups query: %w", err)
	}
	defer rows.Close()
	return scanIDs(rows, "ListCredentialProfileIDsForGroups")
}

// CredentialProfileGrantedToGroups reports whether at least one of the groups
// has been granted the credential profile.
func (s *Store) CredentialProfileGrantedToGroups(ctx context.Context, profileID string, groupIDs []string) (bool, error) {
	if profileID == "" || len(groupIDs) == 0 {
		return false, nil
	}
	var granted bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM integration_group_grants
			WHERE credential_profile_id = $1 AND group_id = ANY($2)
		 )`,
		profileID, groupIDs,
	).Scan(&granted)
	if err != nil {
		return false, fmt.Errorf("CredentialProfileGrantedToGroups: %w", err)
	}
	return granted, nil
}

// CredentialProfileHasGrants reports whether a profile is restricted to groups
// at all. Profiles without any grant stay usable by every member of the tenant.
func (s *Store) CredentialProfileHasGrants(ctx context.Context, profileID string) (bool, error) {
	if profileID == "" {
		return false, nil
	}
	var restricted bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM integration_group_grants WHERE credential_profile_id = $1)`,
		profileID,
	).Scan(&restricted)
	if err != nil {
		return false, fmt.Errorf("CredentialProfileHasGrants: %w", err)
	}
	return restricted, nil
}

// ListRestrictedCredentialProfileIDs returns every profile of the tenant that
// has at least one group grant.
func (s *Store) ListRestrictedCredentialProfileIDs(ctx context.Context, tenantID string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT gr.credential_profile_id
		 FROM integration_group_grants gr
		 JOIN user_groups g ON g.id = gr.group_id
		 WHERE g.tenant_id = $1`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("ListRestrictedCredentialProfileIDs query: %w", err)
	}
	defer rows.Close()
	return scanIDs(rows, "ListRestrictedCredentialProfileIDs")
}

// TenantRBACStrictMode reports whether the tenant denies integration access to
// callers without a resolvable user (legacy tenant-wide API keys).
func (s *Store) TenantRBACStrictMode(ctx context.Context, tenantID string) (bool, error) {
	var strict bool
	if err := s.pool.QueryRow(ctx,
		`SELECT rbac_strict_mode FROM tenants WHERE id = $1`, tenantID,
	).Scan(&strict); err != nil {
		return false, fmt.Errorf("TenantRBACStrictMode: %w", err)
	}
	return strict, nil
}

// SetTenantRBACStrictMode toggles strict mode for a tenant.
func (s *Store) SetTenantRBACStrictMode(ctx context.Context, tenantID string, strict bool) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE tenants SET rbac_strict_mode = $2 WHERE id = $1`, tenantID, strict,
	)
	if err != nil {
		return fmt.Errorf("SetTenantRBACStrictMode: %w", err)
	}
	return nil
}

type rowsScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanIDs(rows rowsScanner, label string) ([]string, error) {
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("%s scan: %w", label, err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func scanUserGroup(row scannable) (*models.UserGroup, error) {
	var g models.UserGroup
	if err := row.Scan(&g.ID, &g.TenantID, &g.Name, &g.Description, &g.CreatedAt, &g.UpdatedAt); err != nil {
		return nil, err
	}
	return &g, nil
}
