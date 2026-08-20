CREATE TABLE IF NOT EXISTS sandbox_host_enrollment_challenges (
  id TEXT PRIMARY KEY,
  nonce_hash TEXT NOT NULL UNIQUE,
  pool_id TEXT NOT NULL REFERENCES sandbox_host_pools(id),
  csr_sha256 TEXT NOT NULL,
  provider TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  consumed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS sandbox_host_enrollment_challenges_active_idx ON sandbox_host_enrollment_challenges(pool_id, expires_at) WHERE consumed_at IS NULL;
ALTER TABLE sandbox_hosts ADD COLUMN IF NOT EXISTS certificate_serial TEXT NOT NULL DEFAULT '';
ALTER TABLE sandbox_hosts ADD COLUMN IF NOT EXISTS certificate_expires_at TIMESTAMPTZ;
ALTER TABLE sandbox_hosts ADD COLUMN IF NOT EXISTS certificate_revoked_at TIMESTAMPTZ;
