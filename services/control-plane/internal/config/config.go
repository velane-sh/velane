package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"os"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
)

// Config holds all runtime configuration loaded from environment variables.
type Config struct {
	DatabaseURL                        string
	BunExecutorURL                     string
	PythonExecutorURL                  string
	Port                               string
	RedisURL                           string // default: "localhost:6379"
	WorkerCount                        int    // default: 5
	EncryptionKey                      string // hex-encoded 32-byte AES key; generated ephemerally if empty
	ClickHouseDSN                      string // optional Phase 5 metrics store DSN
	LogsBucket                         string // optional Phase 5 object storage bucket for logs
	ReplayBucket                       string // optional Phase 5 object storage bucket for replay payloads
	ObjectStorageDriver                string
	ObjectStorageBucket                string
	ObjectStoragePrefix                string
	ObjectStorageS3Region              string
	ObjectStorageS3Endpoint            string
	ObjectStorageS3ForcePathStyle      bool
	ObjectStorageAzureAccountURL       string
	ObjectStorageAzureConnectionString string
	ObjectGCGracePeriod                time.Duration
	InvocationRetention                time.Duration

	// JWT auth (Phase 9)
	JWTPrivateKeyPEM      string // RS256 private key PEM (env: JWT_PRIVATE_KEY); if empty, generate ephemeral key with warning
	JWTPublicKeyPEM       string // derived from private key, not loaded from env
	SSOSAMLPrivateKeyPEM  string // stable SAML SP signing key
	SSOSAMLCertificatePEM string // matching public certificate

	// Social login (Phase 9) — OAuth user sign-in for the admin portal.
	PublicBaseURL           string // PUBLIC_BASE_URL: browser-facing admin portal origin (default http://localhost:8092). API is reached at PublicBaseURL + "/api".
	GoogleOAuthClientID     string // GOOGLE_OAUTH_CLIENT_ID
	GoogleOAuthClientSecret string // GOOGLE_OAUTH_CLIENT_SECRET
	GitHubOAuthClientID     string // GITHUB_OAUTH_CLIENT_ID
	GitHubOAuthClientSecret string // GITHUB_OAUTH_CLIENT_SECRET

	// Bootstrap (first-run admin seeding)
	BootstrapEmail    string // BOOTSTRAP_EMAIL: email for the initial admin user
	BootstrapPassword string // BOOTSTRAP_PASSWORD: password for the initial admin user
	BootstrapTenant   string // BOOTSTRAP_TENANT: slug for the initial tenant (default: "default")

	// Nango (integration OAuth proxy)
	NangoInternalURL   string // http://nango:3003 — internal only, never exposed
	NangoPublicURL     string // browser-accessible Nango URL, used to rewrite logo image URLs
	NangoConnectURL    string // browser-accessible Connect UI URL (NANGO_PUBLIC_CONNECT_URL), passed to @nangohq/frontend as baseURL
	NangoApiURL        string // browser-accessible Nango API URL, passed to @nangohq/frontend as apiURL
	NangoSecretKey     string // NANGO_SECRET_KEY
	NangoPublicKey     string // NANGO_PUBLIC_KEY — returned to frontend for Connect UI
	NangoWebhookSecret string // NANGO_WEBHOOK_SECRET — HMAC-SHA256 signing secret for webhook verification
	MCPPublicURL       string // MCP_PUBLIC_URL — public URL used by IDE clients (e.g. https://mcp.example.com/mcp)
	InternalProxyURL   string // URL executors use to reach the control plane proxy, e.g. http://control-plane:8080

	// License
	LicenseKey string // VELANE_LICENSE_KEY: instance-level license UUID (optional)
	CloudMode  bool   // VELANE_CLOUD: when true the dashboard shows billing UI (cloud-hosted deployments only)

	// Executor selection (Phase 9)
	ExecutorType            string // "process" (default) | "firecracker"
	FirecrackerBinary       string // path to firecracker binary, default "/usr/local/bin/firecracker"
	FirecrackerJailerBinary string // path to jailer binary, default "/usr/local/bin/jailer"
	FirecrackerBunRootfs    string // path to Bun rootfs image
	FirecrackerPythonRootfs string // path to Python rootfs image
	FirecrackerKernelImage  string // path to Linux kernel image
}

