package scheduler

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/callerid"
	"github.com/abskrj/velane/services/control-plane/internal/executor"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/abskrj/velane/services/control-plane/internal/observability"
	"github.com/abskrj/velane/services/control-plane/internal/platformlibs"
	redisstore "github.com/abskrj/velane/services/control-plane/internal/store/redis"
)

// EventStream reads live invocation events from the per-invocation event stream.
// Implemented by *redisstore.Client.
type EventStream interface {
	ReadEvents(ctx context.Context, invocationID, lastID string, blockFor time.Duration) ([]redisstore.StreamEvent, string, error)
}

// Store is the subset of *postgres.Store that the Scheduler depends on.
// Keeping this narrow makes the scheduler straightforward to test with a mock.
type Store interface {
	GetSnippetByID(ctx context.Context, id string) (*models.Snippet, error)
	GetSnippetBySlug(ctx context.Context, tenantID, slug string) (*models.Snippet, error)
	GetSnippetEnvironment(ctx context.Context, snippetID, env string) (*models.SnippetEnvironment, error)
	GetVersion(ctx context.Context, id string) (*models.SnippetVersion, error)
	GetVersionByNumber(ctx context.Context, snippetID string, num int) (*models.SnippetVersion, error)
	CreateInvocation(ctx context.Context, snippetID, versionID, env, tenantID, input string) (*models.Invocation, error)
	CreateInvocationWithMode(ctx context.Context, snippetID, versionID, environment, tenantID, inputPayload, invokeMode, callbackURL string, status models.InvocationStatus) (*models.Invocation, error)
	UpdateInvocationResult(ctx context.Context, id string, status models.InvocationStatus, output, errMsg, stderr string, durationMs, peakMemoryMB, cpuMs int) error
	GetInvocation(ctx context.Context, id string) (*models.Invocation, error)
	GetSecretsForInvocation(ctx context.Context, tenantID, snippetID, env string, encKey []byte) (map[string]string, error)
	GetTenantLibrariesForInvocation(ctx context.Context, tenantID, tenantSlug, language string) (map[string]string, error)
	GetTenantByID(ctx context.Context, id string) (*models.Tenant, error)
}

// Queue is the subset of *redisstore.Client used by the Scheduler for async jobs.
type Queue interface {
	Enqueue(ctx context.Context, job redisstore.Job) error
}

// InvokeRequest carries the parameters for a snippet invocation.
type InvokeRequest struct {
	TenantID      string
	SnippetSlug   string
	Env           string                // "dev" | "staging" | "prod"
	Input         string                // raw JSON
	PinnedVersion int                   // 0 = use active version from environment
	Caller        models.CallerIdentity // principal whose group access the sandbox inherits
	InvokeMode    string                // "sync" | "stream"; used when sync runs through the queue
}

// Scheduler resolves, executes, and records snippet invocations.
type Scheduler struct {
	store            Store
	executor         executor.Executor
	queue            Queue  // nil in sync-only mode
	encKey           []byte // for secret decryption
	observer         observability.InvocationObserver
	platLibs         []platformlibs.PlatformLib
	internalProxyURL string // injected as VELANE_PROXY_URL into every invocation
	eventStream      EventStream
}

// SetEventStream wires the live invocation event reader (Redis). When set,
// queued+streaming invocations become available via InvokeQueued + ReadEvents.
func (s *Scheduler) SetEventStream(es EventStream) *Scheduler {
	s.eventStream = es
	return s
}

// HasEventStream reports whether a live event stream reader is configured.
func (s *Scheduler) HasEventStream() bool { return s.eventStream != nil && s.queue != nil }

// ReadEvents reads live events for an invocation. See EventStream.ReadEvents.
func (s *Scheduler) ReadEvents(ctx context.Context, invocationID, lastID string, blockFor time.Duration) ([]redisstore.StreamEvent, string, error) {
	if s.eventStream == nil {
		return nil, lastID, fmt.Errorf("event stream not configured")
	}
	return s.eventStream.ReadEvents(ctx, invocationID, lastID, blockFor)
}

// New creates a Scheduler wired to the given store and executor (sync only, no queue).
func New(store Store, exec executor.Executor, encKey []byte, platLibs []platformlibs.PlatformLib) *Scheduler {
	return &Scheduler{
		store:    store,
		executor: exec,
		encKey:   encKey,
		observer: observability.NoopObserver{},
		platLibs: platLibs,
	}
}

