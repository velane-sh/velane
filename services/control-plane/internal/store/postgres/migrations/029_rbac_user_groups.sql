-- Role-based access control: user groups, integration grants and caller-aware API keys.

CREATE TABLE IF NOT EXISTS user_groups (
    id          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_user_groups_tenant_id ON user_groups(tenant_id);

CREATE TABLE IF NOT EXISTS user_group_members (
    group_id  TEXT NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
    user_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    added_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_user_group_members_user_id ON user_group_members(user_id);

CREATE TABLE IF NOT EXISTS integration_group_grants (
    group_id              TEXT NOT NULL REFERENCES user_groups(id) ON DELETE CASCADE,
    credential_profile_id TEXT NOT NULL REFERENCES integration_credential_profiles(id) ON DELETE CASCADE,
    granted_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, credential_profile_id)
);

CREATE INDEX IF NOT EXISTS idx_integration_group_grants_profile
    ON integration_group_grants(credential_profile_id);

-- API keys inherit the group access of the user that created them. Existing keys
-- stay NULL, which means "tenant-wide" and keeps pre-RBAC deployments working.
ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS user_id TEXT REFERENCES users(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id);

-- When strict mode is on, callers without a resolvable user (legacy tenant-wide
-- API keys) lose access to integrations instead of seeing every one of them.
ALTER TABLE tenants
    ADD COLUMN IF NOT EXISTS rbac_strict_mode BOOLEAN NOT NULL DEFAULT FALSE;

-- Widen the member role set. admin/manage/invoke keep working unchanged.
ALTER TABLE tenant_members DROP CONSTRAINT IF EXISTS tenant_members_role_check;
ALTER TABLE tenant_members
    ADD CONSTRAINT tenant_members_role_check
    CHECK (role IN ('invoke', 'manage', 'admin', 'owner', 'integration_manager', 'member', 'viewer'));

ALTER TABLE invite_tokens DROP CONSTRAINT IF EXISTS invite_tokens_role_check;
ALTER TABLE invite_tokens
    ADD CONSTRAINT invite_tokens_role_check
    CHECK (role IN ('invoke', 'manage', 'admin', 'owner', 'integration_manager', 'member', 'viewer'));
