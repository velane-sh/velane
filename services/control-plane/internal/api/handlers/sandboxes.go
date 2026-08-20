package handlers

import (
	"encoding/json"
	"errors"
	"github.com/abskrj/velane/services/control-plane/internal/api/middleware"
	"github.com/abskrj/velane/services/control-plane/internal/sandboxcontrol"
	"github.com/abskrj/velane/services/control-plane/internal/sandboxcontrol/domain"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type SandboxesHandler struct {
	service      *sandboxcontrol.Service
	capabilities sandboxcontrol.Provider
	log          *zap.Logger
}

func NewSandboxesHandler(s *sandboxcontrol.Service, capabilities sandboxcontrol.Provider, l *zap.Logger) *SandboxesHandler {
	return &SandboxesHandler{service: s, capabilities: capabilities, log: l}
}
func (h *SandboxesHandler) List(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, 401, "unauthorized")
		return
	}
	limit, offset := pageQuery(r)
	items, total, err := h.service.ListSandboxes(r.Context(), tenant.ID, limit, offset)
	if err != nil {
		writeError(w, 500, "unable to list sandboxes")
		return
	}
	writeJSON(w, 200, map[string]any{"items": items, "total": total})
}
func (h *SandboxesHandler) Create(w http.ResponseWriter, r *http.Request) {
	if !h.requireCapability(w, r, sandboxcontrol.CapabilitySandboxCreate) {
		return
	}
	var req sandboxcontrol.CreateSandboxRequest
	if !decodeSandboxJSON(w, r, &req) {
		return
	}
	h.mutate(w, r, "", domain.OperationCreate, func(t, a, at, k string, g *int64) (*sandboxcontrol.MutationResult, error) {
		return h.service.CreateSandbox(r.Context(), t, a, at, k, req)
	})
}
func (h *SandboxesHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, 401, "unauthorized")
		return
	}
	sb, err := h.service.GetSandbox(r.Context(), tenant.ID, chi.URLParam(r, "sandboxID"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, 404, "sandbox not found")
		return
	}
	if err != nil {
		writeError(w, 500, "unable to load sandbox")
		return
	}
	writeJSON(w, 200, map[string]any{"sandbox": sb, "available_actions": sb.AvailableActions})
}
func (h *SandboxesHandler) Start(w http.ResponseWriter, r *http.Request) {
	if !h.requireCapability(w, r, sandboxcontrol.CapabilitySandboxStart) {
		return
	}
	h.lifecycle(w, r, domain.OperationStart)
}
func (h *SandboxesHandler) Stop(w http.ResponseWriter, r *http.Request) {
	if !h.requireCapability(w, r, sandboxcontrol.CapabilitySandboxCheckpoint) {
		return
	}
	h.lifecycle(w, r, domain.OperationStop)
}
func (h *SandboxesHandler) Restart(w http.ResponseWriter, r *http.Request) {
	if !h.requireCapability(w, r, sandboxcontrol.CapabilitySandboxCheckpoint) {
		return
	}
	h.lifecycle(w, r, domain.OperationRestart)
}
func (h *SandboxesHandler) Snapshot(w http.ResponseWriter, r *http.Request) {
	if !h.requireCapability(w, r, sandboxcontrol.CapabilitySandboxCheckpoint) {
		return
	}
	h.lifecycle(w, r, domain.OperationSnapshot)
}
func (h *SandboxesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	// Reject before parsing. The service repeats the action-specific check after
	// parsing delete_snapshots, so a future non-HTTP caller also fails closed.
	if !h.requireCapability(w, r, sandboxcontrol.CapabilitySandboxDelete) {
		return
	}
	var request struct {
		DeleteSnapshots bool `json:"delete_snapshots"`
	}
	if !decodeSandboxJSON(w, r, &request) {
		return
	}
	id := chi.URLParam(r, "sandboxID")
	h.mutate(w, r, id, domain.OperationDelete, func(t, a, at, k string, g *int64) (*sandboxcontrol.MutationResult, error) {
		return h.service.DeleteSandbox(r.Context(), t, a, at, k, id, request.DeleteSnapshots, g)
	})
}
func (h *SandboxesHandler) Retry(w http.ResponseWriter, r *http.Request) {
	if !h.requireCapability(w, r, sandboxcontrol.CapabilitySandboxOperationRetry) {
		return
	}
	var request struct {
		OperationID string `json:"operation_id"`
	}
	if !decodeSandboxJSON(w, r, &request) {
		return
	}
	id := chi.URLParam(r, "sandboxID")
	h.mutate(w, r, id, "", func(t, a, at, k string, g *int64) (*sandboxcontrol.MutationResult, error) {
		return h.service.RetrySandboxOperation(r.Context(), t, a, at, k, id, request.OperationID, g)
	})
}
func (h *SandboxesHandler) ListSnapshots(w http.ResponseWriter, r *http.Request) {
	tenant := sandboxTenant(w, r)
	if tenant == "" {
		return
	}
	if _, err := h.service.GetSandbox(r.Context(), tenant, chi.URLParam(r, "sandboxID")); err != nil {
		writeSandboxReadError(w, err, "unable to load sandbox")
		return
	}
	limit, offset := pageQuery(r)
	items, total, err := h.service.Snapshots(r.Context(), tenant, chi.URLParam(r, "sandboxID"), limit, offset)
	if err != nil {
		writeSandboxReadError(w, err, "unable to list snapshots")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}
func (h *SandboxesHandler) GetSnapshot(w http.ResponseWriter, r *http.Request) {
	tenant := sandboxTenant(w, r)
	if tenant == "" {
		return
	}
	item, err := h.service.Snapshot(r.Context(), tenant, chi.URLParam(r, "sandboxID"), chi.URLParam(r, "snapshotID"))
	if err != nil {
		writeSandboxReadError(w, err, "unable to load snapshot")
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func (h *SandboxesHandler) RestoreSnapshot(w http.ResponseWriter, r *http.Request) {
	if !h.requireCapability(w, r, sandboxcontrol.CapabilitySandboxCheckpoint) {
		return
	}
	snapshotID := chi.URLParam(r, "snapshotID")
	h.mutate(w, r, chi.URLParam(r, "sandboxID"), domain.OperationRestore, func(t, a, at, k string, g *int64) (*sandboxcontrol.MutationResult, error) {
		return h.service.RequestSnapshotOperation(r.Context(), t, a, at, k, chi.URLParam(r, "sandboxID"), snapshotID, domain.OperationRestore, g)
	})
}
func (h *SandboxesHandler) DeleteSnapshot(w http.ResponseWriter, r *http.Request) {
	if !h.requireCapability(w, r, sandboxcontrol.CapabilitySandboxCheckpoint) {
		return
	}
	snapshotID := chi.URLParam(r, "snapshotID")
	h.mutate(w, r, chi.URLParam(r, "sandboxID"), domain.OperationSnapshotDelete, func(t, a, at, k string, g *int64) (*sandboxcontrol.MutationResult, error) {
		return h.service.RequestSnapshotOperation(r.Context(), t, a, at, k, chi.URLParam(r, "sandboxID"), snapshotID, domain.OperationSnapshotDelete, g)
	})
}
func (h *SandboxesHandler) GetOperation(w http.ResponseWriter, r *http.Request) {
	tenant := sandboxTenant(w, r)
	if tenant == "" {
		return
	}
	item, err := h.service.Operation(r.Context(), tenant, chi.URLParam(r, "operationID"))
	if err != nil {
		writeSandboxReadError(w, err, "unable to load operation")
		return
	}
	if item.State == "queued" || item.State == "claimed" || item.State == "dispatched" || item.State == "waiting" {
		w.Header().Set("Retry-After", "1")
	}
	writeJSON(w, http.StatusOK, item)
}
func (h *SandboxesHandler) Profiles(w http.ResponseWriter, r *http.Request) {
	items, err := h.service.Profiles(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "unable to list sandbox profiles")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}
func (h *SandboxesHandler) Events(w http.ResponseWriter, r *http.Request) { h.cursor(w, r, true) }
func (h *SandboxesHandler) Logs(w http.ResponseWriter, r *http.Request)   { h.cursor(w, r, false) }
func (h *SandboxesHandler) cursor(w http.ResponseWriter, r *http.Request, events bool) {
	tenant := sandboxTenant(w, r)
	if tenant == "" {
		return
	}
	if _, err := h.service.GetSandbox(r.Context(), tenant, chi.URLParam(r, "sandboxID")); err != nil {
		writeSandboxReadError(w, err, "unable to load sandbox")
		return
	}
	limit, _ := pageQuery(r)
	if _, ok := cursorQuery(w, r); !ok {
		return
	}
	var items any
	var next string
	var err error
	if events {
		items, next, err = h.service.Events(r.Context(), tenant, chi.URLParam(r, "sandboxID"), r.URL.Query().Get("after"), limit)
	} else {
		items, next, err = h.service.Logs(r.Context(), tenant, chi.URLParam(r, "sandboxID"), r.URL.Query().Get("after"), limit)
	}
	if err != nil {
		writeSandboxReadError(w, err, "unable to list sandbox activity")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
}
func (h *SandboxesHandler) lifecycle(w http.ResponseWriter, r *http.Request, kind domain.OperationKind) {
	id := chi.URLParam(r, "sandboxID")
	h.mutate(w, r, id, kind, func(t, a, at, k string, g *int64) (*sandboxcontrol.MutationResult, error) {
		return h.service.RequestOperation(r.Context(), t, a, at, k, id, kind, g)
	})
}
func (h *SandboxesHandler) requireCapability(w http.ResponseWriter, r *http.Request, capability sandboxcontrol.Capability) bool {
	if h.capabilities != nil && h.capabilities.Available(r.Context(), capability) {
		return true
	}
	writeSandboxCapabilityUnavailable(w)
	return false
}
func (h *SandboxesHandler) mutate(w http.ResponseWriter, r *http.Request, _ string, _ domain.OperationKind, fn func(string, string, string, string, *int64) (*sandboxcontrol.MutationResult, error)) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, 401, "unauthorized")
		return
	}
	key := r.Header.Get("Idempotency-Key")
	actor, atype := resolveActor(r)
	var generation *int64
	if raw := r.Header.Get("If-Match"); raw != "" {
		n, err := strconv.ParseInt(strings.Trim(raw, "\""), 10, 64)
		if err != nil {
			writeError(w, 400, "invalid If-Match header")
			return
		}
		generation = &n
	}
	out, err := fn(tenant.ID, actor, atype, key, generation)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, out)
}
func decodeSandboxJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		writeError(w, 400, "invalid JSON body")
		return false
	}
	if err := d.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, 400, "only one JSON document is allowed")
		return false
	}
	return true
}
func sandboxTenant(w http.ResponseWriter, r *http.Request) string {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return ""
	}
	return tenant.ID
}
func pageQuery(r *http.Request) (int, int) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	return limit, offset
}
func cursorQuery(w http.ResponseWriter, r *http.Request) (string, bool) {
	after := r.URL.Query().Get("after")
	if after == "" {
		return "", true
	}
	if _, err := strconv.ParseInt(after, 10, 64); err != nil {
		writeError(w, http.StatusBadRequest, "after must be a cursor")
		return "", false
	}
	return after, true
}
func writeSandboxReadError(w http.ResponseWriter, err error, fallback string) {
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "sandbox resource not found")
		return
	}
	writeError(w, http.StatusInternalServerError, fallback)
}
func writeSandboxError(w http.ResponseWriter, err error) {
	if e, ok := err.(*domain.Error); ok {
		if e.Code == domain.CapabilityUnavailable {
			writeSandboxCapabilityUnavailable(w)
			return
		}
		status := 409
		if e.Code == domain.GenerationConflict {
			status = 412
		}
		if e.Code == domain.SandboxNotFound {
			status = 404
		}
		writeJSON(w, status, map[string]any{"error": e.Message, "code": e.Code, "retryable": e.Retryable, "field_errors": []any{}})
		return
	}
	writeError(w, 422, err.Error())
}

func writeSandboxCapabilityUnavailable(w http.ResponseWriter) {
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{
		"error":        "sandbox capability is not configured",
		"code":         "SANDBOX_CAPABILITY_UNAVAILABLE",
		"retryable":    false,
		"field_errors": []any{},
	})
}
