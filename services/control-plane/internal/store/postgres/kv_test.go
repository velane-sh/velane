package postgres_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestKVSetGetAndIdentity(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	tenant := newKVTestTenant(t, store, "identity")

	first, err := store.SetKV(ctx, tenant.ID, postgres.KVSetInput{Key: "profile", Value: json.RawMessage(`{"b":1,"a":2}`)})
	if err != nil {
		t.Fatalf("set first: %v", err)
	}
	got, err := store.GetKV(ctx, tenant.ID, "", "profile")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	assertJSONEqual(t, got.Value, `{"a":2,"b":1}`)
	if got.SizeBytes != first.SizeBytes {
		t.Fatalf("get size = %d; set size = %d", got.SizeBytes, first.SizeBytes)
	}

	whitespace, err := store.SetKV(ctx, tenant.ID, postgres.KVSetInput{Key: "whitespace", Value: json.RawMessage(` { "a" : 2, "b" : 1 } `)})
	if err != nil {
		t.Fatalf("set whitespace: %v", err)
	}
	if whitespace.SizeBytes != first.SizeBytes {
		t.Fatalf("canonical sizes differ: whitespace=%d compact=%d", whitespace.SizeBytes, first.SizeBytes)
	}

	time.Sleep(time.Millisecond)
	overwritten, err := store.SetKV(ctx, tenant.ID, postgres.KVSetInput{Key: "profile", Value: json.RawMessage(`null`)})
	if err != nil {
		t.Fatalf("overwrite: %v", err)
	}
	if overwritten.ID != first.ID || !overwritten.CreatedAt.Equal(first.CreatedAt) {
		t.Fatal("overwrite did not preserve row identity and created_at")
	}
	if overwritten.UpdatedAt.Before(first.UpdatedAt) {
		t.Fatal("overwrite moved updated_at backwards")
	}
	assertJSONEqual(t, overwritten.Value, `null`)

	withTTL := int64(60)
	if _, err := store.SetKV(ctx, tenant.ID, postgres.KVSetInput{Key: "ttl", Value: json.RawMessage(`1`), TTLSeconds: &withTTL}); err != nil {
		t.Fatalf("set TTL: %v", err)
	}
	cleared, err := store.SetKV(ctx, tenant.ID, postgres.KVSetInput{Key: "ttl", Value: json.RawMessage(`2`)})
	if err != nil {
		t.Fatalf("clear TTL: %v", err)
	}
	if cleared.ExpiresAt != nil {
		t.Fatalf("expires_at = %v; want nil after TTL omission", cleared.ExpiresAt)
	}

	defaultEntry, err := store.SetKV(ctx, tenant.ID, postgres.KVSetInput{Namespace: "", Key: "same", Value: json.RawMessage(`1`)})
	if err != nil {
		t.Fatalf("set default namespace: %v", err)
	}
	explicitDefault, err := store.SetKV(ctx, tenant.ID, postgres.KVSetInput{Namespace: "default", Key: "same", Value: json.RawMessage(`2`)})
	if err != nil {
		t.Fatalf("set explicit default namespace: %v", err)
	}
	if defaultEntry.ID != explicitDefault.ID {
		t.Fatal("empty and explicit default namespace created distinct rows")
	}
	if _, err := store.SetKV(ctx, tenant.ID, postgres.KVSetInput{Namespace: "sync", Key: "same", Value: json.RawMessage(`3`)}); err != nil {
		t.Fatalf("set sync namespace: %v", err)
	}
	if value, err := store.GetKV(ctx, tenant.ID, "sync", "same"); err != nil || string(value.Value) != "3" {
		t.Fatalf("get sync value = %v, %v", value, err)
	}

	deletedID, err := store.DeleteKV(ctx, tenant.ID, "", "same")
	if err != nil || deletedID != defaultEntry.ID {
		t.Fatalf("delete = (%q, %v); want (%q, nil)", deletedID, err, defaultEntry.ID)
	}
	if id, err := store.DeleteKV(ctx, tenant.ID, "", "missing"); !errors.Is(err, postgres.ErrKVNotFound) || id != "" {
		t.Fatalf("delete missing = (%q, %v); want empty ID and ErrKVNotFound", id, err)
	}
}

