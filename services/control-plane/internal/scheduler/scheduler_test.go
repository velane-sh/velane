package scheduler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/executor"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/abskrj/velane/services/control-plane/internal/scheduler"
	redisstore "github.com/abskrj/velane/services/control-plane/internal/store/redis"
)

// testEncKey is a zeroed 32-byte key used in scheduler unit tests.
var testEncKey = make([]byte, 32)

// --- Mocks ---

type mockStore struct {
	getSnippetByID           func(ctx context.Context, id string) (*models.Snippet, error)
	getSnippetBySlug         func(ctx context.Context, tenantID, slug string) (*models.Snippet, error)
	getSnippetEnvironment    func(ctx context.Context, snippetID, env string) (*models.SnippetEnvironment, error)
	getVersion               func(ctx context.Context, id string) (*models.SnippetVersion, error)
	getVersionByNumber       func(ctx context.Context, snippetID string, num int) (*models.SnippetVersion, error)
	createInvocation         func(ctx context.Context, snippetID, versionID, env, tenantID, input string) (*models.Invocation, error)
	createInvocationWithMode func(ctx context.Context, snippetID, versionID, environment, tenantID, inputPayload, invokeMode, callbackURL string, status models.InvocationStatus) (*models.Invocation, error)
	updateInvocationResult   func(ctx context.Context, id string, status models.InvocationStatus, output, errMsg, stderr string, durationMs, peakMemoryMB, cpuMs int) error
	getInvocation            func(ctx context.Context, id string) (*models.Invocation, error)
	getSecretsForInvocation  func(ctx context.Context, tenantID, snippetID, env string, encKey []byte) (map[string]string, error)
	getTenantByID            func(ctx context.Context, id string) (*models.Tenant, error)
}

func (m *mockStore) GetSnippetByID(ctx context.Context, id string) (*models.Snippet, error) {
	if m.getSnippetByID != nil {
		return m.getSnippetByID(ctx, id)
	}
	return nil, errors.New("not found")
}
func (m *mockStore) GetSnippetBySlug(ctx context.Context, tenantID, slug string) (*models.Snippet, error) {
	return m.getSnippetBySlug(ctx, tenantID, slug)
}
func (m *mockStore) GetSnippetEnvironment(ctx context.Context, snippetID, env string) (*models.SnippetEnvironment, error) {
	return m.getSnippetEnvironment(ctx, snippetID, env)
}
func (m *mockStore) GetVersion(ctx context.Context, id string) (*models.SnippetVersion, error) {
	return m.getVersion(ctx, id)
}
func (m *mockStore) GetVersionByNumber(ctx context.Context, snippetID string, num int) (*models.SnippetVersion, error) {
	if m.getVersionByNumber != nil {
		return m.getVersionByNumber(ctx, snippetID, num)
	}
	return nil, errors.New("GetVersionByNumber not implemented in mock")
}
func (m *mockStore) CreateInvocation(ctx context.Context, snippetID, versionID, env, tenantID, input string) (*models.Invocation, error) {
	return m.createInvocation(ctx, snippetID, versionID, env, tenantID, input)
}
func (m *mockStore) CreateInvocationWithMode(ctx context.Context, snippetID, versionID, environment, tenantID, inputPayload, invokeMode, callbackURL string, status models.InvocationStatus) (*models.Invocation, error) {
	if m.createInvocationWithMode != nil {
		return m.createInvocationWithMode(ctx, snippetID, versionID, environment, tenantID, inputPayload, invokeMode, callbackURL, status)
	}
	// Fall back to createInvocation for backward compatibility in existing tests.
	return m.createInvocation(ctx, snippetID, versionID, environment, tenantID, inputPayload)
}
func (m *mockStore) UpdateInvocationResult(ctx context.Context, id string, status models.InvocationStatus, output, errMsg, stderr string, durationMs, peakMemoryMB, cpuMs int) error {
	return m.updateInvocationResult(ctx, id, status, output, errMsg, stderr, durationMs, peakMemoryMB, cpuMs)
}
func (m *mockStore) GetInvocation(ctx context.Context, id string) (*models.Invocation, error) {
	return m.getInvocation(ctx, id)
}
func (m *mockStore) GetSecretsForInvocation(ctx context.Context, tenantID, snippetID, env string, encKey []byte) (map[string]string, error) {
	if m.getSecretsForInvocation != nil {
		return m.getSecretsForInvocation(ctx, tenantID, snippetID, env, encKey)
	}
	return map[string]string{}, nil
}
func (m *mockStore) GetTenantByID(ctx context.Context, id string) (*models.Tenant, error) {
	if m.getTenantByID != nil {
		return m.getTenantByID(ctx, id)
	}
	return &models.Tenant{
		ID:   id,
		Name: "Test Tenant",
		Slug: "test-tenant",
		EgressPolicy: models.EgressPolicy{
			BlockedCIDRs:   []string{},
			BlockedDomains: []string{},
		},
	}, nil
}
func (m *mockStore) GetTenantLibrariesForInvocation(ctx context.Context, tenantID, tenantSlug, language string) (map[string]string, error) {
	return map[string]string{}, nil
}

