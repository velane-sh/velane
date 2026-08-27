package postgres

import (
	"context"
	"crypto/ed25519"
	_ "embed"
	"fmt"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/objectstore"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/001_initial.sql
var migrationSQL1 string

//go:embed migrations/002_phase2.sql
var migrationSQL2 string

//go:embed migrations/003_phase3.sql
var migrationSQL3 string

//go:embed migrations/004_phase4.sql
var migrationSQL4 string

//go:embed migrations/005_phase5.sql
var migrationSQL5 string

//go:embed migrations/006_embed.sql
var migrationSQL6 string

//go:embed migrations/007_phase8.sql
var migrationSQL7 string

//go:embed migrations/008_phase9.sql
var migrationSQL8 string

//go:embed migrations/009_variables.sql
var migrationSQL9 string

//go:embed migrations/010_libraries.sql
var migrationSQL10 string

//go:embed migrations/011_integration.sql
var migrationSQL11 string

//go:embed migrations/012_connections.sql
var migrationSQL12 string

//go:embed migrations/013_connections_alias.sql
var migrationSQL13 string

//go:embed migrations/014_integration_credential_profiles.sql
var migrationSQL14 string

//go:embed migrations/015_oauth.sql
var migrationSQL15 string

//go:embed migrations/016_runtime_limits.sql
var migrationSQL16 string

//go:embed migrations/017_version_runtime_defaults.sql
var migrationSQL17 string

//go:embed migrations/018_soft_delete_snippets.sql
var migrationSQL18 string

//go:embed migrations/019_tenant_license_key.sql
var migrationSQL19 string

//go:embed migrations/020_object_storage.sql
var migrationSQL20 string

//go:embed migrations/021_enterprise_sso.sql
var migrationSQL21 string

//go:embed migrations/022_kv_store.sql
var migrationSQL22 string

//go:embed migrations/023_workflow_triggers.sql
var migrationSQL23 string

//go:embed migrations/024_sandbox_control_plane.sql
var migrationSQL24 string

//go:embed migrations/025_sandbox_host_enrollment.sql
var migrationSQL25 string

//go:embed migrations/026_sandbox_dispatch_fencing.sql
var migrationSQL26 string

//go:embed migrations/027_sandbox_snapshot_multipart.sql
var migrationSQL27 string

//go:embed migrations/028_sandbox_lifecycle_artifact_manifest.sql
var migrationSQL28 string

//go:embed migrations/029_rbac_user_groups.sql
var migrationSQL29 string

// Store wraps a pgxpool.Pool and provides all database operations.
type Store struct {
	pool                *pgxpool.Pool
	objects             objectstore.Store
	objectGCGrace       time.Duration
	invocationRetention time.Duration
	watchdogSigner      ed25519.PrivateKey
	snapshotObjects     objectstore.SnapshotStore
}

// ConfigureWatchdogLeaseSigner installs the private signing key used only for
// short-lived watchdog grants. A nil key deliberately makes VM dispatch
// unavailable rather than issuing an unsigned grant.
func (s *Store) ConfigureWatchdogLeaseSigner(key ed25519.PrivateKey) {
	s.watchdogSigner = append(ed25519.PrivateKey(nil), key...)
}

func (s *Store) SetObjectStore(objects objectstore.Store) {
	s.objects = objects
	if snapshots, ok := objects.(objectstore.SnapshotStore); ok {
		s.snapshotObjects = snapshots
	}
}

// SetSnapshotObjectStore is useful for narrow S3 fakes in snapshot tests.
func (s *Store) SetSnapshotObjectStore(objects objectstore.SnapshotStore) {
	s.snapshotObjects = objects
}

// HasSnapshotObjectStore reports the instantiated multipart provider, not an
// environment label. It is used only for fail-closed capability admission.
func (s *Store) HasSnapshotObjectStore() bool { return s != nil && s.snapshotObjects != nil }

func (s *Store) ConfigureObjectMaintenance(gcGrace, invocationRetention time.Duration) {
	s.objectGCGrace = gcGrace
	s.invocationRetention = invocationRetention
}

// New connects to Postgres using the provided DSN, runs the embedded migration
// SQL to ensure the schema is up to date, and returns a ready Store.
func New(ctx context.Context, dsn string) (*Store, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres ping: %w", err)
	}

	for i, sql := range []string{migrationSQL1, migrationSQL2, migrationSQL3, migrationSQL4, migrationSQL5, migrationSQL6, migrationSQL7, migrationSQL8, migrationSQL9, migrationSQL10, migrationSQL11, migrationSQL12, migrationSQL13, migrationSQL14, migrationSQL15, migrationSQL16, migrationSQL17, migrationSQL18, migrationSQL19, migrationSQL20, migrationSQL21, migrationSQL22, migrationSQL23, migrationSQL24, migrationSQL25, migrationSQL26, migrationSQL27, migrationSQL28, migrationSQL29} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			pool.Close()
			return nil, fmt.Errorf("running migration %d: %w", i+1, err)
		}
	}

	return &Store{pool: pool}, nil
}

// Close releases all pool connections.
func (s *Store) Close() {
	s.pool.Close()
}

// Pool exposes the pgx pool to the sandbox control-plane service for tightly
// scoped read-only recipe/profile resolution. Invocation code does not use it.
func (s *Store) Pool() *pgxpool.Pool { return s.pool }
