package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/api/middleware"
	"github.com/abskrj/velane/services/control-plane/internal/auth"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/abskrj/velane/services/control-plane/internal/scheduler"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// InvocationsHandler bundles invocation-related HTTP handlers.
type InvocationsHandler struct {
	store     *postgres.Store
	scheduler *scheduler.Scheduler
	log       *zap.Logger
	provider  auth.Provider // optional; enables session JWT auth on /invoke
}

// NewInvocationsHandler constructs an InvocationsHandler.
func NewInvocationsHandler(store *postgres.Store, sched *scheduler.Scheduler, log *zap.Logger) *InvocationsHandler {
	return &InvocationsHandler{store: store, scheduler: sched, log: log}
}

// WithAuthProvider enables session JWT auth on the Invoke endpoint in addition
// to API key auth. Call this when wiring up the router.
func (h *InvocationsHandler) WithAuthProvider(p auth.Provider) *InvocationsHandler {
	h.provider = p
	return h
}

// invokeBody is the optional JSON body for invoke requests.
type invokeBody struct {
	CallbackURL string `json:"callback_url"`
}

// Invoke is kept as a compatibility shim and delegates to InvokeByToken.
func (h *InvocationsHandler) Invoke(w http.ResponseWriter, r *http.Request) {
	h.InvokeByToken(w, r)
}

// InvokeByToken handles POST /v1/invoke/{snippetSlug} (tenant-slug-free variant).
//
// The tenant is resolved from the authenticated credential rather than the URL:
//   - API key (vl_…): tenant comes from the key's TenantID.
//   - Session JWT: tenant is identified from the user's active membership.
//
// All other behaviour (invoke modes, query params, body) is identical to Invoke.
func (h *InvocationsHandler) InvokeByToken(w http.ResponseWriter, r *http.Request) {
	snippetSlug := chi.URLParam(r, "snippetSlug")

	// --- Inline auth: session JWT first, then API key ---
	var token string
	if authHdr := r.Header.Get("Authorization"); authHdr != "" {
		parts := strings.SplitN(authHdr, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
			writeError(w, http.StatusUnauthorized, "malformed Authorization header")
			return
		}
		token = strings.TrimSpace(parts[1])
	} else if cookie, err := r.Cookie(middleware.SessionCookieName); err == nil {
		token = strings.TrimSpace(cookie.Value)
	}
	if token == "" {
		writeError(w, http.StatusUnauthorized, "missing Authorization header or session cookie")
		return
	}

	var tenant *models.Tenant

	// Try session JWT auth when an auth provider is configured.
	if h.provider != nil {
		if user, err := h.provider.ValidateSession(r.Context(), token); err == nil {
			memberships, err := h.store.ListUserTenantMemberships(r.Context(), user.ID)
			if err != nil || len(memberships) == 0 {
				writeError(w, http.StatusForbidden, "no tenant membership found")
				return
			}
			activeMembership := memberships[0]
			if role := activeMembership.Role; role != "invoke" && role != "manage" && role != "admin" {
				writeError(w, http.StatusForbidden, "insufficient role for invoke")
				return
			}
			t, err := h.store.GetTenantByID(r.Context(), activeMembership.TenantID)
			if err != nil {
				writeError(w, http.StatusNotFound, "tenant not found")
				return
			}
			tenant = t
			goto authOKByToken
		}
	}

	// Fall back to API key auth — tenant is resolved from key.TenantID.
	{
		key, err := h.store.ValidateAPIKey(r.Context(), token)
		if err != nil {
			h.log.Debug("invoke: invalid api key", zap.Error(err))
			writeError(w, http.StatusUnauthorized, "invalid api key")
			return
		}
		if !key.HasScope("invoke") {
			writeError(w, http.StatusForbidden, "api key missing 'invoke' scope")
			return
		}
		t, err := h.store.GetTenantByID(r.Context(), key.TenantID)
		if err != nil {
			h.log.Error("invoke: tenant lookup failed", zap.String("tenant_id", key.TenantID), zap.Error(err))
			writeError(w, http.StatusInternalServerError, "tenant not found")
			return
		}
		tenant = t
	}

authOKByToken:

	// --- Read the input payload ---
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}

	var ib invokeBody
	if len(body) > 0 {
		_ = json.Unmarshal(body, &ib)
	}

	inputPayload := string(body)
	if inputPayload == "" {
		inputPayload = "{}"
	}

	if !json.Valid([]byte(inputPayload)) {
		writeError(w, http.StatusBadRequest, "request body must be valid JSON")
		return
	}

	// --- Resolve env from query param, default to prod ---
	env := r.URL.Query().Get("env")
	if env == "" {
		env = "prod"
	}
	if env != "dev" && env != "staging" && env != "prod" {
		writeError(w, http.StatusBadRequest, "env must be 'dev', 'staging', or 'prod'")
		return
	}

	// --- Resolve pinned version from query param ---
	var pinnedVersion int
	if vStr := r.URL.Query().Get("version"); vStr != "" {
		vStr = strings.TrimPrefix(vStr, "v")
		n, err := strconv.Atoi(vStr)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "version must be a positive integer, e.g. ?version=3 or ?version=v3")
			return
		}
		pinnedVersion = n
	}

	invokeReq := scheduler.InvokeRequest{
		TenantID:      tenant.ID,
		SnippetSlug:   snippetSlug,
		Env:           env,
		Input:         inputPayload,
		PinnedVersion: pinnedVersion,
	}

	// --- Invoke mode ---
	mode := strings.ToLower(r.Header.Get("X-Invoke-Mode"))
	if mode == "" {
		mode = "sync"
	}
	invokeReq.InvokeMode = mode

	switch mode {
	case "sync":
		// When a live event stream is configured, route sync through the queue:
		// the worker streams output and we present it as SSE (Accept negotiation)
		// or a buffered JSON body. Otherwise fall back to inline execution.
		if h.scheduler.HasEventStream() {
			h.invokeQueuedMode(w, r, invokeReq, wantsSSE(r))
			return
		}
		h.invokeSyncMode(w, r, invokeReq)
	case "async":
		h.invokeAsyncMode(w, r, invokeReq, ib.CallbackURL)
	case "stream":
		// Stream mode routes through the queue when available. Transport is
		// chosen by Accept: SSE for text/event-stream callers, buffered JSON
		// otherwise (e.g. MCP, which cannot consume SSE — it gets the final
		// aggregated result).
		if h.scheduler.HasEventStream() {
			h.invokeQueuedMode(w, r, invokeReq, wantsSSE(r))
			return
		}
		h.invokeStreamMode(w, r, invokeReq)
	default:
		writeError(w, http.StatusBadRequest, "X-Invoke-Mode must be 'sync', 'async', or 'stream'")
	}
}

