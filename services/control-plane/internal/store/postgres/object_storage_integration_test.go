package postgres_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/abskrj/velane/services/control-plane/internal/models"
)

type memoryObjectStore struct {
	mu      sync.RWMutex
	objects map[string][]byte
	failPut bool
}

func newMemoryObjectStore() *memoryObjectStore {
	return &memoryObjectStore{objects: make(map[string][]byte)}
}

func (s *memoryObjectStore) Put(_ context.Context, key, _, _ string, body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failPut {
		return fmt.Errorf("object storage unavailable")
	}
	s.objects[key] = append([]byte(nil), body...)
	return nil
}

func (s *memoryObjectStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	body, ok := s.objects[key]
	if !ok {
		return nil, fmt.Errorf("object %q not found", key)
	}
	return append([]byte(nil), body...), nil
}

func (s *memoryObjectStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, key)
	return nil
}

func (s *memoryObjectStore) setFailPut(fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failPut = fail
}

func TestObjectBackedVersionAndInvocationRoundTrip(t *testing.T) {
	store := testStore(t)
	objects := newMemoryObjectStore()
	store.SetObjectStore(objects)

	ctx := context.Background()
	tenant, err := store.CreateTenant(ctx, "Object Storage Test", uniqueSlug(t, "object-storage"))
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	snippet, err := store.CreateSnippet(ctx, tenant.ID, "Object-backed workflow", "bun", "user-1")
	if err != nil {
		t.Fatalf("create snippet: %v", err)
	}
	version, err := store.CreateVersion(
		ctx, snippet.ID, "export default () => ({ok: true})",
		`{"type":"object"}`, `{"type":"object"}`, "user-1", 5000, 128, 100,
	)
	if err != nil {
		t.Fatalf("create version: %v", err)
	}

	byNumber, err := store.GetVersionByNumber(ctx, snippet.ID, version.VersionNumber)
	if err != nil {
		t.Fatalf("get version by number: %v", err)
	}
	if byNumber.Code != "export default () => ({ok: true})" {
		t.Fatalf("hydrated code = %q", byNumber.Code)
	}

	invocation, err := store.CreateInvocation(ctx, snippet.ID, version.ID, "dev", tenant.ID, `{"name":"test"}`)
	if err != nil {
		t.Fatalf("create invocation: %v", err)
	}
	objects.setFailPut(true)
	if err := store.UpdateInvocationResult(
		ctx, invocation.ID, models.InvocationCompleted, `{"ok":true}`, "", "debug", 12, 8, 3,
	); err != nil {
		t.Fatalf("update invocation: %v", err)
	}

	detail, err := store.GetInvocation(ctx, invocation.ID)
	if err != nil {
		t.Fatalf("get invocation: %v", err)
	}
	if detail.InputPayload != `{"name":"test"}` || detail.Output != `{"ok":true}` || detail.Stderr != "debug" {
		t.Fatalf("hydrated invocation payload mismatch: %#v", detail)
	}
	if detail.PayloadState != "failed" {
		t.Fatalf("payload state during outage = %q; want failed", detail.PayloadState)
	}

	objects.setFailPut(false)
	if err := store.MaintainObjectStorage(ctx); err != nil {
		t.Fatalf("retry payload outbox: %v", err)
	}
	detail, err = store.GetInvocation(ctx, invocation.ID)
	if err != nil {
		t.Fatalf("get invocation after retry: %v", err)
	}
	if detail.PayloadState != "stored" || detail.Output != `{"ok":true}` {
		t.Fatalf("retried invocation payload mismatch: %#v", detail)
	}

	items, err := store.ListInvocationsBySnippet(ctx, snippet.ID, 10)
	if err != nil {
		t.Fatalf("list invocations: %v", err)
	}
	if len(items) != 1 || items[0].PayloadState != "stored" {
		t.Fatalf("invocation summaries = %#v", items)
	}
}