// NewWithQueue creates a Scheduler with an async job queue.
func NewWithQueue(store Store, exec executor.Executor, q Queue, encKey []byte, platLibs []platformlibs.PlatformLib) *Scheduler {
	return &Scheduler{
		store:    store,
		executor: exec,
		queue:    q,
		encKey:   encKey,
		observer: observability.NoopObserver{},
		platLibs: platLibs,
	}
}

// WithInternalProxyURL sets the URL executors use to reach the control plane proxy.
// This is injected as VELANE_PROXY_URL into every invocation's environment.
func (s *Scheduler) WithInternalProxyURL(url string) *Scheduler {
	s.internalProxyURL = url
	return s
}

// getLibraries merges platform libs (from the embedded binary) with the
// tenant's latest published libs. Tenant libs override platform libs on
// the same import path.
func (s *Scheduler) getLibraries(ctx context.Context, tenantID, tenantSlug, language string) (map[string]string, error) {
	result := make(map[string]string)

	for _, lib := range s.platLibs {
		if lib.Language == language {
			result[platformlibs.ImportPath(language, lib.Slug)] = lib.Code
		}
	}

	tenantLibs, err := s.store.GetTenantLibrariesForInvocation(ctx, tenantID, tenantSlug, language)
	if err != nil {
		return nil, err
	}
	for k, v := range tenantLibs {
		result[k] = v
	}
	return result, nil
}

// injectProxyEnv adds VELANE_PROXY_URL and VELANE_TENANT_ID to the env map so
// that @velane/integrations inside snippet code can reach the internal proxy.
//
// It also mints the caller identity token the proxy uses for group-based
// integration checks. The group list is exposed unsigned for introspection
// only; the proxy trusts the signed token exclusively, so snippet code cannot
// grant itself access by editing either value.
func (s *Scheduler) injectProxyEnv(env map[string]string, tenantID string, caller models.CallerIdentity) {
	if s.internalProxyURL != "" {
		env["VELANE_PROXY_URL"] = s.internalProxyURL
	}
	env["VELANE_TENANT_ID"] = tenantID

	caller.TenantID = tenantID
	if len(caller.GroupIDs) > 0 {
		env["VELANE_CALLER_GROUPS"] = strings.Join(caller.GroupIDs, ",")
	}
	if caller.UserID == "" && caller.Role == "" && len(caller.GroupIDs) == 0 {
		// Nothing to attest — legacy tenant-wide invocation.
		return
	}
	token, err := callerid.Sign(s.encKey, callerid.Claims{
		TenantID: caller.TenantID,
		UserID:   caller.UserID,
		Role:     caller.Role,
		GroupIDs: caller.GroupIDs,
	}, callerid.DefaultTTL)
	if err != nil {
		return
	}
	env[callerid.EnvVar] = token
}

// SetObserver injects a post-invocation observer for observability pipelines.
func (s *Scheduler) SetObserver(observer observability.InvocationObserver) {
	if observer == nil {
		s.observer = observability.NoopObserver{}
		return
	}
	s.observer = observer
}

func (s *Scheduler) resolveSnippet(ctx context.Context, tenantID, slugOrID string) (*models.Snippet, error) {
	if sn, err := s.store.GetSnippetByID(ctx, slugOrID); err == nil {
		if sn.TenantID != tenantID {
			return nil, ErrAccessDenied
		}
		return sn, nil
	}
	return s.store.GetSnippetBySlug(ctx, tenantID, slugOrID)
}