// mockExecutor implements executor.Executor.
type mockExecutor struct {
	run       func(ctx context.Context, spec executor.RunSpec) executor.RunResult
	runStream func(ctx context.Context, spec executor.RunSpec) (<-chan executor.StreamChunk, error)
}

func (m *mockExecutor) Run(ctx context.Context, spec executor.RunSpec) executor.RunResult {
	if m.run != nil {
		return m.run(ctx, spec)
	}
	return executor.RunResult{}
}

func (m *mockExecutor) RunStream(ctx context.Context, spec executor.RunSpec) (<-chan executor.StreamChunk, error) {
	if m.runStream != nil {
		return m.runStream(ctx, spec)
	}
	ch := make(chan executor.StreamChunk)
	close(ch)
	return ch, nil
}

// mockQueue captures Enqueue calls.
type mockQueue struct {
	jobs []redisstore.Job
}

func (m *mockQueue) Enqueue(ctx context.Context, job redisstore.Job) error {
	m.jobs = append(m.jobs, job)
	return nil
}

// --- Fixtures ---

func fixSnippet() *models.Snippet {
	return &models.Snippet{
		ID:       "snip-1",
		TenantID: "tenant-1",
		Slug:     "hello",
		Language: models.LanguageBun,
	}
}

func fixVersion(id string) *models.SnippetVersion {
	return &models.SnippetVersion{
		ID:            id,
		SnippetID:     "snip-1",
		VersionNumber: 1,
		Code:          `export async function handler() { return {ok:true} }`,
		TimeoutMs:     5000,
		MaxMemoryMB:   128,
		Status:        models.StatusPublished,
	}
}

func fixInvocation(id, versionID string) *models.Invocation {
	now := time.Now()
	return &models.Invocation{
		ID:          id,
		SnippetID:   "snip-1",
		VersionID:   versionID,
		Environment: "prod",
		TenantID:    "tenant-1",
		Status:      models.InvocationRunning,
		InvokeMode:  "sync",
		CreatedAt:   now,
	}
}

// buildDefaultStore creates a mockStore pre-filled with happy-path behaviour.
// Callers can override individual fields after calling this.
func buildDefaultStore(invocationID, versionID string, activeVersion *string) *mockStore {
	return &mockStore{
		getSnippetBySlug: func(ctx context.Context, tenantID, slug string) (*models.Snippet, error) {
			return fixSnippet(), nil
		},
		getSnippetEnvironment: func(ctx context.Context, snippetID, env string) (*models.SnippetEnvironment, error) {
			return &models.SnippetEnvironment{
				SnippetID:       snippetID,
				Env:             env,
				ActiveVersionID: activeVersion,
			}, nil
		},
		getVersion: func(ctx context.Context, id string) (*models.SnippetVersion, error) {
			return fixVersion(id), nil
		},
		getVersionByNumber: func(ctx context.Context, snippetID string, num int) (*models.SnippetVersion, error) {
			v := fixVersion(versionID)
			v.VersionNumber = num
			return v, nil
		},
		createInvocation: func(ctx context.Context, snippetID, versionID, env, tenantID, input string) (*models.Invocation, error) {
			return fixInvocation(invocationID, versionID), nil
		},
		createInvocationWithMode: func(ctx context.Context, snippetID, versionID, environment, tenantID, inputPayload, invokeMode, callbackURL string, status models.InvocationStatus) (*models.Invocation, error) {
			inv := fixInvocation(invocationID, versionID)
			inv.Status = status
			inv.InvokeMode = invokeMode
			inv.CallbackURL = callbackURL
			return inv, nil
		},
		updateInvocationResult: func(ctx context.Context, id string, status models.InvocationStatus, output, errMsg, stderr string, durationMs, peakMemoryMB, cpuMs int) error {
			return nil
		},
		getInvocation: func(ctx context.Context, id string) (*models.Invocation, error) {
			inv := fixInvocation(invocationID, versionID)
			inv.Status = models.InvocationCompleted
			inv.Output = `{"ok":true}`
			inv.DurationMs = 42
			return inv, nil
		},
	}
}

