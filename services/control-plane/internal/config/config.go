package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
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
	ObjectStorageS3KMSKeyID            string
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

	// Durable sandbox control plane. Disabled until operator capacity and private mTLS are configured.
	SandboxControlEnabled                  bool
	SandboxHostListenAddr                  string
	SandboxHostCAFile                      string
	SandboxHostCertFile                    string
	SandboxHostKeyFile                     string
	SandboxHostSigningCAFile               string
	SandboxHostSigningKeyFile              string
	SandboxHostCertificateTTL              time.Duration
	SandboxHostAWSPoolID                   string
	SandboxHostAWSAccountID                string
	SandboxHostAWSRegion                   string
	SandboxHostAWSASGName                  string
	SandboxHostAWSAMI                      string
	SandboxHostAWSLaunchTemplateID         string
	SandboxHostAWSIIDRootCAFile            string
	SandboxHostExpectedLineageID           string
	SandboxHostExpectedCompatibilityKey    string
	SandboxWatchdogSigningPrivateKeyBase64 string
	// Public sandbox mutation admission. These settings are distinct from host
	// enrollment: each represents a complete operational dependency.
	SandboxHostCommandPathEnabled bool
	SandboxCommandPayloadsEnabled bool
	SandboxSnapshotArtifactStore  string
	SandboxSnapshotKeyWrapper     string
	SandboxImageBuilderEndpoint   string
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
		DatabaseURL:                            getEnv("DATABASE_URL", "postgres://velane:velane@localhost:5432/velane"),
		BunExecutorURL:                         getEnv("BUN_EXECUTOR_URL", "http://localhost:8081"),
		PythonExecutorURL:                      getEnv("PYTHON_EXECUTOR_URL", "http://localhost:8082"),
		Port:                                   getEnv("PORT", "8080"),
		RedisURL:                               getEnv("REDIS_URL", "localhost:6379"),
		WorkerCount:                            workerCount,
		EncryptionKey:                          os.Getenv("ENCRYPTION_KEY"),
		ClickHouseDSN:                          os.Getenv("CLICKHOUSE_DSN"),
		LogsBucket:                             os.Getenv("LOGS_BUCKET"),
		ReplayBucket:                           os.Getenv("REPLAY_BUCKET"),
		ObjectStorageDriver:                    strings.TrimSpace(os.Getenv("OBJECT_STORAGE_DRIVER")),
		ObjectStorageBucket:                    getEnv("OBJECT_STORAGE_BUCKET", "velane-data"),
		ObjectStoragePrefix:                    strings.Trim(os.Getenv("OBJECT_STORAGE_PREFIX"), "/"),
		ObjectStorageS3Region:                  getEnv("OBJECT_STORAGE_S3_REGION", "us-east-1"),
		ObjectStorageS3Endpoint:                strings.TrimSpace(os.Getenv("OBJECT_STORAGE_S3_ENDPOINT")),
		ObjectStorageS3ForcePathStyle:          getEnv("OBJECT_STORAGE_S3_FORCE_PATH_STYLE", "false") == "true",
		ObjectStorageS3KMSKeyID:                strings.TrimSpace(os.Getenv("OBJECT_STORAGE_S3_KMS_KEY_ID")),
		ObjectStorageAzureAccountURL:           strings.TrimSpace(os.Getenv("OBJECT_STORAGE_AZURE_ACCOUNT_URL")),
		ObjectStorageAzureConnectionString:     strings.TrimSpace(os.Getenv("OBJECT_STORAGE_AZURE_CONNECTION_STRING")),
		ObjectGCGracePeriod:                    getDurationEnv("OBJECT_GC_GRACE_PERIOD", 7*24*time.Hour),
		InvocationRetention:                    getDurationEnv("INVOCATION_RETENTION", 0),
		PublicBaseURL:                          getEnv("PUBLIC_BASE_URL", "http://localhost:8092"),
		GoogleOAuthClientID:                    os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
		GoogleOAuthClientSecret:                os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"),
		GitHubOAuthClientID:                    os.Getenv("GITHUB_OAUTH_CLIENT_ID"),
		GitHubOAuthClientSecret:                os.Getenv("GITHUB_OAUTH_CLIENT_SECRET"),
		BootstrapEmail:                         os.Getenv("BOOTSTRAP_EMAIL"),
		BootstrapPassword:                      os.Getenv("BOOTSTRAP_PASSWORD"),
		BootstrapTenant:                        getEnv("BOOTSTRAP_TENANT", "default"),
		JWTPrivateKeyPEM:                       os.Getenv("JWT_PRIVATE_KEY"),
		SSOSAMLPrivateKeyPEM:                   os.Getenv("SSO_SAML_PRIVATE_KEY"),
		SSOSAMLCertificatePEM:                  os.Getenv("SSO_SAML_CERTIFICATE"),
		NangoInternalURL:                       getEnv("NANGO_INTERNAL_URL", "http://nango:3003"),
		NangoPublicURL:                         getEnv("NANGO_PUBLIC_URL", "http://localhost:3003"),
		NangoConnectURL:                        getEnv("NANGO_CONNECT_URL", "http://localhost:3009"),
		NangoApiURL:                            getEnv("NANGO_API_URL", "http://localhost:3003"),
		NangoSecretKey:                         os.Getenv("NANGO_SECRET_KEY"),
		NangoPublicKey:                         os.Getenv("NANGO_PUBLIC_KEY"),
		NangoWebhookSecret:                     os.Getenv("NANGO_WEBHOOK_SECRET"),
		MCPPublicURL:                           strings.TrimSpace(os.Getenv("MCP_PUBLIC_URL")),
		InternalProxyURL:                       getEnv("INTERNAL_PROXY_URL", "http://control-plane:8080"),
		LicenseKey:                             os.Getenv("VELANE_LICENSE_KEY"),
		CloudMode:                              os.Getenv("VELANE_CLOUD") == "true",
		ExecutorType:                           getEnv("EXECUTOR_TYPE", "process"),
		FirecrackerBinary:                      getEnv("FIRECRACKER_BINARY", "/usr/local/bin/firecracker"),
		FirecrackerJailerBinary:                getEnv("FIRECRACKER_JAILER_BINARY", "/usr/local/bin/jailer"),
		FirecrackerBunRootfs:                   os.Getenv("FIRECRACKER_BUN_ROOTFS"),
		FirecrackerPythonRootfs:                os.Getenv("FIRECRACKER_PYTHON_ROOTFS"),
		FirecrackerKernelImage:                 os.Getenv("FIRECRACKER_KERNEL_IMAGE"),
		SandboxControlEnabled:                  getEnv("SANDBOX_CONTROL_ENABLED", "false") == "true",
		SandboxHostListenAddr:                  getEnv("SANDBOX_HOST_LISTEN_ADDR", ":8443"),
		SandboxHostCAFile:                      strings.TrimSpace(os.Getenv("SANDBOX_HOST_CA_FILE")),
		SandboxHostCertFile:                    strings.TrimSpace(os.Getenv("SANDBOX_HOST_CERT_FILE")),
		SandboxHostKeyFile:                     strings.TrimSpace(os.Getenv("SANDBOX_HOST_KEY_FILE")),
		SandboxHostSigningCAFile:               strings.TrimSpace(os.Getenv("SANDBOX_HOST_SIGNING_CA_FILE")),
		SandboxHostSigningKeyFile:              strings.TrimSpace(os.Getenv("SANDBOX_HOST_SIGNING_KEY_FILE")),
		SandboxHostCertificateTTL:              getDurationEnv("SANDBOX_HOST_CERTIFICATE_TTL", 24*time.Hour),
		SandboxHostAWSPoolID:                   strings.TrimSpace(os.Getenv("SANDBOX_HOST_AWS_POOL_ID")),
		SandboxHostAWSAccountID:                strings.TrimSpace(os.Getenv("SANDBOX_HOST_AWS_ACCOUNT_ID")),
		SandboxHostAWSRegion:                   strings.TrimSpace(os.Getenv("SANDBOX_HOST_AWS_REGION")),
		SandboxHostAWSASGName:                  strings.TrimSpace(os.Getenv("SANDBOX_HOST_AWS_ASG_NAME")),
		SandboxHostAWSAMI:                      strings.TrimSpace(os.Getenv("SANDBOX_HOST_AWS_AMI_ID")),
		SandboxHostAWSLaunchTemplateID:         strings.TrimSpace(os.Getenv("SANDBOX_HOST_AWS_LAUNCH_TEMPLATE_ID")),
		SandboxHostAWSIIDRootCAFile:            strings.TrimSpace(os.Getenv("SANDBOX_HOST_AWS_IID_ROOT_CA_FILE")),
		SandboxHostExpectedLineageID:           strings.TrimSpace(os.Getenv("SANDBOX_HOST_EXPECTED_LINEAGE_ID")),
		SandboxHostExpectedCompatibilityKey:    strings.TrimSpace(os.Getenv("SANDBOX_HOST_EXPECTED_COMPATIBILITY_KEY")),
		SandboxWatchdogSigningPrivateKeyBase64: strings.TrimSpace(os.Getenv("SANDBOX_WATCHDOG_SIGNING_PRIVATE_KEY_BASE64")),
		SandboxHostCommandPathEnabled:          getEnv("SANDBOX_HOST_COMMAND_PATH_ENABLED", "false") == "true",
		SandboxCommandPayloadsEnabled:          getEnv("SANDBOX_COMMAND_PAYLOADS_ENABLED", "false") == "true",
		SandboxSnapshotArtifactStore:           strings.TrimSpace(os.Getenv("SANDBOX_SNAPSHOT_ARTIFACT_STORE")),
		SandboxSnapshotKeyWrapper:              strings.TrimSpace(os.Getenv("SANDBOX_SNAPSHOT_KEY_WRAPPER")),
		SandboxImageBuilderEndpoint:            strings.TrimSpace(os.Getenv("SANDBOX_IMAGE_BUILDER_ENDPOINT")),
	}
}

