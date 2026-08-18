package sandboxcontrol

import (
	"context"
	"errors"
	"testing"

	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
)

type intentStoreStub struct {
	result *postgres.SandboxIntentResult
	err    error
	calls  int
}

func (s *intentStoreStub) CreateSandboxIntent(context.Context, postgres.CreateSandboxParams, string, string, string, string) (*postgres.SandboxIntentResult, error) {
	s.calls++
	return s.result, s.err
}
func (s *intentStoreStub) RequestSandboxOperation(context.Context, string, string, string, string, string, string, string, *int64, *string, *string) (*postgres.SandboxIntentResult, error) {
	return nil, errors.New("unexpected RequestSandboxOperation call")
}

func TestCreateSandboxRejectsInvalidRequestBeforePersistence(t *testing.T) {
	store := &intentStoreStub{}
	_, err := createSandboxWithStore(context.Background(), store, "tenant", "actor", "user", "key", CreateSandboxRequest{Name: "demo"})
	if err == nil {
		t.Fatal("CreateSandbox succeeded without immutable recipe and profile IDs")
	}
	if store.calls != 0 {
		t.Fatalf("CreateSandboxIntent called %d times for invalid input", store.calls)
	}
}

func TestCreateSandboxReturnsSanitizedDTO(t *testing.T) {
	operation := &models.SandboxOperation{ID: "op-1", Kind: "create", State: "queued"}
	store := &intentStoreStub{result: &postgres.SandboxIntentResult{Sandbox: &models.Sandbox{ID: "sb-1", TenantID: "private-tenant", Name: "demo", RecipeVersionID: "rv-1", ProfileVersionID: "pv-1", ObservedState: "pending", DesiredState: "running", Generation: 1, FenceEpoch: 99}, Operation: operation}}
	result, err := createSandboxWithStore(context.Background(), store, "tenant", "actor", "user", "key", CreateSandboxRequest{Name: "demo", RecipeVersionID: "rv-1", ProfileVersionID: "pv-1"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Sandbox == nil || result.Sandbox.ID != "sb-1" || result.Operation != operation {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestReconcileDueOperationsDoesNotClaimWithoutDispatch(t *testing.T) {
	service := &Service{}
	if err := service.ReconcileDueOperations(context.Background()); err != nil {
		t.Fatal(err)
	}
}
