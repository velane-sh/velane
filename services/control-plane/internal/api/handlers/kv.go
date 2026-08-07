package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/abskrj/velane/services/control-plane/internal/api/middleware"
	"github.com/abskrj/velane/services/control-plane/internal/audit"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
	"go.uber.org/zap"
)

var errKVBadRequest = errors.New("invalid KV request")

// KVHandler bundles the public and trust-header KV HTTP handlers.
type KVHandler struct {
	store   *postgres.Store
	log     *zap.Logger
	auditor *audit.Logger
}

// NewKVHandler constructs a KVHandler.
func NewKVHandler(store *postgres.Store, log *zap.Logger) *KVHandler {
	return &KVHandler{store: store, log: log}
}

// WithAuditor attaches an audit logger to the KV handler.
func (h *KVHandler) WithAuditor(a *audit.Logger) *KVHandler {
	h.auditor = a
	return h
}

type setKVRequest struct {
	Value      json.RawMessage `json:"value"`
	TTLSeconds *int64          `json:"ttl_seconds,omitempty"`
}

type revealKVRequest struct {
	Namespace string `json:"namespace,omitempty"`
	Key       string `json:"key"`
}

// ListKV handles GET /v1/kv/entries.
func (h *KVHandler) ListKV(w http.ResponseWriter, r *http.Request) {
	tenant, ok := kvPublicTenant(w, r)
	if !ok {
		return
	}
	filters, err := kvListFiltersFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.listKV(w, r, tenant.ID, filters)
}

// GetKV handles GET /v1/kv/entry.
func (h *KVHandler) GetKV(w http.ResponseWriter, r *http.Request) {
	tenant, ok := kvPublicTenant(w, r)
	if !ok {
		return
	}
	h.getKV(w, r, tenant.ID)
}

// SetKV handles PUT /v1/kv/entry.
func (h *KVHandler) SetKV(w http.ResponseWriter, r *http.Request) {
	tenant, ok := kvPublicTenant(w, r)
	if !ok {
		return
	}
	h.setKV(w, r, tenant.ID, true)
}

// DeleteKV handles DELETE /v1/kv/entry.
func (h *KVHandler) DeleteKV(w http.ResponseWriter, r *http.Request) {
	tenant, ok := kvPublicTenant(w, r)
	if !ok {
		return
	}
	h.deleteKV(w, r, tenant.ID, true)
}

// ListNamespaces handles GET /v1/kv/namespaces.
func (h *KVHandler) ListNamespaces(w http.ResponseWriter, r *http.Request) {
	tenant, ok := kvPublicTenant(w, r)
	if !ok {
		return
	}
	namespaces, err := h.store.ListKVNamespaces(r.Context(), tenant.ID)
	if err != nil {
		writeKVStoreError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, namespaces)
}

// RevealKV handles POST /v1/kv/reveal.
func (h *KVHandler) RevealKV(w http.ResponseWriter, r *http.Request) {
	tenant, ok := kvPublicTenant(w, r)
	if !ok {
		return
	}
	req, err := decodeKVRevealRequest(w, r)
	if err != nil {
		writeKVStoreError(w, h.log, err)
		return
	}
	entry, err := h.store.GetKV(r.Context(), tenant.ID, req.Namespace, req.Key)
	if err != nil {
		writeKVStoreError(w, h.log, err)
		return
	}
	h.audit(r, tenant.ID, "kv_reveal", entry.ID, map[string]any{
		"namespace": entry.Namespace,
		"key":       entry.Key,
	})
	writeJSON(w, http.StatusOK, entry)
}

// InternalListKV handles GET /v1/internal/kv/entries.
func (h *KVHandler) InternalListKV(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := kvInternalTenant(w, r)
	if !ok {
		return
	}
	filters, err := kvListFiltersFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.listKV(w, r, tenantID, filters)
}

