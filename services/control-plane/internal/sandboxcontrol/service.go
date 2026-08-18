package sandboxcontrol

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/abskrj/velane/services/control-plane/internal/ids"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/abskrj/velane/services/control-plane/internal/sandboxcontrol/domain"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
	"strings"
	"time"
)

type Service struct {
	store        *postgres.Store
	capabilities Provider
}

func NewService(store *postgres.Store, capabilities Provider) *Service {
	if capabilities == nil {
		capabilities = StaticProvider{}
	}
	return &Service{store: store, capabilities: capabilities}
}

func (s *Service) CapabilityProvider() Provider { return s.capabilities }

func capabilityUnavailable() error {
	return domain.NewError(domain.CapabilityUnavailable, "sandbox capability is not configured", false)
}

func (s *Service) requireCapability(ctx context.Context, capability Capability) error {
	if s == nil || s.capabilities == nil || !s.capabilities.Available(ctx, capability) {
		return capabilityUnavailable()
	}
	return nil
}

type CreateSandboxRequest struct {
	Name             string `json:"name"`
	RecipeVersionID  string `json:"recipe_version_id"`
	ProfileVersionID string `json:"profile_version_id"`
}
type MutationResult struct {
	Sandbox   *models.SandboxDTO       `json:"sandbox,omitempty"`
	Operation *models.SandboxOperation `json:"operation"`
	Replayed  bool                     `json:"replayed"`
}

type sandboxIntentStore interface {
	CreateSandboxIntent(context.Context, postgres.CreateSandboxParams, string, string, string, string) (*postgres.SandboxIntentResult, error)
}

func createSandboxWithStore(ctx context.Context, store sandboxIntentStore, tenantID, actorID, actorType, idempotencyKey string, r CreateSandboxRequest) (*MutationResult, error) {
	return createSandbox(ctx, store, tenantID, actorID, actorType, idempotencyKey, r)
}