func TestKVTenantIsolationAndListRedaction(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	a := newKVTestTenant(t, store, "tenant-a")
	b := newKVTestTenant(t, store, "tenant-b")
	entry, err := store.SetKV(ctx, a.ID, postgres.KVSetInput{Key: "shared", Value: json.RawMessage(`{"a":1}`)})
	if err != nil {
		t.Fatalf("set A: %v", err)
	}
	if _, err := store.GetKV(ctx, b.ID, "", "shared"); !errors.Is(err, postgres.ErrKVNotFound) {
		t.Fatalf("B get A row: %v; want ErrKVNotFound", err)
	}
	if id, err := store.DeleteKV(ctx, b.ID, "", "shared"); !errors.Is(err, postgres.ErrKVNotFound) || id != "" {
		t.Fatalf("B delete A row = (%q, %v)", id, err)
	}
	items, total, err := store.ListKV(ctx, b.ID, postgres.KVListFilters{})
	if err != nil || total != 0 || len(items) != 0 {
		t.Fatalf("B list = (%d items, %d total, %v); want empty", len(items), total, err)
	}
	bEntry, err := store.SetKV(ctx, b.ID, postgres.KVSetInput{Key: "shared", Value: json.RawMessage(`{"b":2}`)})
	if err != nil {
		t.Fatalf("set B: %v", err)
	}
	if bEntry.ID == entry.ID {
		t.Fatal("same key in different tenants reused a row")
	}
	items, total, err = store.ListKV(ctx, a.ID, postgres.KVListFilters{})
	if err != nil || total != 1 || len(items) != 1 || items[0].Value != nil || items[0].SizeBytes <= 0 {
		t.Fatalf("A list redaction = %#v, total=%d, err=%v", items, total, err)
	}
}

func TestKVExpiryAndReaper(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	tenant := newKVTestTenant(t, store, "expiry")
	for _, key := range []string{"expired-1", "expired-2", "expired-3", "live"} {
		if _, err := store.SetKV(ctx, tenant.ID, postgres.KVSetInput{Key: key, Value: json.RawMessage(`"v"`)}); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}
	setKVExpired(t, tenant.ID, []string{"expired-1", "expired-2", "expired-3"})
	if _, err := store.GetKV(ctx, tenant.ID, "", "expired-1"); !errors.Is(err, postgres.ErrKVNotFound) {
		t.Fatalf("get expired: %v", err)
	}
	items, total, err := store.ListKV(ctx, tenant.ID, postgres.KVListFilters{})
	if err != nil || total != 1 || len(items) != 1 || items[0].Key != "live" {
		t.Fatalf("list live entries = %#v, %d, %v", items, total, err)
	}
	usage, _, err := store.CountKV(ctx, tenant.ID)
	if err != nil || usage.Keys != 1 {
		t.Fatalf("live usage = %+v, %v", usage, err)
	}
	namespaces, err := store.ListKVNamespaces(ctx, tenant.ID)
	if err != nil || len(namespaces) != 1 || namespaces[0].Keys != 1 {
		t.Fatalf("live namespace usage = %#v, %v", namespaces, err)
	}

	deleted, err := store.DeleteExpiredKV(ctx, 2, 1)
	if err != nil || deleted != 2 {
		t.Fatalf("first reap = %d, %v; want 2", deleted, err)
	}
	deleted, err = store.DeleteExpiredKV(ctx, 2, 2)
	if err != nil || deleted != 1 {
		t.Fatalf("second reap = %d, %v; want 1", deleted, err)
	}
	if countKVRows(t, tenant.ID) != 1 {
		t.Fatalf("physical rows after reap = %d; want 1", countKVRows(t, tenant.ID))
	}

	if _, err := store.SetKV(ctx, tenant.ID, postgres.KVSetInput{Key: "reset", Value: json.RawMessage(`1`)}); err != nil {
		t.Fatalf("set reset: %v", err)
	}
	setKVExpired(t, tenant.ID, []string{"reset"})
	if _, err := store.SetKV(ctx, tenant.ID, postgres.KVSetInput{Key: "reset", Value: json.RawMessage(`2`)}); err != nil {
		t.Fatalf("upsert expired row: %v", err)
	}
	if _, err := store.GetKV(ctx, tenant.ID, "", "reset"); err != nil {
		t.Fatalf("get reset entry: %v", err)
	}
	if _, err := store.DeleteExpiredKV(ctx, 0, 0); err != nil {
		t.Fatalf("clamped reaper args: %v", err)
	}
}

