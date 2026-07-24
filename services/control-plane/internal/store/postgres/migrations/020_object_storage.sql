-- Object storage is authoritative for workflow source and invocation payloads.
-- Legacy text columns remain during the compatibility/backfill window.
ALTER TABLE snippet_versions
    ADD COLUMN IF NOT EXISTS object_ref TEXT,
    ADD COLUMN IF NOT EXISTS object_checksum TEXT,
    ADD COLUMN IF NOT EXISTS object_size BIGINT,
    ADD COLUMN IF NOT EXISTS content_state TEXT NOT NULL DEFAULT 'legacy'
        CHECK (content_state IN ('legacy', 'uploading', 'ready', 'failed', 'deleted'));

ALTER TABLE invocations
    ADD COLUMN IF NOT EXISTS payload_ref TEXT,
    ADD COLUMN IF NOT EXISTS payload_checksum TEXT,
    ADD COLUMN IF NOT EXISTS payload_size BIGINT,
    ADD COLUMN IF NOT EXISTS payload_state TEXT NOT NULL DEFAULT 'legacy'
        CHECK (payload_state IN ('legacy', 'pending', 'stored', 'failed', 'purged')),
    ADD COLUMN IF NOT EXISTS payload_outbox BYTEA,
    ADD COLUMN IF NOT EXISTS payload_attempts INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS payload_retry_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_invocations_payload_retry
    ON invocations(payload_retry_at)
    WHERE payload_outbox IS NOT NULL;

ALTER TABLE snippets
    ADD COLUMN IF NOT EXISTS objects_delete_after TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS objects_deleted_at TIMESTAMPTZ;
