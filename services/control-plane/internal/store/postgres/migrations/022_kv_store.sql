-- Cross-invocation key-value store. Values are plaintext JSONB (not encrypted).
-- Identity is (tenant_id, namespace, key); namespace defaults to the literal 'default'.
CREATE TABLE IF NOT EXISTS kv_entries (
    id         TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    namespace  TEXT NOT NULL DEFAULT 'default',
    key        TEXT NOT NULL,
    value      JSONB NOT NULL,
    expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_kv_entries_identity
    ON kv_entries(tenant_id, namespace, key);

-- Prefix scan: text_pattern_ops makes key LIKE 'p%' index-usable under any collation.
CREATE INDEX IF NOT EXISTS idx_kv_entries_prefix
    ON kv_entries(tenant_id, namespace, key text_pattern_ops);

-- Reaper scan touches only rows that can ever expire.
CREATE INDEX IF NOT EXISTS idx_kv_entries_expires_at
    ON kv_entries(expires_at) WHERE expires_at IS NOT NULL;

ALTER TABLE tenants
  ADD COLUMN IF NOT EXISTS kv_limits JSONB NOT NULL DEFAULT '{
    "max_keys": 10000,
    "max_value_bytes": 131072,
    "max_total_bytes": 67108864
  }'::jsonb;
