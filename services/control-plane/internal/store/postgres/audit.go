package postgres

import (
	"context"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/ids"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/jackc/pgx/v5"
)

// AuditQueryOpts provides optional filters and pagination for audit log queries.
type AuditQueryOpts struct {
	Action     string
	ActorID    string
	ResourceID string
	Limit      int
	Before     time.Time // cursor for pagination; zero value means no upper bound
}

// AppendAuditLog inserts a new audit log entry. The table is append-only (no deletes, no updates).
func (s *Store) AppendAuditLog(ctx context.Context, entry models.AuditEntry) error {
	metadata := entry.Metadata
	if metadata == nil {
		metadata = []byte("{}")
	}

	_, err := s.pool.Exec(ctx,
		`INSERT INTO audit_log (id, tenant_id, actor_id, actor_type, action, resource_id, metadata)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		ids.New(),
		entry.TenantID,
		entry.ActorID,
		entry.ActorType,
		entry.Action,
		entry.ResourceID,
		metadata,
	)
	return err
}

// ListAuditLog returns audit log entries for a tenant, newest first, with optional filters.
func (s *Store) ListAuditLog(ctx context.Context, tenantID string, opts AuditQueryOpts) ([]*models.AuditEntry, error) {
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	query := `SELECT id, tenant_id, actor_id, actor_type, action, resource_id, metadata, created_at
	           FROM audit_log
	           WHERE tenant_id = $1`
	args := []any{tenantID}
	idx := 2

	if !opts.Before.IsZero() {
		query += " AND created_at < $" + itoa(idx)
		args = append(args, opts.Before)
		idx++
	}
	if opts.Action != "" {
		query += " AND action = $" + itoa(idx)
		args = append(args, opts.Action)
		idx++
	}
	if opts.ActorID != "" {
		query += " AND actor_id = $" + itoa(idx)
		args = append(args, opts.ActorID)
		idx++
	}
	if opts.ResourceID != "" {
		query += " AND resource_id = $" + itoa(idx)
		args = append(args, opts.ResourceID)
		idx++
	}

	query += " ORDER BY created_at DESC LIMIT $" + itoa(idx)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*models.AuditEntry
	for rows.Next() {
		var e models.AuditEntry
		if err := rows.Scan(&e.ID, &e.TenantID, &e.ActorID, &e.ActorType, &e.Action, &e.ResourceID, &e.Metadata, &e.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, &e)
	}
	return entries, rows.Err()
}

// itoa converts an int to a string without importing strconv at package level.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	buf := make([]byte, 0, 3)
	for i > 0 {
		buf = append([]byte{byte('0' + i%10)}, buf...)
		i /= 10
	}
	return string(buf)
}

// AppendAuditLogTx inserts an audit record in an existing transaction so that
// user intent, operation, event, and audit are committed atomically.
func AppendAuditLogTx(ctx context.Context, tx pgx.Tx, entry models.AuditEntry) error {
	metadata := entry.Metadata
	if metadata == nil {
		metadata = []byte("{}")
	}
	_, err := tx.Exec(ctx, `INSERT INTO audit_log (id, tenant_id, actor_id, actor_type, action, resource_id, metadata) VALUES ($1,$2,$3,$4,$5,$6,$7)`, ids.New(), entry.TenantID, entry.ActorID, entry.ActorType, entry.Action, entry.ResourceID, metadata)
	return err
}
