package redisstore_test

import (
	"context"
	"os"
	"testing"
	"time"

	redisstore "github.com/abskrj/velane/services/control-plane/internal/store/redis"
)

func newTestClient(t *testing.T) *redisstore.Client {
	t.Helper()
	addr := os.Getenv("TEST_REDIS_URL")
	if addr == "" {
		t.Skip("TEST_REDIS_URL not set — skipping Redis integration test")
	}
	client, err := redisstore.New(addr)
	if err != nil {
		t.Fatalf("connect to test redis: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func TestQueue_EnqueueDequeue(t *testing.T) {
	client := newTestClient(t)

	job := redisstore.Job{
		InvocationID: "inv-abc",
		SnippetID:    "snip-1",
		VersionID:    "ver-1",
		TenantID:     "tenant-1",
		Language:     "bun",
		Code:         "export default async function handler() { return 42 }",
		Input:        `{"x":1}`,
		TimeoutMs:    5000,
		MaxMemoryMB:  128,
		CallbackURL:  "https://example.com/callback",
		Env:          "prod",
	}

	ctx := context.Background()

	if err := client.Enqueue(ctx, job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	// Dequeue with a short-lived context so the test doesn't hang.
	dCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	got, err := client.Dequeue(dCtx)
	if err != nil {
		t.Fatalf("Dequeue: %v", err)
	}
	if got == nil {
		t.Fatal("Dequeue returned nil job")
	}

	if got.InvocationID != job.InvocationID {
		t.Errorf("InvocationID = %q; want %q", got.InvocationID, job.InvocationID)
	}
	if got.SnippetID != job.SnippetID {
		t.Errorf("SnippetID = %q; want %q", got.SnippetID, job.SnippetID)
	}
	if got.Language != job.Language {
		t.Errorf("Language = %q; want %q", got.Language, job.Language)
	}
	if got.CallbackURL != job.CallbackURL {
		t.Errorf("CallbackURL = %q; want %q", got.CallbackURL, job.CallbackURL)
	}
	if got.Input != job.Input {
		t.Errorf("Input = %q; want %q", got.Input, job.Input)
	}
}

func TestQueue_DequeueContextCancelled(t *testing.T) {
	client := newTestClient(t)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay; nothing is enqueued so Dequeue would block forever.
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	job, err := client.Dequeue(ctx)
	if err != nil {
		// Some Redis clients propagate context errors; that is acceptable.
		t.Logf("Dequeue returned error on cancel (acceptable): %v", err)
		return
	}
	if job != nil {
		t.Error("expected nil job on cancelled context, got non-nil")
	}
}

func TestQueue_EnqueueMultiple(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()

	jobs := []redisstore.Job{
		{InvocationID: "inv-1", Language: "bun"},
		{InvocationID: "inv-2", Language: "python"},
		{InvocationID: "inv-3", Language: "bun"},
	}

	for _, j := range jobs {
		if err := client.Enqueue(ctx, j); err != nil {
			t.Fatalf("Enqueue %s: %v", j.InvocationID, err)
		}
	}

	seen := make(map[string]bool)
	for i := 0; i < len(jobs); i++ {
		dCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		got, err := client.Dequeue(dCtx)
		cancel()
		if err != nil {
			t.Fatalf("Dequeue %d: %v", i, err)
		}
		if got == nil {
			t.Fatalf("Dequeue %d returned nil", i)
		}
		seen[got.InvocationID] = true
	}

	for _, j := range jobs {
		if !seen[j.InvocationID] {
			t.Errorf("job %s was not dequeued", j.InvocationID)
		}
	}
}

func TestIntegrationEventQueue_EnqueueDequeue(t *testing.T) {
	client := newTestClient(t)
	ctx := context.Background()
	want := redisstore.IntegrationEventJob{ReceiptID: "receipt-1", Attempt: 2}
	if err := client.EnqueueIntegrationEvent(ctx, want); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	dequeueCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	got, err := client.DequeueIntegrationEvent(dequeueCtx)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if got == nil || got.ReceiptID != want.ReceiptID || got.Attempt != want.Attempt {
		t.Fatalf("job=%+v, want %+v", got, want)
	}
}
