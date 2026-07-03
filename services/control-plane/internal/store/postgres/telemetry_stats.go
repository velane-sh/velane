package postgres

import (
	"context"
	"time"
)

// TelemetryStats holds aggregate counts for the telemetry heartbeat.
type TelemetryStats struct {
	SnippetCount      int64
	MemberCount       int64
	InvocationCount24h int64
	ConnectionCount   int64
	EmbedTokenCount   int64
	BunSnippetCount   int64
	PythonSnippetCount int64
}

// GetTelemetryStats returns aggregate counts across all tenants.
func (s *Store) GetTelemetryStats(ctx context.Context) (*TelemetryStats, error) {
	since := time.Now().UTC().Add(-24 * time.Hour)

	row := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM snippets WHERE deleted_at IS NULL)::bigint,
			(SELECT COUNT(*) FROM tenant_members)::bigint,
			(SELECT COUNT(*) FROM invocations WHERE created_at >= $1)::bigint,
			(SELECT COUNT(*) FROM connections)::bigint,
			(SELECT COUNT(*) FROM embed_tokens WHERE revoked_at IS NULL)::bigint,
			(SELECT COUNT(*) FROM snippets WHERE language = 'bun'  AND deleted_at IS NULL)::bigint,
			(SELECT COUNT(*) FROM snippets WHERE language = 'python' AND deleted_at IS NULL)::bigint
	`, since)

	var st TelemetryStats
	err := row.Scan(
		&st.SnippetCount,
		&st.MemberCount,
		&st.InvocationCount24h,
		&st.ConnectionCount,
		&st.EmbedTokenCount,
		&st.BunSnippetCount,
		&st.PythonSnippetCount,
	)
	return &st, err
}
