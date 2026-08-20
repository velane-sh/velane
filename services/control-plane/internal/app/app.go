package app

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"fmt"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/api"
	"github.com/abskrj/velane/services/control-plane/internal/api/handlers"
	"github.com/abskrj/velane/services/control-plane/internal/auth"
	"github.com/abskrj/velane/services/control-plane/internal/config"
	"github.com/abskrj/velane/services/control-plane/internal/executor"
	"github.com/abskrj/velane/services/control-plane/internal/executor/firecracker"
	"github.com/abskrj/velane/services/control-plane/internal/executor/remote"
	"github.com/abskrj/velane/services/control-plane/internal/license"
	"github.com/abskrj/velane/services/control-plane/internal/nango"
	"github.com/abskrj/velane/services/control-plane/internal/objectstore"
	"github.com/abskrj/velane/services/control-plane/internal/observability"
	"github.com/abskrj/velane/services/control-plane/internal/platformlibs"
	"github.com/abskrj/velane/services/control-plane/internal/sandboxcontrol"
	"github.com/abskrj/velane/services/control-plane/internal/sandboxcontrol/hostidentity"
	hostaws "github.com/abskrj/velane/services/control-plane/internal/sandboxcontrol/hostidentity/aws"
	"github.com/abskrj/velane/services/control-plane/internal/scheduler"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
	redisstore "github.com/abskrj/velane/services/control-plane/internal/store/redis"
	"github.com/abskrj/velane/services/control-plane/internal/telemetry"
	"github.com/abskrj/velane/services/control-plane/internal/worker"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// App holds all initialised dependencies. The caller owns the HTTP server lifecycle;
// App just wires the pieces and exposes the router for extension.
type App struct {
	Router                *chi.Mux
	Store                 *postgres.Store
	Log                   *zap.Logger
	Port                  string
	HostRouter            *chi.Mux
	SandboxHostListenAddr string
	SandboxHostTLSConfig  *tls.Config

	redis *redisstore.Client
}

// Close shuts down long-lived resources (DB pool, Redis). Call after the HTTP
// server has stopped accepting requests.
func (a *App) Close() {
	a.Store.Close()
	if a.redis != nil {
		_ = a.redis.Close()
	}
}

