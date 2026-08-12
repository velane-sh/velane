package postgres_test

import (
	"context"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"testing"
)

func TestWorkflowTriggers_CRUDAndTenantIsolation(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	tenantA, _ := store.CreateTenant(ctx, "Trigger A", uniqueSlug(t, "trigger-a"))
	tenantB, _ := store.CreateTenant(ctx, "Trigger B", uniqueSlug(t, "trigger-b"))
	workflowA, _ := store.CreateSnippet(ctx, tenantA.ID, "Event workflow", "bun", "user-1")
	connA, _ := store.UpsertConnection(ctx, tenantA.ID, "salesforce", "default", "cfg-a", nil, "Salesforce")
	connB, _ := store.UpsertConnection(ctx, tenantB.ID, "salesforce", "default", "cfg-b", nil, "Salesforce")
	created, err := store.CreateWorkflowTrigger(ctx, &models.WorkflowTrigger{TenantID: tenantA.ID, WorkflowID: workflowA.ID, ConnectionID: connA.ID, ProviderConfigKey: connA.ProviderConfigKey, Model: "Case", ChangeTypes: []string{"added", "updated"}, Environment: "dev"})
	if err != nil {
		t.Fatalf("create trigger: %v", err)
	}
	if created.Enabled {
		t.Error("new trigger must be disabled")
	}
	rows, err := store.ListWorkflowTriggers(ctx, tenantA.ID, workflowA.ID)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list=%v err=%v", rows, err)
	}
	if _, err = store.CreateWorkflowTrigger(ctx, &models.WorkflowTrigger{TenantID: tenantB.ID, WorkflowID: workflowA.ID, ConnectionID: connB.ID, ProviderConfigKey: connB.ProviderConfigKey, Model: "Case", ChangeTypes: []string{"added"}, Environment: "dev"}); err == nil {
		t.Fatal("cross-tenant workflow reference was accepted")
	}
	if err = store.DeleteWorkflowTrigger(ctx, tenantB.ID, workflowA.ID, created.ID); err == nil {
		t.Fatal("cross-tenant delete was accepted")
	}
	if err = store.DeleteWorkflowTrigger(ctx, tenantA.ID, workflowA.ID, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestIntegrationEventReceipt_Deduplicates(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	tenant, _ := store.CreateTenant(ctx, "Receipt Tenant", uniqueSlug(t, "receipt"))
	conn, _ := store.UpsertConnection(ctx, tenant.ID, "salesforce", "default", "cfg", nil, "Salesforce")
	first := &models.IntegrationEventReceipt{DeduplicationKey: "event-key-" + uniqueSlug(t, "dedup"), TenantID: tenant.ID, ConnectionID: conn.ID, ProviderConfigKey: "cfg", Model: "Case", ModifiedAfter: "2026-08-03T10:00:00Z", Payload: []byte(`{"type":"sync"}`)}
	created, err := store.CreateIntegrationEventReceipt(ctx, first)
	if err != nil || !created {
		t.Fatalf("first created=%v err=%v", created, err)
	}
	duplicate := *first
	duplicate.ID = ""
	created, err = store.CreateIntegrationEventReceipt(ctx, &duplicate)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("duplicate receipt was inserted")
	}
	stored, err := store.GetIntegrationEventReceipt(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "pending" || stored.Attempts != 0 {
		t.Fatalf("stored status=%s attempts=%d", stored.Status, stored.Attempts)
	}
	if err = store.MarkIntegrationEvent(ctx, first.ID, "processing", ""); err != nil {
		t.Fatal(err)
	}
	stored, _ = store.GetIntegrationEventReceipt(ctx, first.ID)
	if stored.Attempts != 1 {
		t.Fatalf("attempts=%d, want 1", stored.Attempts)
	}
}