func TestKVQuotasAndTenantLimits(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	tenant := newKVTestTenant(t, store, "quotas")

	if _, err := store.SetKV(ctx, tenant.ID, postgres.KVSetInput{Key: "invalid", Value: json.RawMessage(`not-json`)}); !errors.Is(err, postgres.ErrKVInvalidValue) {
		t.Fatalf("invalid JSON error = %v; want ErrKVInvalidValue", err)
	}
	setKVLimits(t, tenant.ID, models.KVLimits{MaxKeys: 2, MaxValueBytes: 4, MaxTotalBytes: 8})
	if _, err := store.SetKV(ctx, tenant.ID, postgres.KVSetInput{Key: "exact", Value: json.RawMessage(`"aa"`)}); err != nil {
		t.Fatalf("exact value cap: %v", err)
	}
	if _, err := store.SetKV(ctx, tenant.ID, postgres.KVSetInput{Key: "large", Value: json.RawMessage(`"aaa"`)}); !errors.Is(err, postgres.ErrKVValueTooLarge) {
		t.Fatalf("large value error = %v; want ErrKVValueTooLarge", err)
	}
	if _, err := store.SetKV(ctx, tenant.ID, postgres.KVSetInput{Key: "second", Value: json.RawMessage(`"aa"`)}); err != nil {
		t.Fatalf("second at byte cap: %v", err)
	}
	if _, err := store.SetKV(ctx, tenant.ID, postgres.KVSetInput{Key: "third", Value: json.RawMessage(`"a"`)}); !errors.Is(err, postgres.ErrKVKeyQuota) {
		t.Fatalf("key quota error = %v; want ErrKVKeyQuota", err)
	}
	if _, err := store.SetKV(ctx, tenant.ID, postgres.KVSetInput{Key: "exact", Value: json.RawMessage(`"a"`)}); err != nil {
		t.Fatalf("overwrite at key cap: %v", err)
	}

	setKVLimits(t, tenant.ID, models.KVLimits{MaxKeys: 10, MaxValueBytes: 10, MaxTotalBytes: 7})
	if _, err := store.SetKV(ctx, tenant.ID, postgres.KVSetInput{Key: "bytes", Value: json.RawMessage(`"aaa"`)}); !errors.Is(err, postgres.ErrKVBytesQuota) {
		t.Fatalf("bytes quota error = %v; want ErrKVBytesQuota", err)
	}
	setKVLimitsJSON(t, tenant.ID, `{}`)
	limits, err := store.KVLimitsForTenant(ctx, tenant.ID)
	if err != nil || limits != models.DefaultKVLimits() {
		t.Fatalf("normalized empty limits = %+v, %v", limits, err)
	}
	if _, err := store.KVLimitsForTenant(ctx, "missing-tenant"); !errors.Is(err, postgres.ErrKVTenantNotFound) {
		t.Fatalf("missing tenant limits: %v", err)
	}
}