// Bootstrap initialises every dependency and returns a ready-to-serve App.
// Background goroutines run in ctx; cancel it before calling Close.
func Bootstrap(ctx context.Context, log *zap.Logger) (*App, error) {
	cfg := config.Load()
	log.Info("starting velane control-plane",
		zap.String("port", cfg.Port),
		zap.String("bun_executor", cfg.BunExecutorURL),
		zap.String("python_executor", cfg.PythonExecutorURL),
		zap.String("redis_url", cfg.RedisURL),
		zap.Int("worker_count", cfg.WorkerCount),
	)

	encKey := cfg.EncryptionKeyBytes(log)

	store, err := connectWithRetry(ctx, cfg.DatabaseURL, log, 10, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	log.Info("postgres connected and migrations applied")

	if cfg.ObjectStorageDriver == "" {
		log.Warn("object storage is not configured; legacy PostgreSQL payload storage remains enabled")
	} else {
		objects, storageErr := objectstore.New(ctx, objectstore.Config{
			Driver: cfg.ObjectStorageDriver, Bucket: cfg.ObjectStorageBucket, Prefix: cfg.ObjectStoragePrefix,
			S3Region: cfg.ObjectStorageS3Region, S3Endpoint: cfg.ObjectStorageS3Endpoint,
			S3ForcePathStyle:      cfg.ObjectStorageS3ForcePathStyle,
			S3KMSKeyID:            cfg.ObjectStorageS3KMSKeyID,
			AzureAccountURL:       cfg.ObjectStorageAzureAccountURL,
			AzureConnectionString: cfg.ObjectStorageAzureConnectionString,
		})
		if storageErr != nil {
			return nil, fmt.Errorf("configure object storage: %w", storageErr)
		}
		store.SetObjectStore(objects)
		store.ConfigureObjectMaintenance(cfg.ObjectGCGracePeriod, cfg.InvocationRetention)
		log.Info("object storage configured", zap.String("driver", cfg.ObjectStorageDriver))
		go runObjectStorageMaintenance(ctx, store, log)
	}

	platLibs, err := platformlibs.Load()
	if err != nil {
		return nil, fmt.Errorf("load platform libraries: %w", err)
	}
	log.Info("platform libraries loaded", zap.Int("count", len(platLibs)))

	privKey, pubKey := cfg.JWTKeyPair(log)
	authProvider := auth.NewJWTProvider(store, privKey, "https://api.velane.io")

	bootstrapAdminIfNeeded(ctx, store, authProvider, cfg, log)

	nangoClient := nango.New(cfg.NangoInternalURL, cfg.NangoSecretKey)
	if cfg.NangoSecretKey == "" {
		log.Warn("NANGO_SECRET_KEY not set — integration OAuth connections will not function")
	}
	log.Info("nango client configured", zap.String("url", cfg.NangoInternalURL))

	var exec executor.Executor
	switch cfg.ExecutorType {
	case "firecracker":
		fcExec, err := firecracker.New(firecracker.Config{
			FirecrackerBin: cfg.FirecrackerBinary,
			JailerBin:      cfg.FirecrackerJailerBinary,
			BunRootfs:      cfg.FirecrackerBunRootfs,
			PythonRootfs:   cfg.FirecrackerPythonRootfs,
			KernelImage:    cfg.FirecrackerKernelImage,
		}, log)
		if err != nil {
			return nil, fmt.Errorf("firecracker executor: %w", err)
		}
		exec = fcExec
	default:
		exec = remote.New(cfg.BunExecutorURL, cfg.PythonExecutorURL)
	}

	var redisClient *redisstore.Client
	var sched *scheduler.Scheduler
	observer := observability.NewPipelineObserver(nil, nil, nil)

	rc, err := redisstore.New(cfg.RedisURL)
	if err != nil {
		log.Warn("redis not available, running in sync-only mode",
			zap.String("redis_url", cfg.RedisURL),
			zap.Error(err),
		)
		sched = scheduler.New(store, exec, encKey, platLibs)
	} else {
		redisClient = rc
		log.Info("redis connected", zap.String("redis_url", cfg.RedisURL))
		sched = scheduler.NewWithQueue(store, exec, redisClient, encKey, platLibs)
		sched.SetEventStream(redisClient)
	}
	sched.WithInternalProxyURL(cfg.InternalProxyURL)
	sched.SetObserver(observer)

	go runKVReaper(ctx, store, log)

	if redisClient != nil {
		w := worker.New(redisClient, store, exec, log, cfg.WorkerCount)
		w.SetObserver(observer)
		w.SetEventPublisher(redisClient)
		go w.Run(ctx)
		log.Info("background worker started", zap.Int("workers", cfg.WorkerCount))
		go runStaleInvocationReaper(ctx, store, log)
		go worker.NewIntegrationEventWorker(redisClient, store, nangoClient, sched, log).Run(ctx)
		log.Info("integration event worker started")
	}

	go telemetry.RunHeartbeat(ctx, store, cfg.PublicBaseURL, cfg.LicenseKey, log)

	licMgr := license.NewManager(cfg.LicenseKey, log)

	oauthCfg := handlers.OAuthConfig{
		PublicBaseURL:           cfg.PublicBaseURL,
		GoogleOAuthClientID:     cfg.GoogleOAuthClientID,
		GoogleOAuthClientSecret: cfg.GoogleOAuthClientSecret,
		GitHubOAuthClientID:     cfg.GitHubOAuthClientID,
		GitHubOAuthClientSecret: cfg.GitHubOAuthClientSecret,
	}
	var sandboxService *sandboxcontrol.Service
	var hostRouter *chi.Mux
	var hostTLSConfig *tls.Config
	var hostEnrollmentReady bool
	if cfg.SandboxHostProtocolConfigured() {
		hostTLSConfig, err = cfg.SandboxHostTLSConfig()
		if err != nil {
			return nil, fmt.Errorf("configure sandbox host mTLS: %w", err)
		}
		signingCAFile, signingKeyFile, certificateTTL, err := cfg.SandboxHostSignerConfig()
		if err != nil {
			return nil, fmt.Errorf("configure sandbox host certificate issuer: %w", err)
		}
		issuer, err := hostidentity.NewCertificateIssuer(signingCAFile, signingKeyFile, certificateTTL)
		if err != nil {
			return nil, fmt.Errorf("load sandbox host certificate issuer: %w", err)
		}
		poolID, accountID, region, asgName, amiID, launchTemplateID, err := cfg.SandboxHostAWSVerifierConfig()
		if err != nil {
			return nil, fmt.Errorf("configure sandbox host AWS verifier: %w", err)
		}
		reader, err := hostaws.NewQueryAPIReader(ctx, region, accountID)
		if err != nil {
			return nil, fmt.Errorf("configure sandbox host AWS reader: %w", err)
		}
		awsRoots, err := cfg.SandboxHostAWSIIDTrustRoots()
		if err != nil {
			return nil, fmt.Errorf("configure sandbox host AWS IID roots: %w", err)
		}
		lineageID, compatibilityKey, err := cfg.SandboxHostExpectedIdentity()
		if err != nil {
			return nil, fmt.Errorf("configure sandbox host expected identity: %w", err)
		}
		watchdogSigningKey, err := cfg.SandboxWatchdogSigningKey()
		if err != nil {
			return nil, fmt.Errorf("configure sandbox watchdog lease signer: %w", err)
		}
		store.ConfigureWatchdogLeaseSigner(ed25519.PrivateKey(watchdogSigningKey))
		hostEnrollmentReady = true
		verifier := hostaws.Verifier{Reader: reader, STSValidator: hostaws.HTTPSTSProofValidator{AccountID: accountID, Region: region}, TrustedRoots: awsRoots, Expected: hostaws.ExpectedPool{PoolID: poolID, AccountID: accountID, Region: region, AutoScalingGroup: asgName, AMIID: amiID, LaunchTemplateID: launchTemplateID, LineageID: lineageID, HostCompatibilityKey: compatibilityKey}}
		hostRouter = api.NewHostRouter(store, api.HostEnrollmentManager{Store: store, Verifier: verifier, Issuer: issuer})
		log.Info("sandbox host enrollment and fencing enabled", zap.String("host_listen_addr", cfg.SandboxHostListenAddr))
	} else if cfg.SandboxControlEnabled {
		log.Warn("sandbox control is enabled but host protocol prerequisites are incomplete; host enrollment and public sandbox mutations remain disabled")
	}
	capabilityProvider := sandboxcontrol.NewOperationalProvider(sandboxcontrol.OperationalDependencies{
		ControlEnabled: cfg.SandboxControlEnabled, HostEnrollment: hostEnrollmentReady,
		LifecyclePayloads: true, CommandDispatch: hostEnrollmentReady,
		WatchdogSigner: hostEnrollmentReady, SnapshotStore: store.HasSnapshotObjectStore(),
		// Snapshot key wrapping and image-builder dispatch are not wired yet.
		// Leaving them nil prevents config labels from advertising dead paths.
		HostCommands: store.SandboxHostCommandReadiness,
	})
	if cfg.SandboxControlEnabled {
		sandboxService = sandboxcontrol.NewService(store, capabilityProvider)
		go sandboxcontrol.NewManager(sandboxService, log).Run(ctx)
	}
	router := api.NewRouterWithJWTAndEventsAndSandboxes(store, sched, log, encKey, authProvider, pubKey, nangoClient, redisClient,
		cfg.NangoInternalURL, cfg.NangoConnectURL, cfg.NangoApiURL,
		cfg.NangoWebhookSecret, cfg.NangoSecretKey, cfg.MCPPublicURL,
		platLibs, oauthCfg, licMgr, sandboxService, capabilityProvider)

	return &App{
		Router:                router,
		Store:                 store,
		Log:                   log,
		Port:                  cfg.Port,
		HostRouter:            hostRouter,
		SandboxHostListenAddr: cfg.SandboxHostListenAddr,
		SandboxHostTLSConfig:  hostTLSConfig,
		redis:                 redisClient,
	}, nil
}

func connectWithRetry(ctx context.Context, dsn string, log *zap.Logger, maxAttempts int, delay time.Duration) (*postgres.Store, error) {
	var lastErr error
	for i := 1; i <= maxAttempts; i++ {
		store, err := postgres.New(ctx, dsn)
		if err == nil {
			return store, nil
		}
		lastErr = err
		log.Warn("postgres not ready, retrying",
			zap.Int("attempt", i),
			zap.Int("max", maxAttempts),
			zap.Error(err),
		)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, fmt.Errorf("gave up after %d attempts: %w", maxAttempts, lastErr)
}

func runStaleInvocationReaper(ctx context.Context, store *postgres.Store, log *zap.Logger) {
	const (
		interval   = 1 * time.Minute
		staleAfter = 10 * time.Minute
	)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := store.FailStaleInvocations(ctx, staleAfter)
			if err != nil {
				log.Warn("reaper: fail stale invocations", zap.Error(err))
				continue
			}
			if n > 0 {
				log.Info("reaper: marked stale invocations as timed out", zap.Int64("count", n))
			}
		}
	}
}