// InternalKV handles GET, PUT, and DELETE /v1/internal/kv/entry.
func (h *KVHandler) InternalKV(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := kvInternalTenant(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.getKV(w, r, tenantID)
	case http.MethodPut:
		h.setKV(w, r, tenantID, false)
	case http.MethodDelete:
		h.deleteKV(w, r, tenantID, false)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (h *KVHandler) listKV(w http.ResponseWriter, r *http.Request, tenantID string, filters postgres.KVListFilters) {
	entries, total, err := h.store.ListKV(r.Context(), tenantID, filters)
	if err != nil {
		writeKVStoreError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": entries, "total": total})
}

func (h *KVHandler) getKV(w http.ResponseWriter, r *http.Request, tenantID string) {
	namespace, key, err := kvEntryIdentityFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	entry, err := h.store.GetKV(r.Context(), tenantID, namespace, key)
	if err != nil {
		writeKVStoreError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, entry)
}

func (h *KVHandler) setKV(w http.ResponseWriter, r *http.Request, tenantID string, public bool) {
	namespace, key, err := kvEntryIdentityFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	limits, err := h.store.KVLimitsForTenant(r.Context(), tenantID)
	if err != nil {
		writeKVStoreError(w, h.log, err)
		return
	}
	req, err := decodeKVSetRequest(w, r, limits.MaxValueBytes)
	if err != nil {
		writeKVStoreError(w, h.log, err)
		return
	}
	entry, err := h.store.SetKV(r.Context(), tenantID, postgres.KVSetInput{
		Namespace:  namespace,
		Key:        key,
		Value:      req.Value,
		TTLSeconds: req.TTLSeconds,
	})
	if err != nil {
		writeKVStoreError(w, h.log, err)
		return
	}
	if public {
		h.audit(r, tenantID, "kv_set", entry.ID, map[string]any{
			"namespace":   entry.Namespace,
			"key":         entry.Key,
			"size_bytes":  entry.SizeBytes,
			"ttl_seconds": req.TTLSeconds,
		})
	} else {
		h.log.Debug("internal kv entry set", zap.String("tenant_id", tenantID), zap.String("namespace", entry.Namespace), zap.String("key", entry.Key))
	}
	writeJSON(w, http.StatusOK, entry)
}

func (h *KVHandler) deleteKV(w http.ResponseWriter, r *http.Request, tenantID string, public bool) {
	namespace, key, err := kvEntryIdentityFromRequest(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := h.store.DeleteKV(r.Context(), tenantID, namespace, key)
	if err != nil {
		writeKVStoreError(w, h.log, err)
		return
	}
	if public {
		h.audit(r, tenantID, "kv_delete", id, map[string]any{"namespace": namespace, "key": key})
	} else {
		h.log.Debug("internal kv entry deleted", zap.String("tenant_id", tenantID), zap.String("namespace", namespace), zap.String("key", key))
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *KVHandler) audit(r *http.Request, tenantID, action, resourceID string, metadata map[string]any) {
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
		Metadata:   auditMeta(metadata),
	})
}

func kvPublicTenant(w http.ResponseWriter, r *http.Request) (*models.Tenant, bool) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return nil, false
	}
	return tenant, true
}

func kvInternalTenant(w http.ResponseWriter, r *http.Request) (string, bool) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Velane-Tenant"))
	if tenantID == "" {
		writeError(w, http.StatusBadRequest, "X-Velane-Tenant header required")
		return "", false
	}
	return tenantID, true
}

func kvEntryNamespaceFromRequest(r *http.Request) (string, error) {
	namespace := models.NormalizeKVNamespace(r.URL.Query().Get("namespace"))
	if err := models.ValidateKVNamespace(namespace); err != nil {
		return "", err
	}
	return namespace, nil
}

func kvKeyFromRequest(r *http.Request) (string, error) {
	key := r.URL.Query().Get("key")
	if key == "" {
		return "", errors.New("key is required")
	}
	if err := models.ValidateKVKey(key); err != nil {
		return "", err
	}
	return key, nil
}

func kvEntryIdentityFromRequest(r *http.Request) (string, string, error) {
	namespace, err := kvEntryNamespaceFromRequest(r)
	if err != nil {
		return "", "", err
	}
	key, err := kvKeyFromRequest(r)
	if err != nil {
		return "", "", err
	}
	return namespace, key, nil
}