func TestKVConcurrentQuotaSerialization(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	tenant := newKVTestTenant(t, store, "concurrent")
	setKVLimits(t, tenant.ID, models.KVLimits{MaxKeys: 1, MaxValueBytes: 20, MaxTotalBytes: 20})

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, key := range []string{"one", "two"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			<-start
			_, err := store.SetKV(context.Background(), tenant.ID, postgres.KVSetInput{Key: key, Value: json.RawMessage(`"v"`)})
			results <- err
		}(key)
	}
	close(start)
	wg.Wait()
	close(results)
	var successes, quotaErrors int
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, postgres.ErrKVKeyQuota) {
			quotaErrors++
		} else {
			t.Fatalf("concurrent write error: %v", err)
		}
	}
	if successes != 1 || quotaErrors != 1 || countKVRows(t, tenant.ID) != 1 {
		t.Fatalf("concurrent results: successes=%d quota=%d rows=%d", successes, quotaErrors, countKVRows(t, tenant.ID))
	}

	bytesTenant := newKVTestTenant(t, store, "concurrent-bytes")
	setKVLimits(t, bytesTenant.ID, models.KVLimits{MaxKeys: 10, MaxValueBytes: 5, MaxTotalBytes: 5})
	start = make(chan struct{})
	results = make(chan error, 2)
	for _, key := range []string{"one", "two"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			<-start
			_, err := store.SetKV(context.Background(), bytesTenant.ID, postgres.KVSetInput{Key: key, Value: json.RawMessage(`"abc"`)})
			results <- err
		}(key)
	}
	close(start)
	wg.Wait()
	close(results)
	successes, quotaErrors = 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, postgres.ErrKVBytesQuota) {
			quotaErrors++
		} else {
			t.Fatalf("concurrent byte write error: %v", err)
		}
	}
	if successes != 1 || quotaErrors != 1 {
		t.Fatalf("concurrent byte results: successes=%d quota=%d", successes, quotaErrors)
	}
	usage, _, err := store.CountKV(ctx, bytesTenant.ID)
	if err != nil || usage.SizeBytes > 5 {
		t.Fatalf("concurrent byte usage = %+v, %v", usage, err)
	}
}

