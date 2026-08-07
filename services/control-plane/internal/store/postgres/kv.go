package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/abskrj/velane/services/control-plane/internal/ids"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	kvListDefaultLimit = 50
	kvListMaxLimit     = 200
	KVReaperBatchSize  = 500
)

var (
	ErrKVNotFound       = errors.New("kv entry not found")
	ErrKVValueTooLarge  = errors.New("kv value exceeds size limit")
	ErrKVKeyQuota       = errors.New("kv key quota exceeded")
	ErrKVBytesQuota     = errors.New("kv storage quota exceeded")
	ErrKVTenantNotFound = errors.New("kv tenant not found")
	ErrKVInvalidValue   = errors.New("kv value is not valid json")
)

type KVSetInput struct {
	Namespace  string
	Key        string
	Value      json.RawMessage
	TTLSeconds *int64 // nil => expires_at NULL
}

type KVListFilters struct {
	// Namespace is a pointer so nil means every namespace, while a pointer to
	// "default" means the default namespace only.
	Namespace *string
	KeyPrefix string
	Limit     int
	Offset    int
}

// SetKV atomically canonicalizes a value, enforces the tenant quota, and upserts it.
func (s *Store) SetKV(ctx context.Context, tenantID string, in KVSetInput) (_ *models.KVEntry, err error) {
	namespace := models.NormalizeKVNamespace(in.Namespace)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("SetKV begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var limitsJSON []byte
	if err := tx.QueryRow(ctx, `SELECT kv_limits FROM tenants WHERE id = $1 FOR UPDATE`, tenantID).Scan(&limitsJSON); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrKVTenantNotFound
		}
		return nil, fmt.Errorf("SetKV lock tenant: %w", err)
	}
	limits, err := decodeKVLimits(limitsJSON)
	if err != nil {
		return nil, fmt.Errorf("SetKV decode limits: %w", err)
	}

	var usageBytes, keys, existingBytes, existingKeys, newBytes int64
	err = tx.QueryRow(ctx, `SELECT COALESCE(SUM(octet_length(value::text)), 0),
       COUNT(*),
       COALESCE(SUM(octet_length(value::text)) FILTER (WHERE namespace = $2 AND key = $3), 0),
       COUNT(*) FILTER (WHERE namespace = $2 AND key = $3),
       octet_length($4::jsonb::text)
FROM kv_entries
WHERE tenant_id = $1 AND (expires_at IS NULL OR expires_at > now())`,
		tenantID, namespace, in.Key, string(in.Value),
	).Scan(&usageBytes, &keys, &existingBytes, &existingKeys, &newBytes)
	if err != nil {
		if isInvalidJSONError(err) {
			return nil, ErrKVInvalidValue
		}
		return nil, fmt.Errorf("SetKV usage: %w", err)
	}
	if newBytes > limits.MaxValueBytes {
		return nil, ErrKVValueTooLarge
	}
	if existingKeys == 0 && keys >= limits.MaxKeys {
		return nil, ErrKVKeyQuota
	}
	if usageBytes-existingBytes+newBytes > limits.MaxTotalBytes {
		return nil, ErrKVBytesQuota
	}

	entry, err := scanKVEntry(tx.QueryRow(ctx, `INSERT INTO kv_entries (id, tenant_id, namespace, key, value, expires_at)
VALUES ($1, $2, $3, $4, $5::jsonb,
        CASE WHEN $6::bigint IS NULL THEN NULL ELSE now() + make_interval(secs => $6::bigint) END)
ON CONFLICT (tenant_id, namespace, key) DO UPDATE
   SET value = EXCLUDED.value, expires_at = EXCLUDED.expires_at, updated_at = now()
RETURNING id, tenant_id, namespace, key, value, octet_length(value::text), expires_at, created_at, updated_at`,
		ids.New(), tenantID, namespace, in.Key, string(in.Value), in.TTLSeconds,
	))
	if err != nil {
		if isInvalidJSONError(err) {
			return nil, ErrKVInvalidValue
		}
		return nil, fmt.Errorf("SetKV upsert: %w", err)
	}
	if entry.SizeBytes != newBytes {
		return nil, fmt.Errorf("SetKV canonical size mismatch: computed %d, returned %d", newBytes, entry.SizeBytes)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("SetKV commit: %w", err)
	}
	return entry, nil
}