func RequestHash(tenant, method, path string, body any) string {
	b, _ := json.Marshal(struct {
		Tenant, Method, Path string
		Body                 any
	}{tenant, method, path, body})
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func validIdempotencyKey(k string) bool {
	if len(k) < 1 || len(k) > 128 {
		return false
	}
	for _, r := range k {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}
func (s *Service) CreateSandbox(ctx context.Context, tenantID, actorID, actorType, idempotencyKey string, r CreateSandboxRequest) (*MutationResult, error) {
	if err := s.requireCapability(ctx, CapabilitySandboxCreate); err != nil {
		return nil, err
	}
	return createSandbox(ctx, s.store, tenantID, actorID, actorType, idempotencyKey, r)
}

func createSandbox(ctx context.Context, store sandboxIntentStore, tenantID, actorID, actorType, idempotencyKey string, r CreateSandboxRequest) (*MutationResult, error) {
	if !validIdempotencyKey(idempotencyKey) {
		return nil, domain.NewError(domain.IdempotencyConflict, "Idempotency-Key must contain 1–128 visible ASCII characters", false)
	}
	if strings.TrimSpace(r.Name) == "" || r.RecipeVersionID == "" || r.ProfileVersionID == "" {
		return nil, fmt.Errorf("name, recipe_version_id, and profile_version_id are required")
	}
	hash := RequestHash(tenantID, "POST", "/v1/sandboxes", r)
	intent, err := store.CreateSandboxIntent(ctx, postgres.CreateSandboxParams{TenantID: tenantID, Name: r.Name, RecipeVersionID: r.RecipeVersionID, ProfileVersionID: r.ProfileVersionID, CreatedBy: actorID}, idempotencyKey, hash, actorID, actorType)
	if err != nil {
		if strings.Contains(err.Error(), "quota exceeded") {
			return nil, domain.NewError(domain.QuotaExceeded, "sandbox quota is exhausted", false)
		}
		if strings.Contains(err.Error(), "no rows") {
			return nil, domain.NewError(domain.RecipeProfileIncompatible, "the image recipe is not ready for this profile", false)
		}
		return nil, err
	}
	if intent.Replayed {
		if intent.ExistingHash != hash {
			return nil, domain.NewError(domain.IdempotencyConflict, "idempotency key was used with a different request", false)
		}
		return &MutationResult{Operation: intent.Operation, Replayed: true}, nil
	}
	public := models.PublicSandbox(*intent.Sandbox, nil)
	return &MutationResult{Sandbox: &public, Operation: intent.Operation}, nil
}
func (s *Service) RequestOperation(ctx context.Context, tenantID, actorID, actorType, idempotencyKey, sandboxID string, kind domain.OperationKind, generation *int64) (*MutationResult, error) {
	capability := CapabilitySandboxStart
	if kind == domain.OperationStop || kind == domain.OperationRestart || kind == domain.OperationSnapshot {
		capability = CapabilitySandboxCheckpoint
	}
	if err := s.requireCapability(ctx, capability); err != nil {
		return nil, err
	}
	return s.requestOperation(ctx, tenantID, actorID, actorType, idempotencyKey, sandboxID, kind, generation, nil)
}
func (s *Service) RequestSnapshotOperation(ctx context.Context, tenantID, actorID, actorType, idempotencyKey, sandboxID, snapshotID string, kind domain.OperationKind, generation *int64) (*MutationResult, error) {
	if err := s.requireCapability(ctx, CapabilitySandboxCheckpoint); err != nil {
		return nil, err
	}
	return s.requestOperation(ctx, tenantID, actorID, actorType, idempotencyKey, sandboxID, kind, generation, &snapshotID)
}
func (s *Service) DeleteSandbox(ctx context.Context, tenantID, actorID, actorType, idempotencyKey, sandboxID string, deleteSnapshots bool, generation *int64) (*MutationResult, error) {
	capability := CapabilitySandboxDelete
	if deleteSnapshots {
		capability = CapabilitySandboxCheckpoint
	}
	if err := s.requireCapability(ctx, capability); err != nil {
		return nil, err
	}
	return s.requestOperation(ctx, tenantID, actorID, actorType, idempotencyKey, sandboxID, domain.OperationDelete, generation, nil, map[string]any{"delete_snapshots": deleteSnapshots})
}
func (s *Service) RetrySandboxOperation(ctx context.Context, tenantID, actorID, actorType, idempotencyKey, sandboxID, operationID string, generation *int64) (*MutationResult, error) {
	if err := s.requireCapability(ctx, CapabilitySandboxOperationRetry); err != nil {
		return nil, err
	}
	if !validIdempotencyKey(idempotencyKey) {
		return nil, domain.NewError(domain.IdempotencyConflict, "Idempotency-Key must contain 1–128 visible ASCII characters", false)
	}
	if operationID == "" {
		return nil, fmt.Errorf("operation_id is required")
	}
	hash := RequestHash(tenantID, "POST", "/v1/sandboxes/"+sandboxID+"/retry", map[string]string{"operation_id": operationID})
	intent, err := s.store.RetrySandboxOperation(ctx, tenantID, sandboxID, operationID, idempotencyKey, hash, actorID, actorType, generation)
	if err != nil {
		return nil, mapSandboxIntentError(err)
	}
	if intent.Replayed {
		if intent.ExistingHash != hash {
			return nil, domain.NewError(domain.IdempotencyConflict, "idempotency key was used with a different request", false)
		}
		return &MutationResult{Operation: intent.Operation, Replayed: true}, nil
	}
	public := models.PublicSandbox(*intent.Sandbox, s.availableActions(ctx, intent.Sandbox.ObservedState))
	return &MutationResult{Sandbox: &public, Operation: intent.Operation}, nil
}
func (s *Service) requestOperation(ctx context.Context, tenantID, actorID, actorType, idempotencyKey, sandboxID string, kind domain.OperationKind, generation *int64, snapshotID *string, body ...any) (*MutationResult, error) {
	if !validIdempotencyKey(idempotencyKey) {
		return nil, domain.NewError(domain.IdempotencyConflict, "Idempotency-Key must contain 1–128 visible ASCII characters", false)
	}
	method := "POST"
	path := "/v1/sandboxes/" + sandboxID + "/" + string(kind)
	switch kind {
	case domain.OperationSnapshot:
		path = "/v1/sandboxes/" + sandboxID + "/snapshots"
	case domain.OperationRestore:
		path = "/v1/sandboxes/" + sandboxID + "/snapshots/" + *snapshotID + "/restore"
	case domain.OperationSnapshotDelete:
		method = "DELETE"
		path = "/v1/sandboxes/" + sandboxID + "/snapshots/" + *snapshotID
	case domain.OperationDelete:
		method = "DELETE"
		path = "/v1/sandboxes/" + sandboxID
	}
	requestBody := any(map[string]any{})
	if len(body) > 0 {
		requestBody = body[0]
	}
	hash := RequestHash(tenantID, method, path, requestBody)
	intent, err := s.store.RequestSandboxOperation(ctx, tenantID, sandboxID, string(kind), idempotencyKey, hash, actorID, actorType, generation, snapshotID, nil, body...)
	if err != nil {
		return nil, mapSandboxIntentError(err)
	}
	if intent.Replayed {
		if intent.ExistingHash != hash {
			return nil, domain.NewError(domain.IdempotencyConflict, "idempotency key was used with a different request", false)
		}
		return &MutationResult{Operation: intent.Operation, Replayed: true}, nil
	}
	public := models.PublicSandbox(*intent.Sandbox, s.availableActions(ctx, intent.Sandbox.ObservedState))
	return &MutationResult{Sandbox: &public, Operation: intent.Operation}, nil
}

func mapSandboxIntentError(err error) error {
	if errors.Is(err, postgres.ErrNotFound) {
		return domain.NewError(domain.SandboxNotFound, "sandbox not found", false)
	}
	if strings.Contains(err.Error(), "generation conflict") {
		return domain.NewError(domain.GenerationConflict, "sandbox generation no longer matches", true)
	}
	if strings.Contains(err.Error(), "operation conflict") {
		return domain.NewError(domain.OperationConflict, "another sandbox operation is already active", true)
	}
	if strings.Contains(err.Error(), "idempotency conflict") {
		return domain.NewError(domain.IdempotencyConflict, "idempotency key was used with a different request", false)
	}
	return err
}

// ReconcileDueOperations claims only work for which DispatchClaimedSandboxOperation
// can atomically persist a command or return it to durable waiting state.
func (s *Service) ReconcileDueOperations(ctx context.Context) error {
	if s.store == nil {
		return nil
	}
	if err := s.store.ExpireSandboxHostLeases(ctx); err != nil {
		return err
	}
	owner, leaseHash := reconcileLeaseIdentity()
	operations, err := s.store.ClaimDueSandboxOperations(ctx, owner, leaseHash, 30*time.Second, 16)
	if err != nil {
		return err
	}
	for _, operation := range operations {
		if err := s.store.DispatchClaimedSandboxOperation(ctx, operation, owner, leaseHash); err != nil {
			return err
		}
	}
	return nil
}

func reconcileLeaseIdentity() (string, string) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		// crypto/rand errors are exceptionally rare; the DB CAS still prevents
		// another worker from advancing this claim if this fallback repeats.
		return ids.New(), ids.New()
	}
	return ids.New(), hex.EncodeToString(bytes)
}

