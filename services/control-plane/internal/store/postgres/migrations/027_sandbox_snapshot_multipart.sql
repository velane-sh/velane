-- Immutable expectations are established before any capability is issued. Each
-- encrypted snapshot chunk is a distinct S3 object and multipart session.
CREATE TABLE IF NOT EXISTS sandbox_snapshot_expected_artifacts (
  id TEXT PRIMARY KEY,
  snapshot_id TEXT NOT NULL REFERENCES sandbox_snapshots(id) ON DELETE CASCADE,
  artifact_type TEXT NOT NULL CHECK(artifact_type IN('memory.full','vmstate.full','drive.full')),
  drive_id TEXT NOT NULL DEFAULT '',
  logical_size BIGINT NOT NULL CHECK(logical_size > 0),
  logical_sha256 TEXT NOT NULL,
  UNIQUE(snapshot_id, artifact_type, drive_id)
);
CREATE TABLE IF NOT EXISTS sandbox_snapshot_artifact_chunks (
  id TEXT PRIMARY KEY,
  expected_artifact_id TEXT NOT NULL REFERENCES sandbox_snapshot_expected_artifacts(id) ON DELETE CASCADE,
  chunk_index INT NOT NULL CHECK(chunk_index >= 0),
  plaintext_size BIGINT NOT NULL CHECK(plaintext_size > 0),
  ciphertext_size BIGINT NOT NULL CHECK(ciphertext_size > 0),
  plaintext_sha256 TEXT NOT NULL,
  ciphertext_sha256 TEXT NOT NULL,
  nonce TEXT NOT NULL,
  object_ref TEXT NOT NULL UNIQUE,
  object_version TEXT NOT NULL DEFAULT '',
  object_etag TEXT NOT NULL DEFAULT '',
  object_checksum_sha256 TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL CHECK(state IN('planned','uploading','completed','verified','aborted')),
  UNIQUE(expected_artifact_id, chunk_index)
);
CREATE TABLE IF NOT EXISTS sandbox_snapshot_multipart_uploads (
  id TEXT PRIMARY KEY,
  chunk_id TEXT NOT NULL UNIQUE REFERENCES sandbox_snapshot_artifact_chunks(id) ON DELETE CASCADE,
  provider_upload_id TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  state TEXT NOT NULL CHECK(state IN('active','completed','aborted','expired')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS sandbox_snapshot_multipart_parts (
  upload_id TEXT NOT NULL REFERENCES sandbox_snapshot_multipart_uploads(id) ON DELETE CASCADE,
  part_number INT NOT NULL CHECK(part_number > 0),
  expected_size BIGINT NOT NULL CHECK(expected_size > 0),
  expected_checksum_sha256 TEXT NOT NULL,
  etag TEXT NOT NULL DEFAULT '',
  completed_checksum_sha256 TEXT NOT NULL DEFAULT '',
  PRIMARY KEY(upload_id, part_number)
);
CREATE TABLE IF NOT EXISTS sandbox_snapshot_upload_evidence (
  chunk_id TEXT PRIMARY KEY REFERENCES sandbox_snapshot_artifact_chunks(id) ON DELETE CASCADE,
  provider_upload_id TEXT NOT NULL,
  part_number INT NOT NULL CHECK(part_number > 0),
  completed_etag TEXT NOT NULL,
  completed_checksum_sha256 TEXT NOT NULL,
  object_ref TEXT NOT NULL,
  object_version TEXT NOT NULL,
  object_etag TEXT NOT NULL,
  object_checksum_sha256 TEXT NOT NULL,
  object_size BIGINT NOT NULL CHECK(object_size > 0),
  verified_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS sandbox_snapshot_multipart_uploads_expiry_idx ON sandbox_snapshot_multipart_uploads(expires_at) WHERE state='active';