// GetKV returns one live KV entry scoped to its tenant.
func (s *Store) GetKV(ctx context.Context, tenantID, namespace, key string) (*models.KVEntry, error) {
	entry, err := scanKVEntry(s.pool.QueryRow(ctx, `SELECT id, tenant_id, namespace, key, value,
       octet_length(value::text), expires_at, created_at, updated_at
FROM kv_entries
WHERE tenant_id = $1 AND namespace = $2 AND key = $3
  AND (expires_at IS NULL OR expires_at > now())`, tenantID, models.NormalizeKVNamespace(namespace), key))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrKVNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("GetKV scan: %w", err)
	}
	return entry, nil
}

// DeleteKV deletes an entry, including a physically present expired entry, and returns its row ID.
func (s *Store) DeleteKV(ctx context.Context, tenantID, namespace, key string) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `DELETE FROM kv_entries
WHERE tenant_id = $1 AND namespace = $2 AND key = $3
RETURNING id`, tenantID, models.NormalizeKVNamespace(namespace), key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrKVNotFound
	}
	if err != nil {
		return "", fmt.Errorf("DeleteKV: %w", err)
	}
	return id, nil
}

// ListKV returns redacted live entries and the matching total.
func (s *Store) ListKV(ctx context.Context, tenantID string, f KVListFilters) ([]*models.KVEntry, int64, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = kvListDefaultLimit
	}
	if limit > kvListMaxLimit {
		limit = kvListMaxLimit
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	clauses := []string{"tenant_id = $1", "(expires_at IS NULL OR expires_at > now())"}
	args := []any{tenantID}
	argPos := 2
	if f.Namespace != nil {
		clauses = append(clauses, fmt.Sprintf("namespace = $%d", argPos))
		args = append(args, models.NormalizeKVNamespace(*f.Namespace))
		argPos++
	}
	escapedPrefix := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(f.KeyPrefix)
	clauses = append(clauses, fmt.Sprintf("($%d = '' OR key LIKE $%d || '%%' ESCAPE '\\')", argPos, argPos))
	args = append(args, escapedPrefix)
	argPos++
	where := strings.Join(clauses, " AND ")

	var total int64
	if err := s.pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM kv_entries WHERE %s`, where), args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("ListKV count: %w", err)
	}
	query := fmt.Sprintf(`SELECT id, tenant_id, namespace, key, octet_length(value::text), expires_at, created_at, updated_at
FROM kv_entries WHERE %s
ORDER BY namespace ASC, key ASC
LIMIT $%d OFFSET $%d`, where, argPos, argPos+1)
	args = append(args, limit, offset)
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("ListKV query: %w", err)
	}
	defer rows.Close()

	entries := make([]*models.KVEntry, 0)
	for rows.Next() {
		entry, err := scanKVEntryWithoutValue(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("ListKV scan: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("ListKV rows: %w", err)
	}
	return entries, total, nil
}

// ListKVNamespaces returns live usage grouped by namespace.
func (s *Store) ListKVNamespaces(ctx context.Context, tenantID string) ([]models.KVNamespaceSummary, error) {
	rows, err := s.pool.Query(ctx, `SELECT namespace, COUNT(*), COALESCE(SUM(octet_length(value::text)), 0)
FROM kv_entries
WHERE tenant_id = $1 AND (expires_at IS NULL OR expires_at > now())
GROUP BY namespace
ORDER BY namespace ASC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("ListKVNamespaces query: %w", err)
	}
	defer rows.Close()

	result := make([]models.KVNamespaceSummary, 0)
	for rows.Next() {
		var summary models.KVNamespaceSummary
		if err := rows.Scan(&summary.Namespace, &summary.Keys, &summary.SizeBytes); err != nil {
			return nil, fmt.Errorf("ListKVNamespaces scan: %w", err)
		}
		result = append(result, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ListKVNamespaces rows: %w", err)
	}
	return result, nil
}

