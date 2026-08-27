package models

import "time"

// Tenant member roles. The legacy invoke/manage/admin values keep working and
// the RBAC roles below are layered on top of them.
const (
	RoleOwner              = "owner"
	RoleAdmin              = "admin"
	RoleIntegrationManager = "integration_manager"
	RoleManage             = "manage"
	RoleMember             = "member"
	RoleInvoke             = "invoke"
	RoleViewer             = "viewer"
)

// ValidRoles lists every role accepted on invites and membership rows.
var ValidRoles = []string{RoleInvoke, RoleManage, RoleAdmin, RoleOwner, RoleIntegrationManager, RoleMember, RoleViewer}

// IsValidRole reports whether role is one of ValidRoles.
func IsValidRole(role string) bool {
	for _, r := range ValidRoles {
		if r == role {
			return true
		}
	}
	return false
}

// RoleGrantsScope maps a tenant member role onto the API key scope model.
// owner/admin grant every scope, integration_manager mirrors manage, member
// mirrors invoke, and viewer is read-only so it grants none.
func RoleGrantsScope(role, scope string) bool {
	switch role {
	case RoleAdmin, RoleOwner:
		return true
	case RoleManage, RoleIntegrationManager:
		return scope == RoleInvoke || scope == RoleManage
	case RoleInvoke, RoleMember:
		return scope == RoleInvoke
	}
	return false
}

// RoleSeesAllIntegrations reports whether a role bypasses group-based
// integration filtering and enforcement.
func RoleSeesAllIntegrations(role string) bool {
	return role == RoleOwner || role == RoleAdmin
}

// UserGroup is a tenant-scoped collection of users used to grant integration access.
type UserGroup struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// UserGroupMember links a user to a group.
type UserGroupMember struct {
	GroupID string    `json:"group_id"`
	UserID  string    `json:"user_id"`
	Email   string    `json:"email,omitempty"`
	AddedAt time.Time `json:"added_at"`
}

// IntegrationGroupGrant grants a group access to one integration credential profile.
type IntegrationGroupGrant struct {
	GroupID             string    `json:"group_id"`
	GroupName           string    `json:"group_name,omitempty"`
	CredentialProfileID string    `json:"credential_profile_id"`
	GrantedAt           time.Time `json:"granted_at"`
}

// CallerIdentity is the resolved principal behind a request or an invocation:
// the tenant it acts in, the user it belongs to (empty for legacy tenant-wide
// API keys), its role and the groups it inherits integration access from.
type CallerIdentity struct {
	TenantID string   `json:"tenant_id"`
	UserID   string   `json:"user_id,omitempty"`
	Role     string   `json:"role,omitempty"`
	GroupIDs []string `json:"group_ids,omitempty"`
}

// SeesAllIntegrations reports whether the caller bypasses group filtering.
func (c CallerIdentity) SeesAllIntegrations() bool { return RoleSeesAllIntegrations(c.Role) }

// HasGroup reports whether the caller belongs to the given group.
func (c CallerIdentity) HasGroup(groupID string) bool {
	for _, g := range c.GroupIDs {
		if g == groupID {
			return true
		}
	}
	return false
}
