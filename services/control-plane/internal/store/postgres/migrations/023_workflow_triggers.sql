CREATE UNIQUE INDEX IF NOT EXISTS snippets_id_tenant_unique ON snippets(id, tenant_id);
CREATE UNIQUE INDEX IF NOT EXISTS connections_id_tenant_unique ON connections(id, tenant_id);

CREATE TABLE IF NOT EXISTS workflow_triggers (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    workflow_id TEXT NOT NULL REFERENCES snippets(id) ON DELETE CASCADE,
    connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    provider_config_key TEXT NOT NULL,
    model TEXT NOT NULL CHECK (model ~ '^[A-Za-z][A-Za-z0-9_.-]{0,127}$'),
    change_types TEXT[] NOT NULL DEFAULT ARRAY['added','updated','deleted'],
    environment TEXT NOT NULL CHECK (environment IN ('dev','staging','prod')),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    activated_at TIMESTAMPTZ,
    last_delivery_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workflow_id, connection_id, model, environment),
    CHECK (change_types <@ ARRAY['added','updated','deleted']::TEXT[] AND cardinality(change_types) > 0)
);
DO $$ BEGIN
    ALTER TABLE workflow_triggers ADD CONSTRAINT workflow_triggers_workflow_tenant_fk FOREIGN KEY (workflow_id, tenant_id) REFERENCES snippets(id, tenant_id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
DO $$ BEGIN
    ALTER TABLE workflow_triggers ADD CONSTRAINT workflow_triggers_connection_tenant_fk FOREIGN KEY (connection_id, tenant_id) REFERENCES connections(id, tenant_id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE INDEX IF NOT EXISTS workflow_triggers_match_idx ON workflow_triggers(connection_id, model) WHERE enabled;

CREATE TABLE IF NOT EXISTS integration_event_receipts (
    id TEXT PRIMARY KEY,
    deduplication_key TEXT NOT NULL UNIQUE,
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
    provider_config_key TEXT NOT NULL,
    model TEXT NOT NULL,
    modified_after TEXT NOT NULL,
    initial_sync BOOLEAN NOT NULL DEFAULT FALSE,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','processing','completed','failed')),
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
DO $$ BEGIN
    ALTER TABLE integration_event_receipts ADD CONSTRAINT integration_receipts_connection_tenant_fk FOREIGN KEY (connection_id, tenant_id) REFERENCES connections(id, tenant_id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE IF NOT EXISTS workflow_trigger_dispatches (
    id TEXT PRIMARY KEY,
    receipt_id TEXT NOT NULL REFERENCES integration_event_receipts(id) ON DELETE CASCADE,
    trigger_id TEXT NOT NULL REFERENCES workflow_triggers(id) ON DELETE CASCADE,
    invocation_ids TEXT[] NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','completed','failed')),
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (receipt_id, trigger_id)
);