func runKVReaper(ctx context.Context, store *postgres.Store, log *zap.Logger) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := store.DeleteExpiredKV(ctx, postgres.KVReaperBatchSize, 20)
			if err != nil {
				log.Warn("reaper: delete expired KV entries", zap.Error(err))
				continue
			}
			if n > 0 {
				log.Info("reaper: deleted expired KV entries", zap.Int64("count", n))
			}
		}
	}
}

func runObjectStorageMaintenance(ctx context.Context, store *postgres.Store, log *zap.Logger) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := store.MaintainObjectStorage(ctx); err != nil && ctx.Err() == nil {
			log.Warn("object storage maintenance failed", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func bootstrapAdminIfNeeded(ctx context.Context, store *postgres.Store, provider auth.Provider, cfg config.Config, log *zap.Logger) {
	if cfg.BootstrapEmail == "" || cfg.BootstrapPassword == "" {
		return
	}
	_, err := store.GetUserByEmail(ctx, cfg.BootstrapEmail)
	if err == nil {
		log.Info("bootstrap: admin user already exists, skipping", zap.String("email", cfg.BootstrapEmail))
		return
	}
	tenant, err := store.GetTenantBySlug(ctx, cfg.BootstrapTenant)
	if err != nil {
		tenant, err = store.CreateTenant(ctx, cfg.BootstrapTenant, cfg.BootstrapTenant)
		if err != nil {
			log.Error("bootstrap: failed to create tenant", zap.String("slug", cfg.BootstrapTenant), zap.Error(err))
			return
		}
		log.Info("bootstrap: tenant created", zap.String("slug", tenant.Slug), zap.String("id", tenant.ID))
	}
	user, err := provider.CreateUser(ctx, cfg.BootstrapEmail, cfg.BootstrapPassword)
	if err != nil {
		log.Error("bootstrap: failed to create admin user", zap.String("email", cfg.BootstrapEmail), zap.Error(err))
		return
	}
	if _, err := store.AddMember(ctx, tenant.ID, user.ID, "admin"); err != nil {
		log.Error("bootstrap: failed to add admin member", zap.Error(err))
		return
	}
	log.Info("bootstrap: admin user created — remove BOOTSTRAP_EMAIL/PASSWORD from environment after first boot",
		zap.String("email", user.Email),
		zap.String("tenant", tenant.Slug),
		zap.String("role", "admin"),
	)
}