// Load reads configuration from environment variables, falling back to sensible
// defaults for local development.
func Load() Config {
	workerCount := 5
	if v := os.Getenv("WORKER_COUNT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			workerCount = n
		}
	}

	return Config{
		DatabaseURL:                        getEnv("DATABASE_URL", "postgres://velane:velane@localhost:5432/velane"),
		BunExecutorURL:                     getEnv("BUN_EXECUTOR_URL", "http://localhost:8081"),
		PythonExecutorURL:                  getEnv("PYTHON_EXECUTOR_URL", "http://localhost:8082"),
		Port:                               getEnv("PORT", "8080"),
		RedisURL:                           getEnv("REDIS_URL", "localhost:6379"),
		WorkerCount:                        workerCount,
		EncryptionKey:                      os.Getenv("ENCRYPTION_KEY"),
		ClickHouseDSN:                      os.Getenv("CLICKHOUSE_DSN"),
		LogsBucket:                         os.Getenv("LOGS_BUCKET"),
		ReplayBucket:                       os.Getenv("REPLAY_BUCKET"),
		ObjectStorageDriver:                strings.TrimSpace(os.Getenv("OBJECT_STORAGE_DRIVER")),
		ObjectStorageBucket:                getEnv("OBJECT_STORAGE_BUCKET", "velane-data"),
		ObjectStoragePrefix:                strings.Trim(os.Getenv("OBJECT_STORAGE_PREFIX"), "/"),
		ObjectStorageS3Region:              getEnv("OBJECT_STORAGE_S3_REGION", "us-east-1"),
		ObjectStorageS3Endpoint:            strings.TrimSpace(os.Getenv("OBJECT_STORAGE_S3_ENDPOINT")),
		ObjectStorageS3ForcePathStyle:      getEnv("OBJECT_STORAGE_S3_FORCE_PATH_STYLE", "false") == "true",
		ObjectStorageAzureAccountURL:       strings.TrimSpace(os.Getenv("OBJECT_STORAGE_AZURE_ACCOUNT_URL")),
		ObjectStorageAzureConnectionString: strings.TrimSpace(os.Getenv("OBJECT_STORAGE_AZURE_CONNECTION_STRING")),
		ObjectGCGracePeriod:                getDurationEnv("OBJECT_GC_GRACE_PERIOD", 7*24*time.Hour),
		InvocationRetention:                getDurationEnv("INVOCATION_RETENTION", 0),
		PublicBaseURL:                      getEnv("PUBLIC_BASE_URL", "http://localhost:8092"),
		GoogleOAuthClientID:                os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
		GoogleOAuthClientSecret:            os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"),
		GitHubOAuthClientID:                os.Getenv("GITHUB_OAUTH_CLIENT_ID"),
		GitHubOAuthClientSecret:            os.Getenv("GITHUB_OAUTH_CLIENT_SECRET"),
		BootstrapEmail:                     os.Getenv("BOOTSTRAP_EMAIL"),
		BootstrapPassword:                  os.Getenv("BOOTSTRAP_PASSWORD"),
		BootstrapTenant:                    getEnv("BOOTSTRAP_TENANT", "default"),
		JWTPrivateKeyPEM:                   os.Getenv("JWT_PRIVATE_KEY"),
		SSOSAMLPrivateKeyPEM:               os.Getenv("SSO_SAML_PRIVATE_KEY"),
		SSOSAMLCertificatePEM:              os.Getenv("SSO_SAML_CERTIFICATE"),
		NangoInternalURL:                   getEnv("NANGO_INTERNAL_URL", "http://nango:3003"),
		NangoPublicURL:                     getEnv("NANGO_PUBLIC_URL", "http://localhost:3003"),
		NangoConnectURL:                    getEnv("NANGO_CONNECT_URL", "http://localhost:3009"),
		NangoApiURL:                        getEnv("NANGO_API_URL", "http://localhost:3003"),
		NangoSecretKey:                     os.Getenv("NANGO_SECRET_KEY"),
		NangoPublicKey:                     os.Getenv("NANGO_PUBLIC_KEY"),
		NangoWebhookSecret:                 os.Getenv("NANGO_WEBHOOK_SECRET"),
		MCPPublicURL:                       strings.TrimSpace(os.Getenv("MCP_PUBLIC_URL")),
		InternalProxyURL:                   getEnv("INTERNAL_PROXY_URL", "http://control-plane:8080"),
		LicenseKey:                         os.Getenv("VELANE_LICENSE_KEY"),
		CloudMode:                          os.Getenv("VELANE_CLOUD") == "true",
		ExecutorType:                       getEnv("EXECUTOR_TYPE", "process"),
		FirecrackerBinary:                  getEnv("FIRECRACKER_BINARY", "/usr/local/bin/firecracker"),
		FirecrackerJailerBinary:            getEnv("FIRECRACKER_JAILER_BINARY", "/usr/local/bin/jailer"),
		FirecrackerBunRootfs:               os.Getenv("FIRECRACKER_BUN_ROOTFS"),
		FirecrackerPythonRootfs:            os.Getenv("FIRECRACKER_PYTHON_ROOTFS"),
		FirecrackerKernelImage:             os.Getenv("FIRECRACKER_KERNEL_IMAGE"),
	}
}