// CountKV returns live usage and normalized limits for a tenant.
func (s *Store) CountKV(ctx context.Context, tenantID string) (models.KVUsage, models.KVLimits, error) {
	limits, err := s.KVLimitsForTenant(ctx, tenantID)
	if err != nil {
		return models.KVUsage{}, models.KVLimits{}, err
	}
	var usage models.KVUsage
	err = s.pool.QueryRow(ctx, `SELECT COUNT(*), COALESCE(SUM(octet_length(value::text)), 0)
FROM kv_entries
WHERE tenant_id = $1 AND (expires_at IS NULL OR expires_at > now())`, tenantID).Scan(&usage.Keys, &usage.SizeBytes)
	if err != nil {
		return models.KVUsage{}, models.KVLimits{}, fmt.Errorf("CountKV: %w", err)
	}
	return usage, limits, nil
}

// KVLimitsForTenant returns normalized limits without scanning KV entries.
func (s *Store) KVLimitsForTenant(ctx context.Context, tenantID string) (models.KVLimits, error) {
	var raw []byte
	if err := s.pool.QueryRow(ctx, `SELECT kv_limits FROM tenants WHERE id = $1`, tenantID).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.KVLimits{}, ErrKVTenantNotFound
		}
		return models.KVLimits{}, fmt.Errorf("KVLimitsForTenant: %w", err)
	}
	limits, err := decodeKVLimits(raw)
	if err != nil {
		return models.KVLimits{}, fmt.Errorf("KVLimitsForTenant decode: %w", err)
	}
	return limits, nil
}

// DeleteExpiredKV removes expired entries in independently lockable batches.
func (s *Store) DeleteExpiredKV(ctx context.Context, batchSize, maxBatches int) (int64, error) {
	if batchSize <= 0 {
		batchSize = kvListDefaultLimit
	}
	if maxBatches <= 0 {
		maxBatches = 1
	}

	var deleted int64
	for range maxBatches {
		result, err := s.pool.Exec(ctx, `DELETE FROM kv_entries
WHERE ctid IN (
    SELECT ctid FROM kv_entries
    WHERE expires_at IS NOT NULL AND expires_at <= now()
    LIMIT $1 FOR UPDATE SKIP LOCKED
)`, batchSize)
		if err != nil {
			return deleted, fmt.Errorf("DeleteExpiredKV: %w", err)
		}
		count := result.RowsAffected()
		deleted += count
		if count < int64(batchSize) {
			break
		}
	}
	return deleted, nil
}

func decodeKVLimits(raw []byte) (models.KVLimits, error) {
	var limits models.KVLimits
	if err := json.Unmarshal(raw, &limits); err != nil {
		return models.KVLimits{}, err
	}
	return limits.Normalize(), nil
}

func isInvalidJSONError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}

func scanKVEntry(s scannable) (*models.KVEntry, error) {
	var entry models.KVEntry
	var value []byte
	if err := s.Scan(&entry.ID, &entry.TenantID, &entry.Namespace, &entry.Key, &value, &entry.SizeBytes, &entry.ExpiresAt, &entry.CreatedAt, &entry.UpdatedAt); err != nil {
		return nil, err
	}
	entry.Value = json.RawMessage(value)
	return &entry, nil
}

func scanKVEntryWithoutValue(s scannable) (*models.KVEntry, error) {
	var entry models.KVEntry
	if err := s.Scan(&entry.ID, &entry.TenantID, &entry.Namespace, &entry.Key, &entry.SizeBytes, &entry.ExpiresAt, &entry.CreatedAt, &entry.UpdatedAt); err != nil {
		return nil, err
	}
	return &entry, nil
}
