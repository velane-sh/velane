package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
)

// setupInvocationFixtures creates a tenant, snippet, version, and publishes the
// version to prod so it can be invoked. Returns the IDs needed for invocation tests.
func setupInvocationFixtures(t *testing.T) (store interface {
	CreateInvocation(ctx context.Context, snippetID, versionID, env, tenantID, input string) (*models.Invocation, error)
	UpdateInvocationResult(ctx context.Context, id string, status models.InvocationStatus, output, errMsg, stderr string, durationMs, peakMemoryMB, cpuMs int) error
	GetInvocation(ctx context.Context, id string) (*models.Invocation, error)
	ListInvocationsBySnippet(ctx context.Context, snippetID string, limit int) ([]*models.Invocation, error)
}, tenantID, snippetID, versionID string) {
	t.Helper()
	return nil, "", "", "" // replaced by concrete test below
}

func TestCreateInvocation(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	tenant, _ := store.CreateTenant(ctx, "Inv Org", uniqueSlug(t, "inv-org"))
	sn, _ := store.CreateSnippet(ctx, tenant.ID, "Inv Snippet", "bun", "user-1")
	v, _ := store.CreateVersion(ctx, sn.ID, "code", "{}", "{}", "user-1", 5000, 128, 100)

	inv, err := store.CreateInvocation(ctx, sn.ID, v.ID, "prod", tenant.ID, `{"prompt":"hello"}`)
	if err != nil {
		t.Fatalf("CreateInvocation: %v", err)
	}

	if inv.ID == "" {
		t.Error("invocation ID must not be empty")
	}
	if inv.Status != models.InvocationRunning {
		t.Errorf("status = %q; want %q", inv.Status, models.InvocationRunning)
	}
	if inv.InputPayload != `{"prompt":"hello"}` {
		t.Errorf("input_payload = %q; want %q", inv.InputPayload, `{"prompt":"hello"}`)
	}
	if inv.TenantID != tenant.ID {
		t.Errorf("tenant_id = %q; want %q", inv.TenantID, tenant.ID)
	}
}

func TestUpdateInvocationResult(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	tenant, _ := store.CreateTenant(ctx, "Update Inv Org", uniqueSlug(t, "updinv-org"))
	sn, _ := store.CreateSnippet(ctx, tenant.ID, "Upd Snippet", "bun", "user-1")
	v, _ := store.CreateVersion(ctx, sn.ID, "code", "{}", "{}", "user-1", 5000, 128, 100)
	inv, _ := store.CreateInvocation(ctx, sn.ID, v.ID, "prod", tenant.ID, `{}`)

	err := store.UpdateInvocationResult(ctx, inv.ID,
		models.InvocationCompleted, `{"result":"ok"}`, "", "some stderr", 123, 45, 67)
	if err != nil {
		t.Fatalf("UpdateInvocationResult: %v", err)
	}

	fetched, err := store.GetInvocation(ctx, inv.ID)
	if err != nil {
		t.Fatalf("GetInvocation: %v", err)
	}

	if fetched.Status != models.InvocationCompleted {
		t.Errorf("status = %q; want %q", fetched.Status, models.InvocationCompleted)
	}
	if fetched.Output != `{"result":"ok"}` {
		t.Errorf("output = %q; want %q", fetched.Output, `{"result":"ok"}`)
	}
	if fetched.Stderr != "some stderr" {
		t.Errorf("stderr = %q; want %q", fetched.Stderr, "some stderr")
	}
	if fetched.DurationMs != 123 {
		t.Errorf("duration_ms = %d; want 123", fetched.DurationMs)
	}
	if fetched.PeakMemoryMB != 45 {
		t.Errorf("peak_memory_mb = %d; want 45", fetched.PeakMemoryMB)
	}
	if fetched.CPUMs != 67 {
		t.Errorf("cpu_ms = %d; want 67", fetched.CPUMs)
	}
	if fetched.CompletedAt == nil {
		t.Error("completed_at must be set after update")
	}
}

func TestUpdateInvocationResult_Timeout(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	tenant, _ := store.CreateTenant(ctx, "Timeout Org", uniqueSlug(t, "timeout-org"))
	sn, _ := store.CreateSnippet(ctx, tenant.ID, "Timeout Snippet", "python", "user-1")
	v, _ := store.CreateVersion(ctx, sn.ID, "code", "{}", "{}", "user-1", 1000, 64, 50)
	inv, _ := store.CreateInvocation(ctx, sn.ID, v.ID, "prod", tenant.ID, `{}`)

	_ = store.UpdateInvocationResult(ctx, inv.ID, models.InvocationTimeout, "", "timeout", "", 1000, 0, 0)

	fetched, _ := store.GetInvocation(ctx, inv.ID)
	if fetched.Status != models.InvocationTimeout {
		t.Errorf("status = %q; want %q", fetched.Status, models.InvocationTimeout)
	}
}

func TestListInvocationsBySnippet(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	tenant, _ := store.CreateTenant(ctx, "List Inv Org", uniqueSlug(t, "listinv-org"))
	sn, _ := store.CreateSnippet(ctx, tenant.ID, "List Inv Snippet", "bun", "user-1")
	v, _ := store.CreateVersion(ctx, sn.ID, "code", "{}", "{}", "user-1", 5000, 128, 100)

	for i := 0; i < 5; i++ {
		_, err := store.CreateInvocation(ctx, sn.ID, v.ID, "prod", tenant.ID, `{}`)
		if err != nil {
			t.Fatalf("CreateInvocation %d: %v", i, err)
		}
	}

	results, err := store.ListInvocationsBySnippet(ctx, sn.ID, 3)
	if err != nil {
		t.Fatalf("ListInvocationsBySnippet: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("got %d results; want 3 (limit)", len(results))
	}
}

func TestGetInvocation_NotFound(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()

	_, err := store.GetInvocation(ctx, "00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Fatal("expected error for non-existent invocation, got nil")
	}
	if !errors.Is(err, postgres.ErrInvocationNotFound) {
		t.Fatalf("error = %v; want ErrInvocationNotFound", err)
	}
}