// resolveVersion resolves the snippet and version for a request.
// If req.PinnedVersion > 0, fetches that specific version number.
// Otherwise uses the active version from the snippet environment, applying
// canary routing when configured.
func (s *Scheduler) resolveVersion(ctx context.Context, req InvokeRequest) (*models.Snippet, *models.SnippetVersion, error) {
	snippet, err := s.resolveSnippet(ctx, req.TenantID, req.SnippetSlug)
	if err != nil {
		if errors.Is(err, ErrAccessDenied) {
			return nil, nil, err
		}
		return nil, nil, fmt.Errorf("snippet not found: %w", err)
	}

	if req.PinnedVersion > 0 {
		version, err := s.store.GetVersionByNumber(ctx, snippet.ID, req.PinnedVersion)
		if err != nil {
			return nil, nil, fmt.Errorf("get pinned version %d: %w", req.PinnedVersion, err)
		}
		return snippet, version, nil
	}

	// Use the active version from the environment.
	env, err := s.store.GetSnippetEnvironment(ctx, snippet.ID, req.Env)
	if err != nil {
		return nil, nil, fmt.Errorf("get environment: %w", err)
	}
	if env.ActiveVersionID == nil {
		return nil, nil, fmt.Errorf("no published version in environment %q for snippet %q", req.Env, req.SnippetSlug)
	}

	version, err := s.store.GetVersion(ctx, *env.ActiveVersionID)
	if err != nil {
		return nil, nil, fmt.Errorf("get version: %w", err)
	}

	// Apply canary routing: if a canary version is configured and the random
	// roll falls within the canary percentage, route to the canary version.
	if env.CanaryVersionID != nil && env.CanaryPct > 0 {
		if rand.Intn(100) < env.CanaryPct { //nolint:gosec
			canaryVersion, err := s.store.GetVersion(ctx, *env.CanaryVersionID)
			if err != nil {
				return nil, nil, fmt.Errorf("get canary version: %w", err)
			}
			version = canaryVersion
		}
	}

	return snippet, version, nil
}

// normaliseInput returns "{}" for empty input.
func normaliseInput(input string) string {
	if input == "" {
		return "{}"
	}
	return input
}

// mapResultStatus maps an executor RunResult to an InvocationStatus.
func mapResultStatus(result executor.RunResult) models.InvocationStatus {
	if result.Error == "timeout" {
		return models.InvocationTimeout
	}
	if result.Error == "oom" {
		return models.InvocationOOMKilled
	}
	if result.ExitCode != 0 || result.Error != "" {
		return models.InvocationFailed
	}
	return models.InvocationCompleted
}

// buildEgressPolicy converts a models.EgressPolicy to an executor.EgressPolicy.
func buildEgressPolicy(p models.EgressPolicy) *executor.EgressPolicy {
	return &executor.EgressPolicy{
		BlockedCIDRs:   p.BlockedCIDRs,
		BlockedDomains: p.BlockedDomains,
	}
}

// Invoke executes a snippet synchronously:
//  1. Resolve the snippet and version (active, canary, or pinned).
//  2. Fetch secrets and tenant egress policy.
//  3. Create an invocation record (status=running).
//  4. Call the executor.
//  5. Persist the result and return the completed invocation.
func (s *Scheduler) Invoke(ctx context.Context, req InvokeRequest) (*models.Invocation, error) {
	snippet, version, err := s.resolveVersion(ctx, req)
	if err != nil {
		return nil, err
	}

	input := normaliseInput(req.Input)

	secrets, err := s.store.GetSecretsForInvocation(ctx, req.TenantID, snippet.ID, req.Env, s.encKey)
	if err != nil {
		return nil, fmt.Errorf("fetch secrets: %w", err)
	}

	tenant, err := s.store.GetTenantByID(ctx, req.TenantID)
	if err != nil {
		return nil, fmt.Errorf("fetch tenant: %w", err)
	}

	libs, err := s.getLibraries(ctx, req.TenantID, tenant.Slug, string(snippet.Language))
	if err != nil {
		return nil, fmt.Errorf("fetch libraries: %w", err)
	}

	invocation, err := s.store.CreateInvocation(ctx, snippet.ID, version.ID, req.Env, req.TenantID, input)
	if err != nil {
		return nil, fmt.Errorf("create invocation: %w", err)
	}

	s.injectProxyEnv(secrets, req.TenantID, req.Caller)
	result := s.executor.Run(ctx, executor.RunSpec{
		Language:      string(snippet.Language),
		Code:          version.Code,
		Input:         input,
		TimeoutMs:     version.TimeoutMs,
		MaxMemoryMB:   version.MaxMemoryMB,
		MaxCPUPercent: version.MaxCPUPercent,
		SecretEnvVars: secrets,
		Libraries:     libs,
		EgressPolicy:  buildEgressPolicy(tenant.EgressPolicy),
	})

	status := mapResultStatus(result)

	updateErr := s.store.UpdateInvocationResult(ctx,
		invocation.ID,
		status,
		result.Output,
		result.Error,
		result.Stderr,
		result.DurationMs,
		result.PeakMemoryMB,
		0,
	)
	if updateErr != nil {
		_ = updateErr
	}

	final, err := s.store.GetInvocation(ctx, invocation.ID)
	if err != nil {
		invocation.Status = status
		invocation.Output = result.Output
		invocation.Error = result.Error
		invocation.Stderr = result.Stderr
		invocation.DurationMs = result.DurationMs
		invocation.PeakMemoryMB = result.PeakMemoryMB
		invocation.CPUMs = 0
		_ = s.observer.OnInvocationCompleted(ctx, invocation)
		return invocation, nil
	}
	_ = s.observer.OnInvocationCompleted(ctx, final)

	return final, nil
}

