package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/abskrj/velane/services/control-plane/internal/sandboxcontrol"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// SandboxRecipesHandler owns the public, tenant-scoped image recipe API.
type SandboxRecipesHandler struct {
	service      *sandboxcontrol.Service
	capabilities sandboxcontrol.Provider
	log          *zap.Logger
}

func NewSandboxRecipesHandler(service *sandboxcontrol.Service, capabilities sandboxcontrol.Provider, log *zap.Logger) *SandboxRecipesHandler {
	return &SandboxRecipesHandler{service: service, capabilities: capabilities, log: log}
}

type createRecipeRequest struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

func (h *SandboxRecipesHandler) List(w http.ResponseWriter, r *http.Request) {
	tenant := sandboxTenant(w, r)
	if tenant == "" {
		return
	}
	limit, offset := pageQuery(r)
	items, total, err := h.service.Recipes(r.Context(), tenant, limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "unable to list image recipes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (h *SandboxRecipesHandler) Create(w http.ResponseWriter, r *http.Request) {
	if !h.requireCapability(w, r) {
		return
	}
	var request createRecipeRequest
	if !decodeSandboxJSON(w, r, &request) {
		return
	}
	tenant := sandboxTenant(w, r)
	if tenant == "" {
		return
	}
	recipe, replayed, err := h.service.CreateRecipe(r.Context(), tenant, actorFor(r), r.Header.Get("Idempotency-Key"), request.Name, request.Slug, request.Description)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"recipe": recipe, "replayed": replayed})
}

func (h *SandboxRecipesHandler) Get(w http.ResponseWriter, r *http.Request) {
	tenant := sandboxTenant(w, r)
	if tenant == "" {
		return
	}
	recipe, err := h.service.Recipe(r.Context(), tenant, chi.URLParam(r, "recipeID"))
	if err != nil {
		writeSandboxReadError(w, err, "unable to load image recipe")
		return
	}
	writeJSON(w, http.StatusOK, recipe)
}

func (h *SandboxRecipesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if !h.requireCapability(w, r) {
		return
	}
	tenant := sandboxTenant(w, r)
	if tenant == "" {
		return
	}
	if err := h.service.DeleteRecipe(r.Context(), tenant, chi.URLParam(r, "recipeID"), r.Header.Get("Idempotency-Key")); err != nil {
		writeSandboxError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *SandboxRecipesHandler) ListVersions(w http.ResponseWriter, r *http.Request) {
	tenant := sandboxTenant(w, r)
	if tenant == "" {
		return
	}
	if _, err := h.service.Recipe(r.Context(), tenant, chi.URLParam(r, "recipeID")); err != nil {
		writeSandboxReadError(w, err, "unable to load image recipe")
		return
	}
	limit, offset := pageQuery(r)
	items, total, err := h.service.RecipeVersions(r.Context(), tenant, chi.URLParam(r, "recipeID"), limit, offset)
	if err != nil {
		writeSandboxReadError(w, err, "unable to list image recipe versions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

func (h *SandboxRecipesHandler) CreateVersion(w http.ResponseWriter, r *http.Request) {
	if !h.requireCapability(w, r) {
		return
	}
	var document map[string]any
	if !decodeSandboxJSON(w, r, &document) {
		return
	}
	tenant := sandboxTenant(w, r)
	if tenant == "" {
		return
	}
	if _, err := h.service.Recipe(r.Context(), tenant, chi.URLParam(r, "recipeID")); err != nil {
		writeSandboxReadError(w, err, "unable to load image recipe")
		return
	}
	version, operation, replayed, err := h.service.CreateRecipeVersion(r.Context(), tenant, actorFor(r), r.Header.Get("Idempotency-Key"), chi.URLParam(r, "recipeID"), document)
	if err != nil {
		writeSandboxError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"version": version, "operation": operation, "replayed": replayed})
}

func (h *SandboxRecipesHandler) GetVersion(w http.ResponseWriter, r *http.Request) {
	tenant := sandboxTenant(w, r)
	if tenant == "" {
		return
	}
	if _, err := h.service.Recipe(r.Context(), tenant, chi.URLParam(r, "recipeID")); err != nil {
		writeSandboxReadError(w, err, "unable to load image recipe")
		return
	}
	version, ok := recipeVersionParam(w, r)
	if !ok {
		return
	}
	item, err := h.service.RecipeVersion(r.Context(), tenant, chi.URLParam(r, "recipeID"), version)
	if err != nil {
		writeSandboxReadError(w, err, "unable to load image recipe version")
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *SandboxRecipesHandler) Events(w http.ResponseWriter, r *http.Request) { h.cursor(w, r, true) }
func (h *SandboxRecipesHandler) Logs(w http.ResponseWriter, r *http.Request)   { h.cursor(w, r, false) }

func (h *SandboxRecipesHandler) cursor(w http.ResponseWriter, r *http.Request, events bool) {
	tenant := sandboxTenant(w, r)
	if tenant == "" {
		return
	}
	if _, err := h.service.Recipe(r.Context(), tenant, chi.URLParam(r, "recipeID")); err != nil {
		writeSandboxReadError(w, err, "unable to load image recipe")
		return
	}
	version, ok := recipeVersionParam(w, r)
	if !ok {
		return
	}
	if _, ok := cursorQuery(w, r); !ok {
		return
	}
	limit, _ := pageQuery(r)
	var items any
	var next string
	var err error
	if events {
		items, next, err = h.service.RecipeEvents(r.Context(), tenant, chi.URLParam(r, "recipeID"), version, r.URL.Query().Get("after"), limit)
	} else {
		items, next, err = h.service.RecipeLogs(r.Context(), tenant, chi.URLParam(r, "recipeID"), version, r.URL.Query().Get("after"), limit)
	}
	if err != nil {
		writeSandboxReadError(w, err, "unable to list image recipe build activity")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": next})
}

func recipeVersionParam(w http.ResponseWriter, r *http.Request) (int, bool) {
	version, err := strconv.Atoi(strings.TrimSpace(chi.URLParam(r, "version")))
	if err != nil || version < 1 {
		writeError(w, http.StatusBadRequest, "version must be a positive integer")
		return 0, false
	}
	return version, true
}

func actorFor(r *http.Request) string {
	actor, _ := resolveActor(r)
	return actor
}

func (h *SandboxRecipesHandler) requireCapability(w http.ResponseWriter, r *http.Request) bool {
	if h.capabilities != nil && h.capabilities.Available(r.Context(), sandboxcontrol.CapabilityImageRecipeMutation) {
		return true
	}
	writeSandboxCapabilityUnavailable(w)
	return false
}
