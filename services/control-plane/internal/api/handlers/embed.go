package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/api/middleware"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// EmbedHandler bundles token management and read-only embed endpoints.
type EmbedHandler struct {
	store *postgres.Store
	log   *zap.Logger
}

const snippetNotFoundMsg = "snippet not found"

func NewEmbedHandler(store *postgres.Store, log *zap.Logger) *EmbedHandler {
	return &EmbedHandler{store: store, log: log}
}

type createEmbedTokenRequest struct {
	TenantSlug string   `json:"tenant_slug"`
	SnippetIDs []string `json:"snippet_ids,omitempty"`
	TTLSeconds int      `json:"ttl_seconds,omitempty"`
}

func snippetAllowed(allowed map[string]struct{}, id string) bool {
	if len(allowed) == 0 {
		return true
	}
	_, ok := allowed[id]
	return ok
}

func toAllowedSet(ids []string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, id := range ids {
		set[id] = struct{}{}
	}
	return set
}

// ListTokens handles GET /v1/embed/tokens.
func (h *EmbedHandler) ListTokens(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	tokens, err := h.store.ListEmbedTokens(r.Context(), tenant.ID)
	if err != nil {
		h.log.Error("list embed tokens failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to list embed tokens")
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

// CreateToken handles POST /v1/embed/tokens.
func (h *EmbedHandler) CreateToken(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	createdBy := ""
	if key := middleware.APIKeyFromContext(r.Context()); key != nil {
		createdBy = key.ID
	} else if user := middleware.SessionUserFromContext(r.Context()); user != nil {
		createdBy = user.ID
	}

	var req createEmbedTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.TTLSeconds <= 0 {
		req.TTLSeconds = 3600
	}

	for _, snippetID := range req.SnippetIDs {
		snippet, err := h.store.GetSnippetByID(r.Context(), snippetID)
		if err != nil || snippet.TenantID != tenant.ID {
			writeError(w, http.StatusBadRequest, "all snippet_ids must belong to the tenant")
			return
		}
	}

	token, plain, err := h.store.CreateEmbedToken(
		r.Context(),
		tenant.ID,
		req.SnippetIDs,
		time.Duration(req.TTLSeconds)*time.Second,
		createdBy,
	)
	if err != nil {
		h.log.Error("create embed token failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to create embed token")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         token.ID,
		"token":      plain,
		"expires_at": token.ExpiresAt,
	})
}

// RevokeToken handles DELETE /v1/embed/tokens/{tokenID}.
func (h *EmbedHandler) RevokeToken(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	tokenID := chi.URLParam(r, "tokenID")
	if tokenID == "" {
		writeError(w, http.StatusBadRequest, "tokenID is required")
		return
	}
	if err := h.store.RevokeEmbedToken(r.Context(), tenant.ID, tokenID); err != nil {
		writeError(w, http.StatusNotFound, "embed token not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Bootstrap handles GET /v1/embed/bootstrap.
func (h *EmbedHandler) Bootstrap(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	embedCtx := middleware.EmbedContextFromContext(r.Context())
	if tenant == nil || embedCtx == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant": map[string]any{
			"id":       tenant.ID,
			"slug":     tenant.Slug,
			"name":     tenant.Name,
			"branding": tenant.Branding,
		},
		"embed": map[string]any{
			"token_id":            embedCtx.TokenID,
			"expires_at":          embedCtx.ExpiresAt,
			"allowed_snippet_ids": embedCtx.AllowedSnippetIDs,
			"capabilities": map[string]bool{
				"read_snippets": true,
				"read_versions": true,
				"read_metrics":  true,
				"read_logs":     true,
			},
		},
	})
}

// ListSnippets handles GET /v1/embed/snippets.
func (h *EmbedHandler) ListSnippets(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	embedCtx := middleware.EmbedContextFromContext(r.Context())
	if tenant == nil || embedCtx == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	all, err := h.store.ListSnippets(r.Context(), tenant.ID)
	if err != nil {
		h.log.Error("embed list snippets failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to list snippets")
		return
	}
	allowed := toAllowedSet(embedCtx.AllowedSnippetIDs)
	filtered := make([]*models.Snippet, 0, len(all))
	for _, s := range all {
		if snippetAllowed(allowed, s.ID) {
			filtered = append(filtered, s)
		}
	}
	writeJSON(w, http.StatusOK, filtered)
}

// GetSnippet handles GET /v1/embed/snippets/{snippetID}.
func (h *EmbedHandler) GetSnippet(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	embedCtx := middleware.EmbedContextFromContext(r.Context())
	if tenant == nil || embedCtx == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	snippetID := chi.URLParam(r, "snippetID")
	snippet, err := h.store.GetSnippetByID(r.Context(), snippetID)
	if err != nil || snippet.TenantID != tenant.ID {
		writeError(w, http.StatusNotFound, snippetNotFoundMsg)
		return
	}
	if !snippetAllowed(toAllowedSet(embedCtx.AllowedSnippetIDs), snippetID) {
		writeError(w, http.StatusNotFound, snippetNotFoundMsg)
		return
	}

	versions, _ := h.store.ListVersions(r.Context(), snippetID)
	devEnv, _ := h.store.GetSnippetEnvironment(r.Context(), snippetID, "dev")
	stagingEnv, _ := h.store.GetSnippetEnvironment(r.Context(), snippetID, "staging")
	prodEnv, _ := h.store.GetSnippetEnvironment(r.Context(), snippetID, "prod")

	writeJSON(w, http.StatusOK, map[string]any{
		"snippet":  snippet,
		"versions": versions,
		"environments": map[string]any{
			"dev":     devEnv,
			"staging": stagingEnv,
			"prod":    prodEnv,
		},
	})
}

// GetSnippetMetrics handles GET /v1/embed/snippets/{snippetID}/metrics.
func (h *EmbedHandler) GetSnippetMetrics(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	embedCtx := middleware.EmbedContextFromContext(r.Context())
	if tenant == nil || embedCtx == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	snippetID := chi.URLParam(r, "snippetID")
	snippet, err := h.store.GetSnippetByID(r.Context(), snippetID)
	if err != nil || snippet.TenantID != tenant.ID || !snippetAllowed(toAllowedSet(embedCtx.AllowedSnippetIDs), snippetID) {
		writeError(w, http.StatusNotFound, snippetNotFoundMsg)
		return
	}

	window := r.URL.Query().Get("window")
	if window == "" {
		window = "24h"
	}
	var since time.Time
	switch window {
	case "1h":
		since = time.Now().Add(-time.Hour)
	case "24h":
		since = time.Now().Add(-24 * time.Hour)
	case "7d":
		since = time.Now().Add(-7 * 24 * time.Hour)
	default:
		writeError(w, http.StatusBadRequest, "window must be one of: 1h, 24h, 7d")
		return
	}
	metrics, err := h.store.GetSnippetMetrics(r.Context(), snippetID, window, since)
	if err != nil {
		h.log.Error("embed metrics query failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to query metrics")
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

// GetSnippetLogs handles GET /v1/embed/snippets/{snippetID}/logs.
func (h *EmbedHandler) GetSnippetLogs(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	embedCtx := middleware.EmbedContextFromContext(r.Context())
	if tenant == nil || embedCtx == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	snippetID := chi.URLParam(r, "snippetID")
	snippet, err := h.store.GetSnippetByID(r.Context(), snippetID)
	if err != nil || snippet.TenantID != tenant.ID || !snippetAllowed(toAllowedSet(embedCtx.AllowedSnippetIDs), snippetID) {
		writeError(w, http.StatusNotFound, snippetNotFoundMsg)
		return
	}

	limit := 25
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, parseErr := strconv.Atoi(raw)
		if parseErr != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = n
	}
	items, err := h.store.ListInvocationLogs(r.Context(), snippetID, postgres.InvocationLogFilters{
		Status:      r.URL.Query().Get("status"),
		Environment: r.URL.Query().Get("env"),
		Limit:       limit,
	})
	if err != nil {
		h.log.Error("embed logs query failed", zap.Error(err))
		writeError(w, http.StatusInternalServerError, "failed to query logs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"snippet_id": snippetID,
		"items":      summarizeInvocations(items),
	})
}