// SandboxHostProtocolConfigured reports whether the private host enrollment
// and signed command protocol have every configured prerequisite. It does not
// assert that a host is currently available; placement remains responsible for
// that runtime decision.
func (c Config) SandboxHostProtocolConfigured() bool {
	return c.SandboxControlEnabled &&
		c.SandboxHostCAFile != "" &&
		c.SandboxHostCertFile != "" &&
		c.SandboxHostKeyFile != "" &&
		c.SandboxHostSigningCAFile != "" &&
		c.SandboxHostSigningKeyFile != "" &&
		c.SandboxHostCertificateTTL > 0 && c.SandboxHostCertificateTTL <= 24*time.Hour &&
		c.SandboxHostAWSPoolID != "" &&
		c.SandboxHostAWSAccountID != "" &&
		c.SandboxHostAWSRegion != "" &&
		c.SandboxHostAWSASGName != "" &&
		c.SandboxHostAWSAMI != "" &&
		c.SandboxHostAWSLaunchTemplateID != "" &&
		c.SandboxHostAWSIIDRootCAFile != "" &&
		c.SandboxHostExpectedLineageID != "" &&
		c.SandboxHostExpectedCompatibilityKey != "" &&
		c.SandboxWatchdogSigningPrivateKeyBase64 != ""
}

