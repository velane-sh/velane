CREATE TABLE IF NOT EXISTS sso_connections (
    id                    TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    tenant_id             TEXT NOT NULL UNIQUE REFERENCES tenants(id) ON DELETE CASCADE,
    protocol              TEXT NOT NULL CHECK (protocol IN ('oidc', 'saml')),
    display_name          TEXT NOT NULL,
    config_encrypted      TEXT NOT NULL,
    enabled               BOOLEAN NOT NULL DEFAULT false,
    enforced              BOOLEAN NOT NULL DEFAULT false,
    tested_at             TIMESTAMPTZ,
    default_role          TEXT NOT NULL DEFAULT 'invoke' CHECK (default_role IN ('invoke', 'manage')),
    break_glass_user_id   TEXT REFERENCES users(id) ON DELETE SET NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS sso_external_identities (
    id              TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    connection_id   TEXT NOT NULL REFERENCES sso_connections(id) ON DELETE CASCADE,
    user_id         TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    subject         TEXT NOT NULL,
    email           TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (connection_id, subject),
    UNIQUE (connection_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_sso_identities_tenant ON sso_external_identities(tenant_id);

ALTER TABLE refresh_tokens ADD COLUMN IF NOT EXISTS auth_method TEXT NOT NULL DEFAULT 'local';
ALTER TABLE refresh_tokens ADD COLUMN IF NOT EXISTS sso_tenant_id TEXT REFERENCES tenants(id) ON DELETE CASCADE;
ALTER TABLE refresh_tokens ADD COLUMN IF NOT EXISTS sso_connection_id TEXT REFERENCES sso_connections(id) ON DELETE CASCADE;