func (s *Service) ListSandboxes(ctx context.Context, tenantID string, limit, offset int) ([]models.SandboxDTO, int, error) {
	items, total, err := s.store.ListSandboxes(ctx, tenantID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	out := make([]models.SandboxDTO, 0, len(items))
	for _, item := range items {
		out = append(out, models.PublicSandbox(item, s.availableActions(ctx, item.ObservedState)))
	}
	return out, total, nil
}

func (s *Service) GetSandbox(ctx context.Context, tenantID, sandboxID string) (*models.SandboxDTO, error) {
	item, err := s.store.GetSandbox(ctx, tenantID, sandboxID)
	if err != nil {
		return nil, err
	}
	public := models.PublicSandbox(*item, s.availableActions(ctx, item.ObservedState))
	return &public, nil
}

func (s *Service) availableActions(ctx context.Context, observed string) []string {
	return AvailableActions(ctx, s.capabilities, observed)
}

type ListPage[T any] struct {
	Items      []T    `json:"items"`
	Total      int    `json:"total"`
	NextCursor string `json:"next_cursor,omitempty"`
}

func (s *Service) Profiles(ctx context.Context) ([]models.SandboxProfileDTO, error) {
	return s.store.ListSandboxProfiles(ctx)
}
func (s *Service) Snapshots(ctx context.Context, t, id string, l, o int) ([]models.SandboxSnapshot, int, error) {
	return s.store.ListSandboxSnapshots(ctx, t, id, l, o)
}
func (s *Service) Snapshot(ctx context.Context, t, sandbox, id string) (*models.SandboxSnapshot, error) {
	return s.store.GetSandboxSnapshot(ctx, t, sandbox, id)
}
func (s *Service) Operation(ctx context.Context, t, id string) (*models.SandboxOperation, error) {
	return s.store.GetSandboxOperation(ctx, t, id)
}
func (s *Service) Events(ctx context.Context, t, id, after string, l int) ([]models.SandboxEventDTO, string, error) {
	return s.store.ListSandboxEvents(ctx, t, id, after, l)
}
func (s *Service) Logs(ctx context.Context, t, id, after string, l int) ([]models.SandboxLogDTO, string, error) {
	return s.store.ListSandboxLogs(ctx, t, id, after, l)
}
func (s *Service) Recipes(ctx context.Context, t string, l, o int) ([]models.SandboxRecipeDTO, int, error) {
	return s.store.ListRecipes(ctx, t, l, o)
}
func (s *Service) Recipe(ctx context.Context, t, id string) (*models.SandboxRecipeDTO, error) {
	return s.store.GetRecipe(ctx, t, id)
}
func (s *Service) RecipeVersions(ctx context.Context, t, id string, l, o int) ([]models.SandboxRecipeVersionDTO, int, error) {
	return s.store.ListRecipeVersions(ctx, t, id, l, o)
}
func (s *Service) RecipeVersion(ctx context.Context, t, id string, v int) (*models.SandboxRecipeVersionDTO, error) {
	return s.store.GetRecipeVersion(ctx, t, id, v)
}
func (s *Service) CreateRecipe(ctx context.Context, t, actor, key, name, slug, description string) (*models.SandboxRecipeDTO, bool, error) {
	if err := s.requireCapability(ctx, CapabilityImageRecipeMutation); err != nil {
		return nil, false, err
	}
	if !validIdempotencyKey(key) {
		return nil, false, domain.NewError(domain.IdempotencyConflict, "invalid Idempotency-Key", false)
	}
	if strings.TrimSpace(name) == "" {
		return nil, false, fmt.Errorf("name is required")
	}
	if slug == "" {
		slug = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", "-"))
	}
	return s.store.CreateRecipeIntent(ctx, t, name, slug, description, actor, key, RequestHash(t, "POST", "/v1/sandbox-image-recipes", map[string]string{"name": name, "slug": slug, "description": description}))
}
func (s *Service) DeleteRecipe(ctx context.Context, t, id, key string) error {
	if err := s.requireCapability(ctx, CapabilityImageRecipeMutation); err != nil {
		return err
	}
	if !validIdempotencyKey(key) {
		return domain.NewError(domain.IdempotencyConflict, "invalid Idempotency-Key", false)
	}
	return mapSandboxIntentError(s.store.DeleteRecipe(ctx, t, id))
}
func (s *Service) CreateRecipeVersion(ctx context.Context, tenant, actor, key, recipe string, document any) (*models.SandboxRecipeVersionDTO, *models.SandboxOperation, bool, error) {
	if err := s.requireCapability(ctx, CapabilityImageRecipeMutation); err != nil {
		return nil, nil, false, err
	}
	if !validIdempotencyKey(key) {
		return nil, nil, false, domain.NewError(domain.IdempotencyConflict, "invalid Idempotency-Key", false)
	}
	b, err := json.Marshal(document)
	if err != nil {
		return nil, nil, false, fmt.Errorf("encode recipe document: %w", err)
	}
	spec, err := domain.DecodeRecipeSpecV1(b)
	if err != nil {
		return nil, nil, false, err
	}
	digest, err := domain.RecipeDigest(spec)
	if err != nil {
		return nil, nil, false, err
	}
	version, operation, replayed, err := s.store.CreateRecipeVersionIntent(ctx, tenant, recipe, spec.SchemaVersion, b, digest, spec.BaseImage, actor, key, RequestHash(tenant, "POST", "/v1/sandbox-image-recipes/"+recipe+"/versions", document))
	if err != nil {
		return nil, nil, false, mapSandboxIntentError(err)
	}
	return version, operation, replayed, nil
}
func (s *Service) RecipeEvents(ctx context.Context, tenant, recipe string, version int, after string, limit int) ([]models.SandboxEventDTO, string, error) {
	return s.store.ListRecipeEvents(ctx, tenant, recipe, version, after, limit)
}
func (s *Service) RecipeLogs(ctx context.Context, tenant, recipe string, version int, after string, limit int) ([]models.SandboxLogDTO, string, error) {
	return s.store.ListRecipeLogs(ctx, tenant, recipe, version, after, limit)
}