func (c Config) SandboxWatchdogSigningKey() ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(c.SandboxWatchdogSigningPrivateKeyBase64)
	if err != nil || len(key) != 64 {
		return nil, fmt.Errorf("SANDBOX_WATCHDOG_SIGNING_PRIVATE_KEY_BASE64 must contain a base64-encoded 64-byte Ed25519 private key")
	}
	return key, nil
}

// SandboxHostTLSConfig loads the private host listener credentials and requires
// every client to present a certificate chained to the configured host CA.
func (c Config) SandboxHostTLSConfig() (*tls.Config, error) {
	if c.SandboxHostCAFile == "" || c.SandboxHostCertFile == "" || c.SandboxHostKeyFile == "" {
		return nil, fmt.Errorf("SANDBOX_HOST_CA_FILE, SANDBOX_HOST_CERT_FILE, and SANDBOX_HOST_KEY_FILE are required when sandbox control is enabled")
	}
	caPEM, err := os.ReadFile(c.SandboxHostCAFile)
	if err != nil {
		return nil, fmt.Errorf("read sandbox host CA: %w", err)
	}
	clientCAs := x509.NewCertPool()
	if ok := clientCAs.AppendCertsFromPEM(caPEM); !ok {
		return nil, fmt.Errorf("parse sandbox host CA: no certificates found")
	}
	certificate, err := tls.LoadX509KeyPair(c.SandboxHostCertFile, c.SandboxHostKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load sandbox host server certificate: %w", err)
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{certificate},
		ClientCAs:    clientCAs,
		// Bootstrap routes use server-authenticated TLS and reject all requests
		// except a persisted, one-use provider attestation. Host route handlers
		// require and validate mTLS identities for every other operation.
		ClientAuth: tls.VerifyClientCertIfGiven,
	}, nil
}

