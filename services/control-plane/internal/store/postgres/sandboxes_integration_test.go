package postgres

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/ids"
)

func sandboxIntegrationStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — skipping sandbox integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	store, err := New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store
}

type sandboxFixture struct {
	tenantID, recipeVersionID, profileVersionID string
}

func seedSandboxFixture(t *testing.T, store *Store, quota int) sandboxFixture {
	t.Helper()
	ctx := context.Background()
	suffix := ids.New()
	fixture := sandboxFixture{tenantID: ids.New(), recipeVersionID: ids.New(), profileVersionID: ids.New()}
	recipeID := ids.New()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO tenants(id,name,slug) VALUES($1,'Sandbox Test',$2)`, []any{fixture.tenantID, "sandbox-test-" + suffix}},
		{`INSERT INTO sandbox_image_recipes(id,tenant_id,name,slug) VALUES($1,$2,'Recipe',$3)`, []any{recipeID, fixture.tenantID, "recipe-" + suffix}},
		{`INSERT INTO sandbox_profile_versions(id,profile_family,name,version,vcpu,memory_mb,mutable_disk_layout,machine_config,device_topology,status,canonical_document,document_digest) VALUES($1,$2,'Profile','1',1,512,'[]','{}','{}','active','{}',$3)`, []any{fixture.profileVersionID, "family-" + suffix, "profile-digest-" + suffix}},
		{`INSERT INTO sandbox_image_recipe_versions(id,tenant_id,recipe_id,version_number,schema_version,document,document_digest,resolved_base_digest,status) VALUES($1,$2,$3,1,'1','{}',$4,$5,'ready')`, []any{fixture.recipeVersionID, fixture.tenantID, recipeID, "recipe-digest-" + suffix, "base-digest-" + suffix}},
		{`INSERT INTO sandbox_image_recipe_profile_artifacts(id,tenant_id,recipe_version_id,profile_version_id,artifact_ref,artifact_digest,artifact_size,vm_restore_descriptor_digest,status) VALUES($1,$2,$3,$4,'object','artifact',1,'descriptor','ready')`, []any{ids.New(), fixture.tenantID, fixture.recipeVersionID, fixture.profileVersionID}},
		{`INSERT INTO tenant_sandbox_quotas(tenant_id,max_sandboxes) VALUES($1,$2)`, []any{fixture.tenantID, quota}},
	}
	for _, statement := range statements {
		if _, err := store.pool.Exec(ctx, statement.query, statement.args...); err != nil {
			t.Fatalf("seed sandbox fixture: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = store.pool.Exec(context.Background(), `DELETE FROM tenants WHERE id=$1`, fixture.tenantID)
	})
	return fixture
}

func TestCreateSandboxIntentSerializesSameIdempotencyKey(t *testing.T) {
	store := sandboxIntegrationStore(t)
	fixture := seedSandboxFixture(t, store, 20)
	const workers = 20
	var successes atomic.Int32
	var failures atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := store.CreateSandboxIntent(context.Background(), CreateSandboxParams{TenantID: fixture.tenantID, Name: "same", RecipeVersionID: fixture.recipeVersionID, ProfileVersionID: fixture.profileVersionID, CreatedBy: "test"}, "same-key", "same-hash", "test", "user")
			if err != nil {
				failures.Add(1)
				return
			}
			successes.Add(1)
		}(i)
	}
	wg.Wait()
	if failures.Load() != 0 || successes.Load() != workers {
		t.Fatalf("successes=%d failures=%d, want %d/0", successes.Load(), failures.Load(), workers)
	}
	var sandboxes, operations int
	if err := store.pool.QueryRow(context.Background(), `SELECT count(*) FROM sandboxes WHERE tenant_id=$1`, fixture.tenantID).Scan(&sandboxes); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(context.Background(), `SELECT count(*) FROM sandbox_operations WHERE tenant_id=$1`, fixture.tenantID).Scan(&operations); err != nil {
		t.Fatal(err)
	}
	if sandboxes != 1 || operations != 1 {
		t.Fatalf("sandboxes=%d operations=%d, want 1/1", sandboxes, operations)
	}
}

func TestCreateSandboxIntentSerializesQuota(t *testing.T) {
	store := sandboxIntegrationStore(t)
	fixture := seedSandboxFixture(t, store, 3)
	const workers = 12
	var successes atomic.Int32
	var quotaFailures atomic.Int32
	var otherFailures atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := store.CreateSandboxIntent(context.Background(), CreateSandboxParams{TenantID: fixture.tenantID, Name: fmt.Sprintf("sandbox-%d", i), RecipeVersionID: fixture.recipeVersionID, ProfileVersionID: fixture.profileVersionID, CreatedBy: "test"}, fmt.Sprintf("key-%d", i), fmt.Sprintf("hash-%d", i), "test", "user")
			switch {
			case err == nil:
				successes.Add(1)
			case err.Error() == "quota exceeded":
				quotaFailures.Add(1)
			default:
				otherFailures.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if successes.Load() != 3 || quotaFailures.Load() != workers-3 || otherFailures.Load() != 0 {
		t.Fatalf("successes=%d quota=%d other=%d, want 3/%d/0", successes.Load(), quotaFailures.Load(), otherFailures.Load(), workers-3)
	}
}

func TestRequestSandboxOperationGenerationConflictRollsBack(t *testing.T) {
	store := sandboxIntegrationStore(t)
	fixture := seedSandboxFixture(t, store, 1)
	created, err := store.CreateSandboxIntent(context.Background(), CreateSandboxParams{TenantID: fixture.tenantID, Name: "sandbox", RecipeVersionID: fixture.recipeVersionID, ProfileVersionID: fixture.profileVersionID, CreatedBy: "test"}, "create-key", "create-hash", "test", "user")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(context.Background(), `UPDATE sandbox_operations SET state='succeeded' WHERE id=$1`, created.Operation.ID); err != nil {
		t.Fatal(err)
	}
	staleGeneration := int64(0)
	if _, err := store.RequestSandboxOperation(context.Background(), fixture.tenantID, created.Sandbox.ID, "stop", "stop-key", "stop-hash", "test", "user", &staleGeneration, nil, nil); err == nil || err.Error() != "generation conflict" {
		t.Fatalf("generation conflict error = %v", err)
	}
	loaded, err := store.GetSandbox(context.Background(), fixture.tenantID, created.Sandbox.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Generation != 1 || loaded.DesiredState != "running" {
		t.Fatalf("sandbox changed after rejected generation: generation=%d desired=%s", loaded.Generation, loaded.DesiredState)
	}
	var count int
	if err := store.pool.QueryRow(context.Background(), `SELECT count(*) FROM sandbox_operations WHERE tenant_id=$1 AND idempotency_key='stop-key'`, fixture.tenantID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rejected generation persisted %d operations", count)
	}
}

func TestRetrySandboxOperationPreservesFailedOperationKindAndTarget(t *testing.T) {
	store := sandboxIntegrationStore(t)
	fixture := seedSandboxFixture(t, store, 1)
	ctx := context.Background()
	created, err := store.CreateSandboxIntent(ctx, CreateSandboxParams{TenantID: fixture.tenantID, Name: "sandbox", RecipeVersionID: fixture.recipeVersionID, ProfileVersionID: fixture.profileVersionID, CreatedBy: "test"}, "create-key", "create-hash", "test", "user")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.pool.Exec(ctx, `UPDATE sandbox_operations SET state='failed',failure_code='BOOTSTRAP_FAILED' WHERE id=$1`, created.Operation.ID); err != nil {
		t.Fatal(err)
	}
	retry, err := store.RetrySandboxOperation(ctx, fixture.tenantID, created.Sandbox.ID, created.Operation.ID, "retry-key", "retry-hash", "test", "user", nil)
	if err != nil {
		t.Fatal(err)
	}
	if retry.Operation.Kind != "create" || retry.Operation.RetryOfOperationID == nil || *retry.Operation.RetryOfOperationID != created.Operation.ID {
		t.Fatalf("retry operation did not preserve parent semantics: %#v", retry.Operation)
	}
	if retry.Operation.SandboxID == nil || *retry.Operation.SandboxID != created.Sandbox.ID {
		t.Fatalf("retry operation sandbox_id = %v, want %s", retry.Operation.SandboxID, created.Sandbox.ID)
	}
	if _, err = store.RetrySandboxOperation(ctx, fixture.tenantID, created.Sandbox.ID, created.Operation.ID, "retry-key", "retry-hash", "test", "user", nil); err != nil {
		t.Fatalf("same retry idempotency key failed: %v", err)
	}
	if _, err = store.RetrySandboxOperation(ctx, fixture.tenantID, created.Sandbox.ID, created.Operation.ID, "retry-other-key", "retry-other-hash", "test", "user", nil); err == nil || err.Error() != "operation conflict" {
		t.Fatalf("second active retry error = %v, want operation conflict", err)
	}
}

func TestSandboxSchemaEnforcesOwnershipAndSnapshotImmutability(t *testing.T) {
	store := sandboxIntegrationStore(t)
	ctx := context.Background()
	first := seedSandboxFixture(t, store, 2)
	second := seedSandboxFixture(t, store, 2)

	firstSandbox, err := store.CreateSandbox(ctx, CreateSandboxParams{TenantID: first.tenantID, Name: "first", RecipeVersionID: first.recipeVersionID, ProfileVersionID: first.profileVersionID, VMRestoreDescriptorDigest: "descriptor"})
	if err != nil {
		t.Fatal(err)
	}
	secondSandbox, err := store.CreateSandbox(ctx, CreateSandboxParams{TenantID: first.tenantID, Name: "second", RecipeVersionID: first.recipeVersionID, ProfileVersionID: first.profileVersionID, VMRestoreDescriptorDigest: "descriptor"})
	if err != nil {
		t.Fatal(err)
	}
	operation, err := store.CreateSandboxOperation(ctx, CreateOperationParams{TenantID: first.tenantID, SandboxID: &firstSandbox.ID, Kind: "snapshot", IdempotencyKey: "snapshot-operation", RequestHash: "snapshot-operation"})
	if err != nil {
		t.Fatal(err)
	}
	snapshotID := ids.New()
	if _, err := store.pool.Exec(ctx, `INSERT INTO sandbox_snapshots(id,tenant_id,sandbox_id,operation_id,generation,kind,state,lineage_id,source_host_compatibility_key,vm_restore_descriptor_digest,snapshot_compatibility_key) VALUES($1,$2,$3,$4,1,'manual','requested',$5,$6,'descriptor','compatibility')`, snapshotID, first.tenantID, firstSandbox.ID, operation.ID, "lineage-"+snapshotID, "host-key-"+snapshotID); err == nil {
		t.Fatal("snapshot accepted a nonexistent lineage")
	}
	lineageID, lineageKey := "lineage-"+snapshotID, "host-key-"+snapshotID
	if _, err := store.pool.Exec(ctx, `INSERT INTO sandbox_host_lineages(id,provider,host_compatibility_key,host_compatibility_descriptor,descriptor_digest,status) VALUES($1,'test',$2,'{}',$3,'active')`, lineageID, lineageKey, "lineage-digest-"+snapshotID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO sandbox_snapshots(id,tenant_id,sandbox_id,operation_id,generation,kind,state,lineage_id,source_host_compatibility_key,vm_restore_descriptor_digest,snapshot_compatibility_key) VALUES($1,$2,$3,$4,1,'manual','requested',$5,$6,'descriptor','compatibility')`, snapshotID, first.tenantID, firstSandbox.ID, operation.ID, lineageID, lineageKey); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE sandboxes SET latest_snapshot_id=$1 WHERE id=$2`, snapshotID, secondSandbox.ID); err == nil {
		t.Fatal("sandbox accepted another sandbox's latest snapshot")
	}
	if _, err := store.pool.Exec(ctx, `UPDATE sandbox_snapshots SET lineage_id='changed' WHERE id=$1`, snapshotID); err == nil {
		t.Fatal("snapshot accepted a recovery lineage change")
	}
	if _, err := store.pool.Exec(ctx, `UPDATE sandbox_snapshots SET state='uploading',failure_code='TRANSIENT' WHERE id=$1`, snapshotID); err != nil {
		t.Fatalf("snapshot did not allow monotonic progress/failure update: %v", err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE sandbox_snapshots SET state='requested' WHERE id=$1`, snapshotID); err == nil {
		t.Fatal("snapshot accepted a backward state transition")
	}
	eventID := ids.New()
	if _, err := store.pool.Exec(ctx, `INSERT INTO sandbox_events(id,tenant_id,sandbox_id,operation_id,sequence,type,source,message) VALUES($1,$2,$3,$4,1,'test','test','test event')`, eventID, first.tenantID, firstSandbox.ID, operation.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE sandbox_events SET sandbox_id=$1 WHERE id=$2`, secondSandbox.ID, eventID); err == nil {
		t.Fatal("event ownership check was not enforced")
	}
	if _, err := store.pool.Exec(ctx, `UPDATE sandbox_events SET tenant_id=$1 WHERE id=$2`, second.tenantID, eventID); err == nil {
		t.Fatal("event accepted a tenant/sandbox mismatch")
	}
}

func TestSandboxMigrationReapplies(t *testing.T) {
	store := sandboxIntegrationStore(t)
	if _, err := store.pool.Exec(context.Background(), migrationSQL24); err != nil {
		t.Fatalf("reapplying sandbox migration: %v", err)
	}
}
