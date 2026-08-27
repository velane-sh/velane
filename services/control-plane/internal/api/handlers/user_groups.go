package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/abskrj/velane/services/control-plane/internal/api/middleware"
	"github.com/abskrj/velane/services/control-plane/internal/audit"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// UserGroupsHandler manages tenant user groups and the integration grants
// attached to them.
type UserGroupsHandler struct {
	store   *postgres.Store
	log     *zap.Logger
	auditor *audit.Logger
}

func NewUserGroupsHandler(store *postgres.Store, log *zap.Logger) *UserGroupsHandler {
	return &UserGroupsHandler{store: store, log: log}
}

func (h *UserGroupsHandler) WithAuditor(a *audit.Logger) *UserGroupsHandler {
	h.auditor = a
	return h
}

type userGroupResponse struct {
	*models.UserGroup
	Members []*models.UserGroupMember       `json:"members"`
	Grants  []*models.IntegrationGroupGrant `json:"integration_grants"`
}

// ListGroups handles GET /v1/tenant/groups.
func (h *UserGroupsHandler) ListGroups(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	groups, err := h.store.ListUserGroups(r.Context(), tenant.ID)
	if err != nil {
		h.log.Error("list user groups failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to list user groups")
		return
	}

	out := make([]userGroupResponse, 0, len(groups))
	for _, g := range groups {
		members, err := h.store.ListUserGroupMembers(r.Context(), tenant.ID, g.ID)
		if err != nil {
			h.log.Error("list user group members failed", zap.String("group_id", g.ID), zap.Error(err))
			writeError(w, http.StatusInternalServerError, "failed to list user groups")
			return
		}
		grants, err := h.store.ListGrantsForGroup(r.Context(), tenant.ID, g.ID)
		if err != nil {
			h.log.Error("list group grants failed", zap.String("group_id", g.ID), zap.Error(err))
			writeError(w, http.StatusInternalServerError, "failed to list user groups")
			return
		}
		out = append(out, userGroupResponse{UserGroup: g, Members: members, Grants: grants})
	}

	writeJSON(w, http.StatusOK, out)
}

// CreateGroup handles POST /v1/tenant/groups.
func (h *UserGroupsHandler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	group, err := h.store.CreateUserGroup(r.Context(), tenant.ID, req.Name, strings.TrimSpace(req.Description))
	if err != nil {
		h.log.Error("create user group failed", zap.Error(err))
		writeError(w, http.StatusBadRequest, "failed to create user group")
		return
	}

	h.audit(r, tenant.ID, "user_group_create", group.ID, map[string]any{"name": group.Name})
	writeJSON(w, http.StatusCreated, userGroupResponse{
		UserGroup: group,
		Members:   []*models.UserGroupMember{},
		Grants:    []*models.IntegrationGroupGrant{},
	})
}

// DeleteGroup handles DELETE /v1/tenant/groups/{groupID}.
func (h *UserGroupsHandler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	groupID := chi.URLParam(r, "groupID")
	if err := h.store.DeleteUserGroup(r.Context(), tenant.ID, groupID); err != nil {
		writeError(w, http.StatusNotFound, "user group not found")
		return
	}

	h.audit(r, tenant.ID, "user_group_delete", groupID, nil)
	w.WriteHeader(http.StatusNoContent)
}

// AddMember handles POST /v1/tenant/groups/{groupID}/members.
func (h *UserGroupsHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	groupID := chi.URLParam(r, "groupID")
	userID, ok := h.resolveUserID(w, r)
	if !ok {
		return
	}

	if err := h.store.AddUserGroupMember(r.Context(), tenant.ID, groupID, userID); err != nil {
		writeError(w, http.StatusNotFound, "user group not found")
		return
	}

	h.audit(r, tenant.ID, "user_group_member_add", groupID, map[string]any{"user_id": userID})
	writeJSON(w, http.StatusCreated, map[string]string{"group_id": groupID, "user_id": userID})
}