// wantsSSE reports whether the caller asked for a Server-Sent Events stream.
func wantsSSE(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream")
}

func writeInvokeError(w http.ResponseWriter, log *zap.Logger, slug string, err error) {
	if errors.Is(err, scheduler.ErrAccessDenied) {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}
	log.Error("invoke failed", zap.String("slug", slug), zap.Error(err))
	writeError(w, http.StatusBadRequest, err.Error())
}

func (h *InvocationsHandler) invokeSyncMode(w http.ResponseWriter, r *http.Request, req scheduler.InvokeRequest) {
	invocation, err := h.scheduler.Invoke(r.Context(), req)
	if err != nil {
		writeInvokeError(w, h.log, req.SnippetSlug, err)
		return
	}

	w.Header().Set("X-Invocation-Id", invocation.ID)
	w.Header().Set("X-Duration-Ms", fmt.Sprintf("%d", invocation.DurationMs))

	// Parse the output JSON so it embeds naturally in the response object.
	var outputVal any
	if err := json.Unmarshal([]byte(invocation.Output), &outputVal); err != nil {
		outputVal = invocation.Output
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"output":        outputVal,
		"invocation_id": invocation.ID,
		"duration_ms":   invocation.DurationMs,
		"status":        invocation.Status,
		"error":         invocation.Error,
		"stderr":        invocation.Stderr,
	})
}

// eventView is the minimal projection of a stream event payload the handler
// needs to relay or aggregate.
type eventView struct {
	Type   string `json:"type"`
	Stream string `json:"stream"`
	Text   string `json:"text"`
	Done   bool   `json:"done"`
}

// Tuning for the queued read loop.
const (
	queuedBlockFor    = 5 * time.Second
	queuedIdleTimeout = 90 * time.Second
	queuedMaxWait     = 5 * time.Minute
)