func (c Config) SandboxHostAWSVerifierConfig() (poolID, accountID, region, asgName, amiID, launchTemplateID string, err error) {
	values := []string{c.SandboxHostAWSPoolID, c.SandboxHostAWSAccountID, c.SandboxHostAWSRegion, c.SandboxHostAWSASGName, c.SandboxHostAWSAMI, c.SandboxHostAWSLaunchTemplateID}
	for _, value := range values {
		if value == "" {
			return "", "", "", "", "", "", fmt.Errorf("SANDBOX_HOST_AWS_POOL_ID, SANDBOX_HOST_AWS_ACCOUNT_ID, SANDBOX_HOST_AWS_REGION, SANDBOX_HOST_AWS_ASG_NAME, SANDBOX_HOST_AWS_AMI_ID, and SANDBOX_HOST_AWS_LAUNCH_TEMPLATE_ID are required")
		}
	}
	return values[0], values[1], values[2], values[3], values[4], values[5], nil
}

func (c Config) SandboxHostExpectedIdentity() (lineageID, compatibilityKey string, err error) {
	if c.SandboxHostExpectedLineageID == "" || c.SandboxHostExpectedCompatibilityKey == "" {
		return "", "", fmt.Errorf("SANDBOX_HOST_EXPECTED_LINEAGE_ID and SANDBOX_HOST_EXPECTED_COMPATIBILITY_KEY are required")
	}
	return c.SandboxHostExpectedLineageID, c.SandboxHostExpectedCompatibilityKey, nil
}

func (c Config) SandboxHostAWSIIDTrustRoots() (*x509.CertPool, error) {
	if c.SandboxHostAWSIIDRootCAFile == "" {
		return nil, fmt.Errorf("SANDBOX_HOST_AWS_IID_ROOT_CA_FILE is required")
	}
	pemBytes, err := os.ReadFile(c.SandboxHostAWSIIDRootCAFile)
	if err != nil {
		return nil, fmt.Errorf("read AWS EC2 IID root CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("parse AWS EC2 IID root CA: no certificates found")
	}
	return roots, nil
}

// SandboxHostSignerConfig validates the persistent installation-local CA used
// only to sign short-lived host certificates. It must never fall back to an
// ephemeral key while durable sandbox control is enabled.
func (c Config) SandboxHostSignerConfig() (string, string, time.Duration, error) {
	if c.SandboxHostSigningCAFile == "" || c.SandboxHostSigningKeyFile == "" {
		return "", "", 0, fmt.Errorf("SANDBOX_HOST_SIGNING_CA_FILE and SANDBOX_HOST_SIGNING_KEY_FILE are required when sandbox control is enabled")
	}
	if c.SandboxHostCertificateTTL <= 0 || c.SandboxHostCertificateTTL > 24*time.Hour {
		return "", "", 0, fmt.Errorf("SANDBOX_HOST_CERTIFICATE_TTL must be greater than zero and at most 24h")
	}
	return c.SandboxHostSigningCAFile, c.SandboxHostSigningKeyFile, c.SandboxHostCertificateTTL, nil
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