// RemoveMember handles DELETE /v1/tenant/groups/{groupID}/members.
func (h *UserGroupsHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	groupID := chi.URLParam(r, "groupID")
	userID, ok := h.resolveUserID(w, r)
	if !ok {
		return
	}

	if err := h.store.RemoveUserGroupMember(r.Context(), tenant.ID, groupID, userID); err != nil {
		h.log.Error("remove user group member failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to remove group member")
		return
	}

	h.audit(r, tenant.ID, "user_group_member_remove", groupID, map[string]any{"user_id": userID})
	w.WriteHeader(http.StatusNoContent)
}

// GrantIntegration handles POST /v1/tenant/groups/{groupID}/integrations.
func (h *UserGroupsHandler) GrantIntegration(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	groupID := chi.URLParam(r, "groupID")
	profileID, ok := h.decodeProfileID(w, r)
	if !ok {
		return
	}

	if err := h.store.GrantCredentialProfileToGroup(r.Context(), tenant.ID, groupID, profileID); err != nil {
		writeError(w, http.StatusNotFound, "user group or credential profile not found")
		return
	}

	h.audit(r, tenant.ID, "user_group_integration_grant", groupID, map[string]any{"credential_profile_id": profileID})
	writeJSON(w, http.StatusCreated, map[string]string{"group_id": groupID, "credential_profile_id": profileID})
}

// RevokeIntegration handles DELETE /v1/tenant/groups/{groupID}/integrations.
func (h *UserGroupsHandler) RevokeIntegration(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	groupID := chi.URLParam(r, "groupID")
	profileID, ok := h.decodeProfileID(w, r)
	if !ok {
		return
	}

	if err := h.store.RevokeCredentialProfileFromGroup(r.Context(), tenant.ID, groupID, profileID); err != nil {
		h.log.Error("revoke integration grant failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to revoke integration grant")
		return
	}

	h.audit(r, tenant.ID, "user_group_integration_revoke", groupID, map[string]any{"credential_profile_id": profileID})
	w.WriteHeader(http.StatusNoContent)
}

// ListIntegrationGrants handles GET /v1/integrations/configured/{profileID}/groups.
func (h *UserGroupsHandler) ListIntegrationGrants(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	grants, err := h.store.ListGrantsForCredentialProfile(r.Context(), tenant.ID, chi.URLParam(r, "profileID"))
	if err != nil {
		h.log.Error("list profile grants failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to list integration grants")
		return
	}
	writeJSON(w, http.StatusOK, grants)
}

// resolveUserID accepts either a user_id or an email and resolves it to a user
// that belongs to the authenticated tenant.
func (h *UserGroupsHandler) resolveUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	tenant := middleware.TenantFromContext(r.Context())

	var req struct {
		UserID string `json:"user_id"`
		Email  string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return "", false
	}
	req.UserID = strings.TrimSpace(req.UserID)
	req.Email = strings.TrimSpace(req.Email)
	if req.UserID == "" && req.Email == "" {
		writeError(w, http.StatusBadRequest, "user_id or email is required")
		return "", false
	}

	if req.UserID == "" {
		user, err := h.store.GetUserByEmail(r.Context(), req.Email)
		if err != nil {
			writeError(w, http.StatusNotFound, "user not found")
			return "", false
		}
		req.UserID = user.ID
	}

	// Group membership must never span tenants.
	if _, err := h.store.GetMemberRole(r.Context(), tenant.ID, req.UserID); err != nil {
		writeError(w, http.StatusNotFound, "user is not a member of this tenant")
		return "", false
	}
	return req.UserID, true
}

func (h *UserGroupsHandler) decodeProfileID(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req struct {
		CredentialProfileID string `json:"credential_profile_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return "", false
	}
	req.CredentialProfileID = strings.TrimSpace(req.CredentialProfileID)
	if req.CredentialProfileID == "" {
		writeError(w, http.StatusBadRequest, "credential_profile_id is required")
		return "", false
	}
	return req.CredentialProfileID, true
}

func (h *UserGroupsHandler) audit(r *http.Request, tenantID, action, resourceID string, meta map[string]any) {
	if h.auditor == nil {
		return
	}
	actorID, actorType := resolveActor(r)
	h.auditor.Log(r.Context(), models.AuditEntry{
		TenantID:   tenantID,
		ActorID:    actorID,
		ActorType:  actorType,
		Action:     action,
		ResourceID: resourceID,
		Metadata:   auditMeta(meta),
	})
}