func kvListFiltersFromRequest(r *http.Request) (postgres.KVListFilters, error) {
	query := r.URL.Query()
	filters := postgres.KVListFilters{KeyPrefix: query.Get("prefix")}
	if query.Has("namespace") {
		namespace := models.NormalizeKVNamespace(query.Get("namespace"))
		if err := models.ValidateKVNamespace(namespace); err != nil {
			return postgres.KVListFilters{}, err
		}
		filters.Namespace = &namespace
	}
	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			return postgres.KVListFilters{}, errors.New("limit must be a positive integer")
		}
		filters.Limit = limit
	}
	if raw := query.Get("offset"); raw != "" {
		offset, err := strconv.Atoi(raw)
		if err != nil || offset < 0 {
			return postgres.KVListFilters{}, errors.New("offset must be a non-negative integer")
		}
		filters.Offset = offset
	}
	return filters, nil
}

func decodeKVSetRequest(w http.ResponseWriter, r *http.Request, maxValueBytes int64) (setKVRequest, error) {
	var req setKVRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxValueBytes+4096))
	if err := decoder.Decode(&req); err != nil {
		return setKVRequest{}, kvBadRequest(err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return setKVRequest{}, errKVBadRequest
		}
		return setKVRequest{}, kvBadRequest(err)
	}
	if req.Value == nil {
		return setKVRequest{}, fmt.Errorf("%w: value is required", errKVBadRequest)
	}
	if !json.Valid(req.Value) {
		return setKVRequest{}, fmt.Errorf("%w: value must be valid JSON", errKVBadRequest)
	}
	if err := models.ValidateKVTTLSeconds(req.TTLSeconds); err != nil {
		return setKVRequest{}, fmt.Errorf("%w: %v", errKVBadRequest, err)
	}
	return req, nil
}

func decodeKVRevealRequest(w http.ResponseWriter, r *http.Request) (revealKVRequest, error) {
	var req revealKVRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return revealKVRequest{}, kvBadRequest(err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return revealKVRequest{}, errKVBadRequest
		}
		return revealKVRequest{}, kvBadRequest(err)
	}
	req.Namespace = models.NormalizeKVNamespace(req.Namespace)
	if err := models.ValidateKVNamespace(req.Namespace); err != nil {
		return revealKVRequest{}, fmt.Errorf("%w: %v", errKVBadRequest, err)
	}
	if req.Key == "" {
		return revealKVRequest{}, fmt.Errorf("%w: key is required", errKVBadRequest)
	}
	if err := models.ValidateKVKey(req.Key); err != nil {
		return revealKVRequest{}, fmt.Errorf("%w: %v", errKVBadRequest, err)
	}
	return req, nil
}

func kvBadRequest(err error) error {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return err
	}
	return fmt.Errorf("%w: %v", errKVBadRequest, err)
}

func writeKVStoreError(w http.ResponseWriter, log *zap.Logger, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		writeError(w, http.StatusRequestEntityTooLarge, "kv request body exceeds size limit")
		return
	}
	switch {
	case errors.Is(err, postgres.ErrKVNotFound), errors.Is(err, postgres.ErrKVTenantNotFound):
		writeError(w, http.StatusNotFound, "kv entry not found")
	case errors.Is(err, errKVBadRequest):
		writeError(w, http.StatusBadRequest, strings.TrimPrefix(err.Error(), errKVBadRequest.Error()+": "))
	case errors.Is(err, postgres.ErrKVInvalidValue):
		writeError(w, http.StatusBadRequest, "value must be valid JSON")
	case errors.Is(err, postgres.ErrKVValueTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "kv value exceeds max_value_bytes")
	case errors.Is(err, postgres.ErrKVKeyQuota):
		writeError(w, http.StatusConflict, "kv max_keys quota exceeded")
	case errors.Is(err, postgres.ErrKVBytesQuota):
		writeError(w, http.StatusConflict, "kv max_total_bytes quota exceeded")
	default:
		log.Error("kv operation failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "kv operation failed")
	}
}