// --- Sync Invoke tests ---

func TestScheduler_Invoke_Success(t *testing.T) {
	versionID := "ver-1"
	invocationID := "inv-1"
	activeVersion := versionID

	store := buildDefaultStore(invocationID, versionID, &activeVersion)
	exec := &mockExecutor{
		run: func(ctx context.Context, spec executor.RunSpec) executor.RunResult {
			return executor.RunResult{Output: `{"ok":true}`, DurationMs: 42, ExitCode: 0}
		},
	}

	sched := scheduler.New(store, exec, testEncKey, nil)
	inv, err := sched.Invoke(context.Background(), scheduler.InvokeRequest{
		TenantID:    "tenant-1",
		SnippetSlug: "hello",
		Env:         "prod",
		Input:       `{}`,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv.Status != models.InvocationCompleted {
		t.Errorf("status = %q; want %q", inv.Status, models.InvocationCompleted)
	}
	if inv.Output != `{"ok":true}` {
		t.Errorf("output = %q; want %q", inv.Output, `{"ok":true}`)
	}
}

func TestScheduler_Invoke_SnippetNotFound(t *testing.T) {
	store := &mockStore{
		getSnippetBySlug: func(ctx context.Context, tenantID, slug string) (*models.Snippet, error) {
			return nil, errors.New("not found")
		},
	}

	sched := scheduler.New(store, &mockExecutor{}, testEncKey, nil)
	_, err := sched.Invoke(context.Background(), scheduler.InvokeRequest{
		TenantID:    "tenant-1",
		SnippetSlug: "missing",
		Env:         "prod",
	})

	if err == nil {
		t.Fatal("expected error for missing snippet, got nil")
	}
}

func TestScheduler_Invoke_NoPublishedVersion(t *testing.T) {
	store := &mockStore{
		getSnippetBySlug: func(ctx context.Context, tenantID, slug string) (*models.Snippet, error) {
			return fixSnippet(), nil
		},
		getSnippetEnvironment: func(ctx context.Context, snippetID, env string) (*models.SnippetEnvironment, error) {
			return &models.SnippetEnvironment{
				SnippetID:       snippetID,
				Env:             env,
				ActiveVersionID: nil, // nothing published
			}, nil
		},
	}

	sched := scheduler.New(store, &mockExecutor{}, testEncKey, nil)
	_, err := sched.Invoke(context.Background(), scheduler.InvokeRequest{
		TenantID:    "tenant-1",
		SnippetSlug: "hello",
		Env:         "prod",
	})

	if err == nil {
		t.Fatal("expected error when no version is published, got nil")
	}
}

func TestScheduler_Invoke_ExecutorTimeout(t *testing.T) {
	versionID := "ver-1"
	invocationID := "inv-1"
	activeVersion := versionID

	var capturedStatus models.InvocationStatus
	store := buildDefaultStore(invocationID, versionID, &activeVersion)
	store.updateInvocationResult = func(ctx context.Context, id string, status models.InvocationStatus, output, errMsg, stderr string, durationMs, peakMemoryMB, cpuMs int) error {
		capturedStatus = status
		return nil
	}
	store.getInvocation = func(ctx context.Context, id string) (*models.Invocation, error) {
		inv := fixInvocation(invocationID, versionID)
		inv.Status = capturedStatus
		return inv, nil
	}

	exec := &mockExecutor{
		run: func(ctx context.Context, spec executor.RunSpec) executor.RunResult {
			return executor.RunResult{Error: "timeout", ExitCode: 1}
		},
	}

	sched := scheduler.New(store, exec, testEncKey, nil)
	inv, err := sched.Invoke(context.Background(), scheduler.InvokeRequest{
		TenantID:    "tenant-1",
		SnippetSlug: "hello",
		Env:         "prod",
		Input:       `{}`,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv.Status != models.InvocationTimeout {
		t.Errorf("status = %q; want %q", inv.Status, models.InvocationTimeout)
	}
}

func TestScheduler_Invoke_ExecutorOOM(t *testing.T) {
	versionID := "ver-1"
	invocationID := "inv-1"
	activeVersion := versionID

	var capturedStatus models.InvocationStatus
	store := buildDefaultStore(invocationID, versionID, &activeVersion)
	store.updateInvocationResult = func(ctx context.Context, id string, status models.InvocationStatus, output, errMsg, stderr string, durationMs, peakMemoryMB, cpuMs int) error {
		capturedStatus = status
		return nil
	}
	store.getInvocation = func(ctx context.Context, id string) (*models.Invocation, error) {
		inv := fixInvocation(invocationID, versionID)
		inv.Status = capturedStatus
		return inv, nil
	}

	exec := &mockExecutor{
		run: func(ctx context.Context, spec executor.RunSpec) executor.RunResult {
			return executor.RunResult{Error: "oom", ExitCode: 137}
		},
	}

	sched := scheduler.New(store, exec, testEncKey, nil)
	inv, err := sched.Invoke(context.Background(), scheduler.InvokeRequest{
		TenantID: "tenant-1", SnippetSlug: "hello", Env: "prod", Input: `{}`,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv.Status != models.InvocationOOMKilled {
		t.Errorf("status = %q; want %q", inv.Status, models.InvocationOOMKilled)
	}
}

func TestScheduler_Invoke_DefaultsEmptyInputToEmptyObject(t *testing.T) {
	versionID := "ver-1"
	invocationID := "inv-1"
	activeVersion := versionID

	var capturedInput string
	store := buildDefaultStore(invocationID, versionID, &activeVersion)
	store.createInvocation = func(ctx context.Context, snippetID, versionID, env, tenantID, input string) (*models.Invocation, error) {
		capturedInput = input
		return fixInvocation(invocationID, versionID), nil
	}

	exec := &mockExecutor{
		run: func(ctx context.Context, spec executor.RunSpec) executor.RunResult {
			return executor.RunResult{Output: `{}`, ExitCode: 0}
		},
	}

	sched := scheduler.New(store, exec, testEncKey, nil)
	_, err := sched.Invoke(context.Background(), scheduler.InvokeRequest{
		TenantID: "tenant-1", SnippetSlug: "hello", Env: "prod",
		Input: "", // empty — should default to "{}"
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedInput != "{}" {
		t.Errorf("capturedInput = %q; want %q", capturedInput, "{}")
	}
}

// --- Version pinning tests ---

func TestScheduler_Invoke_PinnedVersion(t *testing.T) {
	versionID := "ver-pinned"
	invocationID := "inv-pin-1"
	activeVersion := "ver-active" // different from pinned

	var capturedVersionID string
	store := buildDefaultStore(invocationID, versionID, &activeVersion)
	store.getVersionByNumber = func(ctx context.Context, snippetID string, num int) (*models.SnippetVersion, error) {
		if num != 2 {
			return nil, errors.New("wrong version number")
		}
		v := fixVersion(versionID)
		v.VersionNumber = 2
		v.Code = "pinned-code"
		return v, nil
	}
	store.createInvocation = func(ctx context.Context, snippetID, vid, env, tenantID, input string) (*models.Invocation, error) {
		capturedVersionID = vid
		return fixInvocation(invocationID, vid), nil
	}

	exec := &mockExecutor{
		run: func(ctx context.Context, spec executor.RunSpec) executor.RunResult {
			return executor.RunResult{Output: `{}`, ExitCode: 0}
		},
	}

	sched := scheduler.New(store, exec, testEncKey, nil)
	_, err := sched.Invoke(context.Background(), scheduler.InvokeRequest{
		TenantID:      "tenant-1",
		SnippetSlug:   "hello",
		Env:           "prod",
		Input:         `{}`,
		PinnedVersion: 2,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedVersionID != versionID {
		t.Errorf("version ID = %q; want %q (pinned)", capturedVersionID, versionID)
	}
}

func TestScheduler_Invoke_PinnedVersionNotFound(t *testing.T) {
	versionID := "ver-1"
	invocationID := "inv-1"
	activeVersion := versionID

	store := buildDefaultStore(invocationID, versionID, &activeVersion)
	store.getVersionByNumber = func(ctx context.Context, snippetID string, num int) (*models.SnippetVersion, error) {
		return nil, errors.New("version not found")
	}

	sched := scheduler.New(store, &mockExecutor{}, testEncKey, nil)
	_, err := sched.Invoke(context.Background(), scheduler.InvokeRequest{
		TenantID:      "tenant-1",
		SnippetSlug:   "hello",
		Env:           "prod",
		PinnedVersion: 99,
	})

	if err == nil {
		t.Fatal("expected error for non-existent pinned version, got nil")
	}
}

// --- Async tests ---

func TestScheduler_InvokeAsync_Success(t *testing.T) {
	versionID := "ver-1"
	invocationID := "inv-async-1"
	activeVersion := versionID

	store := buildDefaultStore(invocationID, versionID, &activeVersion)
	queue := &mockQueue{}
	exec := &mockExecutor{}

	sched := scheduler.NewWithQueue(store, exec, queue, testEncKey, nil)
	inv, err := sched.InvokeAsync(context.Background(), scheduler.InvokeRequest{
		TenantID:    "tenant-1",
		SnippetSlug: "hello",
		Env:         "prod",
		Input:       `{}`,
	}, "https://example.com/webhook")

	if err != nil {
		t.Fatalf("InvokeAsync error: %v", err)
	}
	if inv == nil {
		t.Fatal("InvokeAsync returned nil invocation")
	}
	if inv.Status != models.InvocationPending {
		t.Errorf("status = %q; want %q", inv.Status, models.InvocationPending)
	}
	if inv.InvokeMode != "async" {
		t.Errorf("invoke_mode = %q; want %q", inv.InvokeMode, "async")
	}

	// Verify job was enqueued.
	if len(queue.jobs) != 1 {
		t.Fatalf("expected 1 enqueued job, got %d", len(queue.jobs))
	}
	job := queue.jobs[0]
	if job.InvocationID != invocationID {
		t.Errorf("job.InvocationID = %q; want %q", job.InvocationID, invocationID)
	}
	if job.CallbackURL != "https://example.com/webhook" {
		t.Errorf("job.CallbackURL = %q; want https://example.com/webhook", job.CallbackURL)
	}
}

func TestScheduler_InvokeAsync_NoQueue(t *testing.T) {
	versionID := "ver-1"
	invocationID := "inv-1"
	activeVersion := versionID

	store := buildDefaultStore(invocationID, versionID, &activeVersion)

	// No queue — New (not NewWithQueue).
	sched := scheduler.New(store, &mockExecutor{}, testEncKey, nil)
	_, err := sched.InvokeAsync(context.Background(), scheduler.InvokeRequest{
		TenantID:    "tenant-1",
		SnippetSlug: "hello",
		Env:         "prod",
	}, "")

	if err == nil {
		t.Fatal("expected error when queue is not configured, got nil")
	}
}

func TestScheduler_InvokeQueued_PreservesRequestedMode(t *testing.T) {
	for _, mode := range []string{"sync", "stream"} {
		t.Run(mode, func(t *testing.T) {
			versionID := "ver-1"
			activeVersion := versionID
			store := buildDefaultStore("inv-queued-1", versionID, &activeVersion)
			queue := &mockQueue{}
			sched := scheduler.NewWithQueue(store, &mockExecutor{}, queue, testEncKey, nil)

			inv, err := sched.InvokeQueued(context.Background(), scheduler.InvokeRequest{
				TenantID:    "tenant-1",
				SnippetSlug: "hello",
				Env:         "prod",
				Input:       `{}`,
				InvokeMode:  mode,
			})
			if err != nil {
				t.Fatalf("InvokeQueued error: %v", err)
			}
			if inv.InvokeMode != mode {
				t.Errorf("invoke_mode = %q; want %q", inv.InvokeMode, mode)
			}
			if len(queue.jobs) != 1 {
				t.Fatalf("queued jobs = %d; want 1", len(queue.jobs))
			}
		})
	}
}

// --- Stream tests ---

func TestScheduler_InvokeStream_Success(t *testing.T) {
	versionID := "ver-1"
	invocationID := "inv-stream-1"
	activeVersion := versionID

	store := buildDefaultStore(invocationID, versionID, &activeVersion)

	streamCh := make(chan executor.StreamChunk, 3)
	streamCh <- executor.StreamChunk{Data: `{"chunk":"a"}`, Done: false}
	streamCh <- executor.StreamChunk{Data: `{"chunk":"b"}`, Done: false}
	streamCh <- executor.StreamChunk{Data: ``, Done: true}
	close(streamCh)

	exec := &mockExecutor{
		runStream: func(ctx context.Context, spec executor.RunSpec) (<-chan executor.StreamChunk, error) {
			return streamCh, nil
		},
	}

	sched := scheduler.New(store, exec, testEncKey, nil)
	ch, inv, err := sched.InvokeStream(context.Background(), scheduler.InvokeRequest{
		TenantID:    "tenant-1",
		SnippetSlug: "hello",
		Env:         "prod",
		Input:       `{}`,
	})

	if err != nil {
		t.Fatalf("InvokeStream error: %v", err)
	}
	if inv == nil {
		t.Fatal("InvokeStream returned nil invocation")
	}
	if inv.Status != models.InvocationRunning {
		t.Errorf("status = %q; want %q", inv.Status, models.InvocationRunning)
	}
	if inv.InvokeMode != "stream" {
		t.Errorf("invoke_mode = %q; want %q", inv.InvokeMode, "stream")
	}

	var chunks []executor.StreamChunk
	for c := range ch {
		chunks = append(chunks, c)
	}
	if len(chunks) != 3 {
		t.Errorf("got %d chunks; want 3", len(chunks))
	}
	if !chunks[len(chunks)-1].Done {
		t.Error("last chunk should have Done=true")
	}
}

// --- Phase 3: Canary routing tests ---

func TestScheduler_Invoke_CanaryRouting(t *testing.T) {
	activeVersionID := "ver-active"
	canaryVersionID := "ver-canary"
	invocationID := "inv-canary-1"

	var capturedVersionID string

	store := buildDefaultStore(invocationID, activeVersionID, &activeVersionID)

	// Override getSnippetEnvironment to return canary with 100% so it always triggers.
	store.getSnippetEnvironment = func(ctx context.Context, snippetID, env string) (*models.SnippetEnvironment, error) {
		canary := canaryVersionID
		return &models.SnippetEnvironment{
			SnippetID:       snippetID,
			Env:             env,
			ActiveVersionID: &activeVersionID,
			CanaryVersionID: &canary,
			CanaryPct:       100, // always route to canary
		}, nil
	}

	// GetVersion should return different versions depending on ID.
	store.getVersion = func(ctx context.Context, id string) (*models.SnippetVersion, error) {
		return fixVersion(id), nil
	}

	store.createInvocation = func(ctx context.Context, snippetID, versionID, env, tenantID, input string) (*models.Invocation, error) {
		capturedVersionID = versionID
		return fixInvocation(invocationID, versionID), nil
	}

	exec := &mockExecutor{
		run: func(ctx context.Context, spec executor.RunSpec) executor.RunResult {
			return executor.RunResult{Output: `{"ok":true}`, ExitCode: 0}
		},
	}

	sched := scheduler.New(store, exec, testEncKey, nil)
	_, err := sched.Invoke(context.Background(), scheduler.InvokeRequest{
		TenantID:    "tenant-1",
		SnippetSlug: "hello",
		Env:         "prod",
		Input:       `{}`,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedVersionID != canaryVersionID {
		t.Errorf("versionID = %q; want %q (canary should be used at 100%%)", capturedVersionID, canaryVersionID)
	}
}

// --- Phase 3: Secret injection test ---

func TestScheduler_Invoke_SecretsInjected(t *testing.T) {
	versionID := "ver-1"
	invocationID := "inv-sec-1"
	activeVersion := versionID

	store := buildDefaultStore(invocationID, versionID, &activeVersion)

	// Return a secret from the store.
	store.getSecretsForInvocation = func(ctx context.Context, tenantID, snippetID, env string, encKey []byte) (map[string]string, error) {
		return map[string]string{
			"MY_SECRET": "top-secret-value",
		}, nil
	}

	var capturedSpec executor.RunSpec
	exec := &mockExecutor{
		run: func(ctx context.Context, spec executor.RunSpec) executor.RunResult {
			capturedSpec = spec
			return executor.RunResult{Output: `{"ok":true}`, ExitCode: 0}
		},
	}

	sched := scheduler.New(store, exec, testEncKey, nil)
	_, err := sched.Invoke(context.Background(), scheduler.InvokeRequest{
		TenantID:    "tenant-1",
		SnippetSlug: "hello",
		Env:         "prod",
		Input:       `{}`,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedSpec.SecretEnvVars == nil {
		t.Fatal("SecretEnvVars should be set on RunSpec")
	}
	if capturedSpec.SecretEnvVars["MY_SECRET"] != "top-secret-value" {
		t.Errorf("MY_SECRET = %q; want %q", capturedSpec.SecretEnvVars["MY_SECRET"], "top-secret-value")
	}
}