// InvokeAsync enqueues the snippet for background execution and returns the
// pending invocation record immediately.
func (s *Scheduler) InvokeAsync(ctx context.Context, req InvokeRequest, callbackURL string) (*models.Invocation, error) {
	if s.queue == nil {
		return nil, fmt.Errorf("async invocation requires a configured queue")
	}

	snippet, version, err := s.resolveVersion(ctx, req)
	if err != nil {
		return nil, err
	}

	input := normaliseInput(req.Input)

	secrets, err := s.store.GetSecretsForInvocation(ctx, req.TenantID, snippet.ID, req.Env, s.encKey)
	if err != nil {
		return nil, fmt.Errorf("fetch secrets: %w", err)
	}
	s.injectProxyEnv(secrets, req.TenantID, req.Caller)

	tenant, err := s.store.GetTenantByID(ctx, req.TenantID)
	if err != nil {
		return nil, fmt.Errorf("fetch tenant: %w", err)
	}

	invocation, err := s.store.CreateInvocationWithMode(
		ctx,
		snippet.ID, version.ID, req.Env, req.TenantID, input,
		"async", callbackURL,
		models.InvocationPending,
	)
	if err != nil {
		return nil, fmt.Errorf("create invocation: %w", err)
	}

	var jobEgress *redisstore.EgressPolicyJob
	if ep := tenant.EgressPolicy; len(ep.BlockedCIDRs) > 0 || len(ep.BlockedDomains) > 0 {
		jobEgress = &redisstore.EgressPolicyJob{
			BlockedCIDRs:   ep.BlockedCIDRs,
			BlockedDomains: ep.BlockedDomains,
		}
	}

	libs, err := s.getLibraries(ctx, req.TenantID, tenant.Slug, string(snippet.Language))
	if err != nil {
		return nil, fmt.Errorf("fetch libraries: %w", err)
	}

	job := redisstore.Job{
		InvocationID:  invocation.ID,
		SnippetID:     snippet.ID,
		VersionID:     version.ID,
		TenantID:      req.TenantID,
		Language:      string(snippet.Language),
		Code:          version.Code,
		Input:         input,
		TimeoutMs:     version.TimeoutMs,
		MaxMemoryMB:   version.MaxMemoryMB,
		MaxCPUPercent: version.MaxCPUPercent,
		CallbackURL:   callbackURL,
		Env:           req.Env,
		SecretEnvVars: secrets,
		Libraries:     libs,
		EgressPolicy:  jobEgress,
	}

	if err := s.queue.Enqueue(ctx, job); err != nil {
		return nil, fmt.Errorf("enqueue job: %w", err)
	}

	return invocation, nil
}

// HasQueue reports whether the scheduler has a configured async/stream queue.
func (s *Scheduler) HasQueue() bool { return s.queue != nil }

