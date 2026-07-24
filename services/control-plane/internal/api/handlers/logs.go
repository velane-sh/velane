package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/api/middleware"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// LogsHandler contains Phase 5 log query endpoints.
type LogsHandler struct {
	store *postgres.Store
	log   *zap.Logger
}

type invocationLogSummary struct {
	ID           string                  `json:"id"`
	SnippetID    string                  `json:"snippet_id"`
	VersionID    string                  `json:"version_id"`
	Environment  string                  `json:"environment"`
	TenantID     string                  `json:"tenant_id"`
	Status       models.InvocationStatus `json:"status"`
	DurationMs   int                     `json:"duration_ms"`
	PeakMemoryMB int                     `json:"peak_memory_mb"`
	CPUMs        int                     `json:"cpu_ms"`
	CreatedAt    time.Time               `json:"created_at"`
	CompletedAt  *time.Time              `json:"completed_at,omitempty"`
	InvokeMode   string                  `json:"invoke_mode"`
	PayloadState string                  `json:"payload_state"`
}

func summarizeInvocations(invocations []*models.Invocation) []invocationLogSummary {
	items := make([]invocationLogSummary, 0, len(invocations))
	for _, invocation := range invocations {
		items = append(items, invocationLogSummary{
			ID: invocation.ID, SnippetID: invocation.SnippetID, VersionID: invocation.VersionID,
			Environment: invocation.Environment, TenantID: invocation.TenantID, Status: invocation.Status,
			DurationMs: invocation.DurationMs, PeakMemoryMB: invocation.PeakMemoryMB, CPUMs: invocation.CPUMs,
			CreatedAt: invocation.CreatedAt, CompletedAt: invocation.CompletedAt,
			InvokeMode: invocation.InvokeMode, PayloadState: invocation.PayloadState,
		})
	}
	return items
}

// NewLogsHandler constructs a LogsHandler.
func NewLogsHandler(store *postgres.Store, log *zap.Logger) *LogsHandler {
	return &LogsHandler{store: store, log: log}
}

func isValidInvocationStatus(status string) bool {
	switch models.InvocationStatus(status) {
	case models.InvocationPending, models.InvocationRunning, models.InvocationCompleted,
		models.InvocationFailed, models.InvocationTimeout, models.InvocationOOMKilled:
		return true
	default:
		return false
	}
}

// GetSnippetLogs handles GET /v1/logs/snippets/{snippetID}.
func (h *LogsHandler) GetSnippetLogs(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	snippetID := chi.URLParam(r, "snippetID")
	snippet, err := h.store.GetSnippetByID(r.Context(), snippetID)
	if err != nil || snippet.TenantID != tenant.ID {
		writeError(w, http.StatusNotFound, "snippet not found")
		return
	}

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		var parsed int
		if _, scanErr := fmt.Sscanf(v, "%d", &parsed); scanErr != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		limit = parsed
	}

	var startTime *time.Time
	if raw := r.URL.Query().Get("start_time"); raw != "" {
		t, parseErr := time.Parse(time.RFC3339, raw)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "start_time must be RFC3339")
			return
		}
		startTime = &t
	}

	var endTime *time.Time
	if raw := r.URL.Query().Get("end_time"); raw != "" {
		t, parseErr := time.Parse(time.RFC3339, raw)
		if parseErr != nil {
			writeError(w, http.StatusBadRequest, "end_time must be RFC3339")
			return
		}
		endTime = &t
	}

	status := r.URL.Query().Get("status")
	if status != "" && !isValidInvocationStatus(status) {
		writeError(w, http.StatusBadRequest, "invalid status filter")
		return
	}

	env := r.URL.Query().Get("env")
	if env != "" && env != "dev" && env != "staging" && env != "prod" {
		writeError(w, http.StatusBadRequest, "env must be 'dev', 'staging', or 'prod'")
		return
	}

	invocations, listErr := h.store.ListInvocationLogs(r.Context(), snippetID, postgres.InvocationLogFilters{
		Environment: env,
		Status:      status,
		StartTime:   startTime,
		EndTime:     endTime,
		Limit:       limit,
	})
	if listErr != nil {
		h.log.Error("list logs failed", zap.Error(listErr), zap.String("snippet_id", snippetID))
		writeError(w, http.StatusInternalServerError, "failed to query logs")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"snippet_id": snippetID,
		"filters": map[string]any{
			"limit":      limit,
			"status":     status,
			"env":        env,
			"start_time": r.URL.Query().Get("start_time"),
			"end_time":   r.URL.Query().Get("end_time"),
		},
		"items": summarizeInvocations(invocations),
	})
}
