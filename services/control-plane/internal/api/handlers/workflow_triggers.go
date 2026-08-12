package handlers

import (
	"encoding/json"
	"github.com/abskrj/velane/services/control-plane/internal/api/middleware"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/abskrj/velane/services/control-plane/internal/nango"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"net/http"
	"regexp"
	"sort"
)

type WorkflowTriggersHandler struct {
	store *postgres.Store
	nango *nango.Client
	log   *zap.Logger
}

func NewWorkflowTriggersHandler(s *postgres.Store, n *nango.Client, l *zap.Logger) *WorkflowTriggersHandler {
	return &WorkflowTriggersHandler{s, n, l}
}

var triggerModelRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,127}$`)

type triggerRequest struct {
	ConnectionID string   `json:"connection_id"`
	Model        string   `json:"model"`
	ChangeTypes  []string `json:"change_types"`
	Environment  string   `json:"environment"`
	Enabled      bool     `json:"enabled"`
}

func normalizeTriggerRequest(q *triggerRequest) bool {
	if len(q.ChangeTypes) == 0 {
		q.ChangeTypes = []string{"added", "updated", "deleted"}
	}
	sort.Strings(q.ChangeTypes)
	allowed := map[string]bool{"added": true, "updated": true, "deleted": true}
	seen := map[string]bool{}
	for _, v := range q.ChangeTypes {
		if !allowed[v] || seen[v] {
			return false
		}
		seen[v] = true
	}
	return triggerModelRE.MatchString(q.Model) && (q.Environment == "dev" || q.Environment == "staging" || q.Environment == "prod")
}
func (h *WorkflowTriggersHandler) owned(r *http.Request) (string, bool) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		return "", false
	}
	id := chi.URLParam(r, "snippetID")
	sn, e := h.store.GetSnippetByID(r.Context(), id)
	return tenant.ID, e == nil && sn.TenantID == tenant.ID
}
func (h *WorkflowTriggersHandler) List(w http.ResponseWriter, r *http.Request) {
	tenant, ok := h.owned(r)
	if !ok {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	v, e := h.store.ListWorkflowTriggers(r.Context(), tenant, chi.URLParam(r, "snippetID"))
	if e != nil {
		writeError(w, 500, "failed to list triggers")
		return
	}
	writeJSON(w, 200, v)
}
func (h *WorkflowTriggersHandler) Create(w http.ResponseWriter, r *http.Request) {
	tenant, ok := h.owned(r)
	if !ok {
		writeError(w, 403, "access denied")
		return
	}
	var q triggerRequest
	if json.NewDecoder(r.Body).Decode(&q) != nil || !normalizeTriggerRequest(&q) {
		writeError(w, 400, "invalid trigger configuration")
		return
	}
	if q.Enabled {
		writeError(w, 400, "triggers must be created disabled")
		return
	}
	conn, e := h.store.GetConnectionByID(r.Context(), tenant, q.ConnectionID)
	if e != nil {
		writeError(w, 400, "connection not found")
		return
	}
	t, e := h.store.CreateWorkflowTrigger(r.Context(), &models.WorkflowTrigger{TenantID: tenant, WorkflowID: chi.URLParam(r, "snippetID"), ConnectionID: conn.ID, ProviderConfigKey: conn.ProviderConfigKey, Model: q.Model, ChangeTypes: q.ChangeTypes, Environment: q.Environment})
	if e != nil {
		h.log.Error("create trigger", zap.Error(e))
		writeError(w, 400, "failed to create trigger")
		return
	}
	writeJSON(w, 201, t)
}
func (h *WorkflowTriggersHandler) Update(w http.ResponseWriter, r *http.Request) {
	tenant, ok := h.owned(r)
	if !ok {
		writeError(w, 403, "access denied")
		return
	}
	var q triggerRequest
	if json.NewDecoder(r.Body).Decode(&q) != nil || !normalizeTriggerRequest(&q) {
		writeError(w, 400, "invalid trigger configuration")
		return
	}
	t, e := h.store.UpdateWorkflowTrigger(r.Context(), tenant, chi.URLParam(r, "snippetID"), chi.URLParam(r, "triggerID"), q.Model, q.ChangeTypes, q.Environment, q.Enabled)
	if e != nil {
		writeError(w, 400, "trigger not found or selected environment has no published version")
		return
	}
	writeJSON(w, 200, t)
}
func (h *WorkflowTriggersHandler) Delete(w http.ResponseWriter, r *http.Request) {
	tenant, ok := h.owned(r)
	if !ok {
		writeError(w, 403, "access denied")
		return
	}
	if e := h.store.DeleteWorkflowTrigger(r.Context(), tenant, chi.URLParam(r, "snippetID"), chi.URLParam(r, "triggerID")); e != nil {
		writeError(w, 404, "trigger not found")
		return
	}
	w.WriteHeader(204)
}
func (h *WorkflowTriggersHandler) Models(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, 401, "unauthenticated")
		return
	}
	conn, e := h.store.GetConnectionByID(r.Context(), tenant.ID, chi.URLParam(r, "connectionID"))
	if e != nil {
		writeError(w, 403, "access denied")
		return
	}
	if h.nango == nil {
		writeJSON(w, http.StatusOK, map[string]any{"models": []string{}, "manual_entry": true})
		return
	}
	models, e := h.nango.ListSyncModels(r.Context(), conn.ProviderConfigKey)
	if e != nil {
		writeJSON(w, 200, map[string]any{"models": []string{}, "manual_entry": true})
		return
	}
	writeJSON(w, 200, map[string]any{"models": models, "manual_entry": false})
}
