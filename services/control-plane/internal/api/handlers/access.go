package handlers

import (
	"context"
	"net/http"

	"github.com/abskrj/velane/services/control-plane/internal/api/middleware"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
)

// CallerAccess is the integration visibility of the principal behind a request.
//
// A credential profile that has no group grant at all stays visible to the whole
// tenant, so enabling RBAC does not retroactively hide existing integrations.
// Once a profile is granted to at least one group it becomes restricted and only
// admins/owners and members of a granted group may see or use it.
type CallerAccess struct {
	Identity models.CallerIdentity
	// SeesAll short-circuits every check (admin/owner, or a legacy tenant-wide
	// API key while the tenant has not enabled strict mode).
	SeesAll bool

	granted    map[string]bool
	restricted map[string]bool
}

// CanUseProfile reports whether the caller may see or use a credential profile.
func (a *CallerAccess) CanUseProfile(profileID string) bool {
	if a == nil || a.SeesAll {
		return true
	}
	if profileID == "" {
		// Connections predating credential profiles are not group-restricted.
		return true
	}
	if !a.restricted[profileID] {
		return true
	}
	return a.granted[profileID]
}

// ResolveCallerAccess derives the caller's identity and integration visibility
// from the request context, which SessionAuth and Auth have already populated.
func ResolveCallerAccess(ctx context.Context, store *postgres.Store, tenantID string) (*CallerAccess, error) {
	identity := models.CallerIdentity{TenantID: tenantID, Role: middleware.CallerRole(ctx)}

	if user := middleware.SessionUserFromContext(ctx); user != nil {
		identity.UserID = user.ID
	} else if key := middleware.APIKeyFromContext(ctx); key != nil && key.UserID != nil {
		identity.UserID = *key.UserID
	}

	return resolveAccessForIdentity(ctx, store, identity)
}

func resolveAccessForIdentity(ctx context.Context, store *postgres.Store, identity models.CallerIdentity) (*CallerAccess, error) {
	access := &CallerAccess{Identity: identity, granted: map[string]bool{}, restricted: map[string]bool{}}

	if identity.SeesAllIntegrations() {
		access.SeesAll = true
		return access, nil
	}

	if identity.UserID == "" {
		// Legacy tenant-wide credential with no owning user. Backward compatible
		// by default; tenants that opt into strict mode lose access to every
		// group-restricted profile instead.
		strict, err := store.TenantRBACStrictMode(ctx, identity.TenantID)
		if err != nil {
			return nil, err
		}
		if !strict {
			access.SeesAll = true
			return access, nil
		}
	}

	if identity.UserID != "" && len(identity.GroupIDs) == 0 {
		groupIDs, err := store.ListGroupIDsForUser(ctx, identity.TenantID, identity.UserID)
		if err != nil {
			return nil, err
		}
		identity.GroupIDs = groupIDs
		access.Identity = identity
	}

	restricted, err := store.ListRestrictedCredentialProfileIDs(ctx, identity.TenantID)
	if err != nil {
		return nil, err
	}
	for _, id := range restricted {
		access.restricted[id] = true
	}

	granted, err := store.ListCredentialProfileIDsForGroups(ctx, identity.TenantID, identity.GroupIDs)
	if err != nil {
		return nil, err
	}
	for _, id := range granted {
		access.granted[id] = true
	}
	return access, nil
}

// apiKeyRole maps an API key's scopes back onto the equivalent tenant role so
// that admin-scoped keys keep seeing every integration.
func apiKeyRole(key *models.APIKey) string {
	switch {
	case key == nil:
		return ""
	case key.HasScope(models.RoleAdmin):
		return models.RoleAdmin
	case key.HasScope(models.RoleManage):
		return models.RoleManage
	case key.HasScope(models.RoleInvoke):
		return models.RoleInvoke
	}
	return ""
}

// callerAccessOrError resolves access and writes a 500 when resolution fails.
func callerAccessOrError(w http.ResponseWriter, r *http.Request, store *postgres.Store, tenantID string) (*CallerAccess, bool) {
	access, err := ResolveCallerAccess(r.Context(), store, tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to resolve integration access")
		return nil, false
	}
	return access, true
}