func TestKVConcurrentReaper(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	tenant := newKVTestTenant(t, store, "concurrent-reaper")
	keys := []string{"one", "two", "three", "four"}
	for _, key := range keys {
		if _, err := store.SetKV(ctx, tenant.ID, postgres.KVSetInput{Key: key, Value: json.RawMessage(`1`)}); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}
	setKVExpired(t, tenant.ID, keys)

	start := make(chan struct{})
	results := make(chan struct {
		count int64
		err   error
	}, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			count, err := store.DeleteExpiredKV(context.Background(), 2, 2)
			results <- struct {
				count int64
				err   error
			}{count, err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	var deleted int64
	for result := range results {
		if result.err != nil {
			t.Fatalf("concurrent reaper error: %v", result.err)
		}
		deleted += result.count
	}
	if deleted != int64(len(keys)) || countKVRows(t, tenant.ID) != 0 {
		t.Fatalf("concurrent reaper deleted=%d rows=%d; want %d/0", deleted, countKVRows(t, tenant.ID), len(keys))
	}
}

func TestKVListLiteralPrefixesNamespacesAndPagination(t *testing.T) {
	store := testStore(t)
	ctx := context.Background()
	tenant := newKVTestTenant(t, store, "list")
	allPrefix := "x%_\\"
	entries := []struct{ namespace, key string }{
		{"default", "50%off"}, {"default", "a_b"}, {"default", "ab"}, {"default", `back\slash`},
		{"sync", "a"}, {"sync", "b"}, {"sync", allPrefix + "tail"},
	}
	for _, entry := range entries {
		if _, err := store.SetKV(ctx, tenant.ID, postgres.KVSetInput{Namespace: entry.namespace, Key: entry.key, Value: json.RawMessage(`1`)}); err != nil {
			t.Fatalf("set %s/%s: %v", entry.namespace, entry.key, err)
		}
	}
	assertListKeys(t, store, tenant.ID, postgres.KVListFilters{KeyPrefix: "50%"}, []string{"50%off"})
	assertListKeys(t, store, tenant.ID, postgres.KVListFilters{KeyPrefix: "a_"}, []string{"a_b"})
	assertListKeys(t, store, tenant.ID, postgres.KVListFilters{KeyPrefix: `back\`}, []string{`back\slash`})
	assertListKeys(t, store, tenant.ID, postgres.KVListFilters{KeyPrefix: allPrefix}, []string{allPrefix + "tail"})

	all, total, err := store.ListKV(ctx, tenant.ID, postgres.KVListFilters{Limit: 200})
	if err != nil || total != int64(len(entries)) || len(all) != len(entries) {
		t.Fatalf("all namespace list = %d/%d, %v", len(all), total, err)
	}
	defaultNS := "default"
	defaults, defaultTotal, err := store.ListKV(ctx, tenant.ID, postgres.KVListFilters{Namespace: &defaultNS, Limit: 200})
	if err != nil || defaultTotal != 4 || len(defaults) != 4 {
		t.Fatalf("default namespace list = %d/%d, %v", len(defaults), defaultTotal, err)
	}
	seen := make([]string, 0, len(entries))
	for offset := 0; offset < len(entries); offset++ {
		page, _, err := store.ListKV(ctx, tenant.ID, postgres.KVListFilters{Limit: 1, Offset: offset})
		if err != nil || len(page) != 1 {
			t.Fatalf("page at offset %d: %#v, %v", offset, page, err)
		}
		seen = append(seen, page[0].Namespace+"/"+page[0].Key)
	}
	if len(seen) != len(entries) || reflect.DeepEqual(seen, []string{}) {
		t.Fatalf("pagination returned invalid sequence: %#v", seen)
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] <= seen[i-1] {
			t.Fatalf("pagination order not stable: %#v", seen)
		}
	}
}

func newKVTestTenant(t *testing.T, store *postgres.Store, base string) *models.Tenant {
	t.Helper()
	tenant, err := store.CreateTenant(context.Background(), "KV "+base, uniqueSlug(t, "kv-"+base))
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	return tenant
}

func assertJSONEqual(t *testing.T, got []byte, want string) {
	t.Helper()
	var gotValue, wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("unmarshal got JSON %q: %v", got, err)
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("unmarshal want JSON %q: %v", want, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("JSON = %#v; want %#v", gotValue, wantValue)
	}
}

func assertListKeys(t *testing.T, store *postgres.Store, tenantID string, filter postgres.KVListFilters, want []string) {
	t.Helper()
	items, total, err := store.ListKV(context.Background(), tenantID, filter)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.Key)
	}
	if total != int64(len(want)) || !reflect.DeepEqual(keys, want) {
		t.Fatalf("list keys = %#v, total=%d; want %#v", keys, total, want)
	}
}

func kvTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatalf("open test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func setKVLimits(t *testing.T, tenantID string, limits models.KVLimits) {
	t.Helper()
	raw, err := json.Marshal(limits)
	if err != nil {
		t.Fatalf("marshal limits: %v", err)
	}
	setKVLimitsJSON(t, tenantID, string(raw))
}

func setKVLimitsJSON(t *testing.T, tenantID, raw string) {
	t.Helper()
	if _, err := kvTestPool(t).Exec(context.Background(), `UPDATE tenants SET kv_limits = $2::jsonb WHERE id = $1`, tenantID, raw); err != nil {
		t.Fatalf("update kv limits: %v", err)
	}
}

func setKVExpired(t *testing.T, tenantID string, keys []string) {
	t.Helper()
	if _, err := kvTestPool(t).Exec(context.Background(), `UPDATE kv_entries SET expires_at = now() - interval '1 second' WHERE tenant_id = $1 AND key = ANY($2)`, tenantID, keys); err != nil {
		t.Fatalf("expire keys: %v", err)
	}
}

func countKVRows(t *testing.T, tenantID string) int64 {
	t.Helper()
	var count int64
	if err := kvTestPool(t).QueryRow(context.Background(), `SELECT COUNT(*) FROM kv_entries WHERE tenant_id = $1`, tenantID).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}