// InvokeQueued enqueues the snippet as a streaming job and returns the created
// invocation (status=running) immediately. The worker executes it via the
// streaming path and publishes live events to the invocation's event stream;
// the HTTP handler reads that stream and relays it to the client. This is the
// primary path for sync/stream invocations when a queue is configured.
func (s *Scheduler) InvokeQueued(ctx context.Context, req InvokeRequest) (*models.Invocation, error) {
	if s.queue == nil {
		return nil, fmt.Errorf("queued invocation requires a configured queue")
	}

	invokeMode := req.InvokeMode
	if invokeMode == "" {
		invokeMode = "sync"
	}
	if invokeMode != "sync" && invokeMode != "stream" {
		return nil, fmt.Errorf("queued invocation mode must be sync or stream")
	}

	snippet, version, err := s.resolveVersion(ctx, req)
	if err != nil {
		return nil, err
	}

	input := normaliseInput(req.Input)

	secrets, err := s.store.GetSecretsForInvocation(ctx, req.TenantID, snippet.ID, req.Env, s.encKey)
	if err != nil {
		return nil, fmt.Errorf("fetch secrets: %w", err)
	}

	tenant, err := s.store.GetTenantByID(ctx, req.TenantID)
	if err != nil {
		return nil, fmt.Errorf("fetch tenant: %w", err)
	}

	invocation, err := s.store.CreateInvocationWithMode(
		ctx,
		snippet.ID, version.ID, req.Env, req.TenantID, input,
		invokeMode, "",
		models.InvocationRunning,
	)
	if err != nil {
		return nil, fmt.Errorf("create invocation: %w", err)
	}

	// Inject the internal proxy env so @velane/integrations works inside the
	// snippet, matching the inline sync path.
	s.injectProxyEnv(secrets, req.TenantID, req.Caller)

	var jobEgress *redisstore.EgressPolicyJob
	if ep := tenant.EgressPolicy; len(ep.BlockedCIDRs) > 0 || len(ep.BlockedDomains) > 0 {
		jobEgress = &redisstore.EgressPolicyJob{
			BlockedCIDRs:   ep.BlockedCIDRs,
			BlockedDomains: ep.BlockedDomains,
		}
	}

	libs, err := s.getLibraries(ctx, req.TenantID, tenant.Slug, string(snippet.Language))
	if err != nil {
		return nil, fmt.Errorf("fetch libraries: %w", err)
	}

	job := redisstore.Job{
		InvocationID:  invocation.ID,
		SnippetID:     snippet.ID,
		VersionID:     version.ID,
		TenantID:      req.TenantID,
		Language:      string(snippet.Language),
		Code:          version.Code,
		Input:         input,
		TimeoutMs:     version.TimeoutMs,
		MaxMemoryMB:   version.MaxMemoryMB,
		MaxCPUPercent: version.MaxCPUPercent,
		Env:           req.Env,
		SecretEnvVars: secrets,
		Libraries:     libs,
		EgressPolicy:  jobEgress,
		Stream:        true,
	}

	if err := s.queue.Enqueue(ctx, job); err != nil {
		return nil, fmt.Errorf("enqueue job: %w", err)
	}

	return invocation, nil
}

// InvokeStream executes the snippet and streams chunks to the returned channel.
// The caller is responsible for reading from the channel until it is closed.
func (s *Scheduler) InvokeStream(ctx context.Context, req InvokeRequest) (<-chan executor.StreamChunk, *models.Invocation, error) {
	snippet, version, err := s.resolveVersion(ctx, req)
	if err != nil {
		return nil, nil, err
	}

	input := normaliseInput(req.Input)

	secrets, err := s.store.GetSecretsForInvocation(ctx, req.TenantID, snippet.ID, req.Env, s.encKey)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch secrets: %w", err)
	}

	tenant, err := s.store.GetTenantByID(ctx, req.TenantID)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch tenant: %w", err)
	}

	invocation, err := s.store.CreateInvocationWithMode(
		ctx,
		snippet.ID, version.ID, req.Env, req.TenantID, input,
		"stream", "",
		models.InvocationRunning,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("create invocation: %w", err)
	}
	s.injectProxyEnv(secrets, req.TenantID, req.Caller)

	streamLibs, err := s.getLibraries(ctx, req.TenantID, tenant.Slug, string(snippet.Language))
	if err != nil {
		return nil, nil, fmt.Errorf("fetch libraries: %w", err)
	}

	ch, err := s.executor.RunStream(ctx, executor.RunSpec{
		Language:      string(snippet.Language),
		Code:          version.Code,
		Input:         input,
		TimeoutMs:     version.TimeoutMs,
		MaxMemoryMB:   version.MaxMemoryMB,
		MaxCPUPercent: version.MaxCPUPercent,
		SecretEnvVars: secrets,
		Libraries:     streamLibs,
		EgressPolicy:  buildEgressPolicy(tenant.EgressPolicy),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("run stream: %w", err)
	}

	return ch, invocation, nil
}