// EncryptionKeyBytes parses EncryptionKey as a 64-character hex string (32 bytes)
// or generates a random ephemeral key if ENCRYPTION_KEY is empty.
// Logs a warning when generating an ephemeral key — not suitable for production.
func (c *Config) EncryptionKeyBytes(log *zap.Logger) []byte {
	if c.EncryptionKey != "" {
		key, err := hex.DecodeString(c.EncryptionKey)
		if err == nil && len(key) == 32 {
			return key
		}
		log.Warn("ENCRYPTION_KEY is set but invalid (must be 64 hex chars); generating ephemeral key",
			zap.Int("got_bytes", len(key)),
			zap.Error(err),
		)
	} else {
		log.Warn("ENCRYPTION_KEY not set; generating a random ephemeral key — secrets will not survive restarts")
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		// If we can't read random bytes, use a fixed fallback (still better than panic).
		log.Error("failed to generate random encryption key; using zeroed key", zap.Error(err))
		return make([]byte, 32)
	}
	return key
}

// JWTKeyPair returns the RSA private and public keys for JWT signing and validation.
// If JWTPrivateKeyPEM is empty, an ephemeral 2048-bit RSA key is generated with a warning.
func (c *Config) JWTKeyPair(log *zap.Logger) (*rsa.PrivateKey, *rsa.PublicKey) {
	if c.JWTPrivateKeyPEM == "" {
		log.Warn("JWT_PRIVATE_KEY not set — using ephemeral key, all tokens will be invalid after restart")
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			log.Fatal("failed to generate ephemeral JWT key", zap.Error(err))
		}
		return key, &key.PublicKey
	}

	block, _ := pem.Decode([]byte(c.JWTPrivateKeyPEM))
	if block == nil {
		log.Warn("JWT_PRIVATE_KEY PEM decode failed — using ephemeral key, all tokens will be invalid after restart")
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			log.Fatal("failed to generate ephemeral JWT key", zap.Error(err))
		}
		return key, &key.PublicKey
	}

	privKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS8 format.
		k, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			log.Warn("JWT_PRIVATE_KEY parse failed — using ephemeral key, all tokens will be invalid after restart",
				zap.Error(err),
			)
			key, _ := rsa.GenerateKey(rand.Reader, 2048)
			return key, &key.PublicKey
		}
		rsaKey, ok := k.(*rsa.PrivateKey)
		if !ok {
			log.Warn("JWT_PRIVATE_KEY is not an RSA key — using ephemeral key")
			key, _ := rsa.GenerateKey(rand.Reader, 2048)
			return key, &key.PublicKey
		}
		return rsaKey, &rsaKey.PublicKey
	}

	return privKey, &privKey.PublicKey
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return defaultValue
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value < 0 {
		return defaultValue
	}
	return value
}