// invokeQueuedMode enqueues the invocation, then relays the worker's live event
// stream. When sse is true the events are forwarded as Server-Sent Events;
// otherwise they are aggregated and a single JSON body is returned once the
// worker finalizes the invocation. Reads start at "0" — Redis Streams retain
// events, so there is no subscribe-before-enqueue race.
func (h *InvocationsHandler) invokeQueuedMode(w http.ResponseWriter, r *http.Request, req scheduler.InvokeRequest, sse bool) {
	invocation, err := h.scheduler.InvokeQueued(r.Context(), req)
	if err != nil {
		writeInvokeError(w, h.log, req.SnippetSlug, err)
		return
	}

	ctx := r.Context()
	deadline := time.Now().Add(queuedMaxWait)
	lastActivity := time.Now()
	lastID := "0"

	if sse {
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeError(w, http.StatusInternalServerError, "streaming not supported by this server")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		w.Header().Set("X-Invocation-Id", invocation.ID)
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		for {
			if ctx.Err() != nil {
				return // client disconnected; worker continues and finalizes the DB
			}
			events, next, rerr := h.scheduler.ReadEvents(ctx, invocation.ID, lastID, queuedBlockFor)
			if rerr != nil {
				if ctx.Err() != nil {
					return
				}
				h.log.Error("queued: read events", zap.Error(rerr))
				_, _ = fmt.Fprint(w, "data: {\"type\":\"error\",\"message\":\"stream read error\",\"done\":true}\n\n")
				flusher.Flush()
				return
			}
			lastID = next
			if len(events) == 0 {
				if time.Since(lastActivity) > queuedIdleTimeout || time.Now().After(deadline) {
					_, _ = fmt.Fprint(w, "data: {\"type\":\"done\",\"done\":true,\"error\":\"timeout\"}\n\n")
					flusher.Flush()
					return
				}
				continue
			}
			lastActivity = time.Now()
			for _, ev := range events {
				_, _ = fmt.Fprintf(w, "id: %s\ndata: %s\n\n", ev.ID, ev.Payload)
				flusher.Flush()
				var v eventView
				_ = json.Unmarshal(ev.Payload, &v)
				if v.Done || v.Type == "done" {
					return
				}
			}
		}
	}

	// --- Buffered JSON path: aggregate logs, then return the finalized row. ---
	var logs []map[string]string
	for {
		if ctx.Err() != nil {
			return
		}
		events, next, rerr := h.scheduler.ReadEvents(ctx, invocation.ID, lastID, queuedBlockFor)
		if rerr != nil {
			if ctx.Err() != nil {
				return
			}
			break
		}
		lastID = next
		if len(events) == 0 {
			if time.Since(lastActivity) > queuedIdleTimeout || time.Now().After(deadline) {
				break
			}
			continue
		}
		lastActivity = time.Now()
		done := false
		for _, ev := range events {
			var v eventView
			_ = json.Unmarshal(ev.Payload, &v)
			if v.Type == "log" {
				logs = append(logs, map[string]string{"stream": v.Stream, "text": v.Text})
			}
			if v.Done || v.Type == "done" {
				done = true
			}
		}
		if done {
			break
		}
	}

	// The worker finalizes the DB before emitting "done", so the row is
	// authoritative here. Use a fresh context so a client disconnect mid-read
	// does not abort the final lookup.
	final, gerr := h.store.GetInvocation(context.Background(), invocation.ID)
	if gerr != nil {
		final = invocation
	}

	var outputVal any
	if err := json.Unmarshal([]byte(final.Output), &outputVal); err != nil {
		outputVal = final.Output
	}

	w.Header().Set("X-Invocation-Id", final.ID)
	w.Header().Set("X-Duration-Ms", fmt.Sprintf("%d", final.DurationMs))

	resp := map[string]any{
		"output":        outputVal,
		"invocation_id": final.ID,
		"duration_ms":   final.DurationMs,
		"status":        final.Status,
		"error":         final.Error,
		"stderr":        final.Stderr,
	}
	// Debug logs are surfaced only for dev invocations.
	if req.Env == "dev" {
		resp["logs"] = logs
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *InvocationsHandler) invokeAsyncMode(w http.ResponseWriter, r *http.Request, req scheduler.InvokeRequest, callbackURL string) {
	invocation, err := h.scheduler.InvokeAsync(r.Context(), req, callbackURL)
	if err != nil {
		writeInvokeError(w, h.log, req.SnippetSlug, err)
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"invocation_id": invocation.ID,
		"status":        invocation.Status,
		"status_url":    "/v1/invocations/" + invocation.ID,
	})
}

func (h *InvocationsHandler) invokeStreamMode(w http.ResponseWriter, r *http.Request, req scheduler.InvokeRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported by this server")
		return
	}

	ch, invocation, err := h.scheduler.InvokeStream(r.Context(), req)
	if err != nil {
		writeInvokeError(w, h.log, req.SnippetSlug, err)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("X-Invocation-Id", invocation.ID)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for chunk := range ch {
		data, err := json.Marshal(chunk)
		if err != nil {
			h.log.Error("stream: marshal chunk", zap.Error(err))
			continue
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()

		if chunk.Done {
			break
		}
	}

	// Drain any remaining chunks in case we broke early.
	for range ch {
	}

	// Final done event.
	_, _ = fmt.Fprintf(w, "data: {\"done\":true}\n\n")
	flusher.Flush()
}

// GetInvocation handles GET /v1/invocations/{id}.
func (h *InvocationsHandler) GetInvocation(w http.ResponseWriter, r *http.Request) {
	tenant := middleware.TenantFromContext(r.Context())
	if tenant == nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}

	id := chi.URLParam(r, "id")
	invocation, err := h.store.GetInvocation(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "invocation not found")
		return
	}

	// Enforce tenant isolation.
	if invocation.TenantID != tenant.ID {
		writeError(w, http.StatusNotFound, "invocation not found")
		return
	}

	writeJSON(w, http.StatusOK, invocation)
}
