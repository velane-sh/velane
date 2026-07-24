package postgres

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/ids"
	"github.com/abskrj/velane/services/control-plane/internal/models"
)

type invocationPayloadObject struct {
	FormatVersion int    `json:"format_version"`
	InvocationID  string `json:"invocation_id"`
	TenantID      string `json:"tenant_id"`
	WorkflowID    string `json:"workflow_id"`
	VersionID     string `json:"version_id"`
	Input         string `json:"input"`
	Output        string `json:"output,omitempty"`
	Error         string `json:"error,omitempty"`
	Stderr        string `json:"stderr,omitempty"`
}

// CreateInvocation inserts a new invocation record.
// The status defaults to 'running' for sync invocations; callers that want
// 'pending' (async) should call CreateInvocationWithMode.
func (s *Store) CreateInvocation(ctx context.Context, snippetID, versionID, environment, tenantID, inputPayload string) (*models.Invocation, error) {
	return s.CreateInvocationWithMode(ctx, snippetID, versionID, environment, tenantID, inputPayload, "sync", "", models.InvocationRunning)
}

// CreateInvocationWithMode inserts a new invocation record with explicit mode,
// callback URL, and initial status.
func (s *Store) CreateInvocationWithMode(
	ctx context.Context,
	snippetID, versionID, environment, tenantID, inputPayload, invokeMode, callbackURL string,
	status models.InvocationStatus,
) (*models.Invocation, error) {
	invocationID := ids.New()
	createdAt := time.Now().UTC()
	payloadRef := ""
	payloadState := "legacy"
	var outbox []byte
	storedInput := inputPayload
	if s.objects != nil {
		payloadRef = invocationObjectKey(tenantID, snippetID, invocationID, createdAt)
		document := invocationPayloadObject{
			FormatVersion: 1, InvocationID: invocationID, TenantID: tenantID,
			WorkflowID: snippetID, VersionID: versionID, Input: inputPayload,
		}
		encoded, err := encodeInvocationPayload(document)
		if err != nil {
			return nil, err
		}
		storedInput = ""
		if err := s.objects.Put(ctx, payloadRef, "application/json", "gzip", encoded); err != nil {
			payloadState, outbox = "failed", encoded
		} else {
			payloadState = "stored"
		}
	}
	row := s.pool.QueryRow(ctx,
		`INSERT INTO invocations
		   (id, snippet_id, version_id, environment, tenant_id, status, input_payload, invoke_mode, callback_url,
		    payload_ref, payload_state, payload_outbox, payload_retry_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, ''), $11, $12,
		         CASE WHEN $12::bytea IS NULL THEN NULL ELSE now() END, $13)
		 RETURNING id, snippet_id, version_id, environment, tenant_id, status,
		           input_payload, input_ref, output, output_ref, error, stderr, stderr_ref, duration_ms, peak_memory_mb, cpu_ms,
		           created_at, completed_at, callback_url, invoke_mode`,
		invocationID, snippetID, versionID, environment, tenantID, string(status), storedInput, invokeMode,
		nullableString(callbackURL), payloadRef, payloadState, outbox, createdAt,
	)
	inv, err := scanInvocation(row)
	if err != nil {
		return nil, fmt.Errorf("CreateInvocationWithMode scan: %w", err)
	}
	inv.PayloadState = payloadState
	inv.InputPayload = inputPayload
	return inv, nil
}

// UpdateInvocationResult updates an invocation with the execution result.
func (s *Store) UpdateInvocationResult(ctx context.Context, id string, status models.InvocationStatus, output, errMsg, stderr string, durationMs, peakMemoryMB, cpuMs int) error {
	if s.objects != nil {
		invocation, err := s.GetInvocation(ctx, id)
		if err != nil {
			return fmt.Errorf("load invocation payload: %w", err)
		}
		document := invocationPayloadObject{
			FormatVersion: 1, InvocationID: invocation.ID, TenantID: invocation.TenantID,
			WorkflowID: invocation.SnippetID, VersionID: invocation.VersionID,
			Input: invocation.InputPayload, Output: output, Error: errMsg, Stderr: stderr,
		}
		encoded, err := encodeInvocationPayload(document)
		if err != nil {
			return err
		}
		payloadRef := invocationObjectKey(invocation.TenantID, invocation.SnippetID, invocation.ID, invocation.CreatedAt)
		payloadState := "stored"
		var outbox []byte
		if err := s.objects.Put(ctx, payloadRef, "application/json", "gzip", encoded); err != nil {
			payloadState, outbox = "failed", encoded
		}
		sum := sha256.Sum256(encoded)
		_, err = s.pool.Exec(ctx,
			`UPDATE invocations
			 SET status = $2, output = NULL, error = NULL, stderr = NULL,
			     duration_ms = $3, peak_memory_mb = $4, cpu_ms = $5, completed_at = now(),
			     payload_ref = $6, payload_state = $7, payload_outbox = $8,
			     payload_checksum = $9, payload_size = $10,
			     payload_retry_at = CASE WHEN $8::bytea IS NULL THEN NULL ELSE now() END
			 WHERE id = $1`,
			id, string(status), durationMs, peakMemoryMB, cpuMs, payloadRef, payloadState, outbox,
			hex.EncodeToString(sum[:]), len(encoded),
		)
		if err != nil {
			return fmt.Errorf("UpdateInvocationResult object-backed: %w", err)
		}
		return nil
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE invocations
		 SET status       = $2,
		     output       = $3,
		     error        = $4,
		     stderr       = $5,
		     duration_ms  = $6,
		     peak_memory_mb = $7,
		     cpu_ms       = $8,
		     completed_at = now()
		 WHERE id = $1`,
		id, string(status), output, errMsg, stderr, durationMs, peakMemoryMB, cpuMs,
	)
	if err != nil {
		return fmt.Errorf("UpdateInvocationResult: %w", err)
	}
	return nil
}

// FailStaleInvocations marks invocations stuck in pending/running past olderThan
// as timed out. This reaps records whose worker never picked them up or crashed
// mid-execution before finalizing. Returns the number of rows updated.
func (s *Store) FailStaleInvocations(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	if s.objects != nil {
		rows, err := s.pool.Query(ctx,
			`SELECT id FROM invocations
			 WHERE status IN ('pending', 'running') AND created_at < $1`,
			cutoff,
		)
		if err != nil {
			return 0, fmt.Errorf("query stale invocations: %w", err)
		}
		defer rows.Close()
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return 0, err
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			return 0, err
		}
		rows.Close()
		for _, id := range ids {
			if err := s.UpdateInvocationResult(ctx, id, models.InvocationTimeout, "", "execution did not complete", "", 0, 0, 0); err != nil {
				return int64(len(ids)), err
			}
		}
		return int64(len(ids)), nil
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE invocations
		 SET status       = $1,
		     error        = COALESCE(NULLIF(error, ''), 'execution did not complete'),
		     completed_at = now()
		 WHERE status IN ('pending', 'running')
		   AND created_at < $2`,
		string(models.InvocationTimeout), cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("FailStaleInvocations: %w", err)
	}
	return tag.RowsAffected(), nil
}

// InvocationLogFilters filters invocation log query results.
type InvocationLogFilters struct {
	Environment string
	Status      string
	StartTime   *time.Time
	EndTime     *time.Time
	Limit       int
}

// SnippetMetrics contains aggregate and timeseries metrics for a snippet.
type SnippetMetrics struct {
	Window        string
	TotalCount    int64
	Completed     int64
	Failed        int64
	P50DurationMs float64
	P95DurationMs float64
	P99DurationMs float64
	AvgDurationMs float64
	Series        []SnippetMetricsPoint
}

// SnippetMetricsPoint is one timeseries bucket.
type SnippetMetricsPoint struct {
	BucketStart   time.Time `json:"bucket_start"`
	Count         int64     `json:"count"`
	P95DurationMs float64   `json:"p95_duration_ms"`
}

// ListInvocationLogs returns filtered invocations for logs endpoint.
func (s *Store) ListInvocationLogs(ctx context.Context, snippetID string, filters InvocationLogFilters) ([]*models.Invocation, error) {
	limit := filters.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	clauses := []string{"snippet_id = $1"}
	args := []any{snippetID}
	argPos := 2

	if filters.Environment != "" {
		clauses = append(clauses, fmt.Sprintf("environment = $%d", argPos))
		args = append(args, filters.Environment)
		argPos++
	}
	if filters.Status != "" {
		clauses = append(clauses, fmt.Sprintf("status = $%d", argPos))
		args = append(args, filters.Status)
		argPos++
	}
	if filters.StartTime != nil {
		clauses = append(clauses, fmt.Sprintf("created_at >= $%d", argPos))
		args = append(args, *filters.StartTime)
		argPos++
	}
	if filters.EndTime != nil {
		clauses = append(clauses, fmt.Sprintf("created_at <= $%d", argPos))
		args = append(args, *filters.EndTime)
		argPos++
	}

	query := fmt.Sprintf(
		`SELECT id, snippet_id, version_id, environment, tenant_id, status,
		        input_payload, input_ref, output, output_ref, error, stderr, stderr_ref, duration_ms, peak_memory_mb, cpu_ms,
		        created_at, completed_at, callback_url, invoke_mode, payload_state
		 FROM invocations WHERE %s
		 ORDER BY created_at DESC
		 LIMIT $%d`,
		strings.Join(clauses, " AND "),
		argPos,
	)
	args = append(args, limit)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("ListInvocationLogs query: %w", err)
	}
	defer rows.Close()

	var invocations []*models.Invocation
	for rows.Next() {
		inv, scanErr := scanInvocationWithPayloadState(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		invocations = append(invocations, inv)
	}

	return invocations, rows.Err()
}

// GetSnippetMetrics computes aggregate and per-hour metrics in the requested window.
func (s *Store) GetSnippetMetrics(ctx context.Context, snippetID, window string, since time.Time) (*SnippetMetrics, error) {
	m := &SnippetMetrics{Window: window}

	aggRow := s.pool.QueryRow(ctx,
		`SELECT
		    COUNT(*)::bigint AS total_count,
		    COUNT(*) FILTER (WHERE status = 'completed')::bigint AS completed_count,
		    COUNT(*) FILTER (WHERE status IN ('failed', 'timeout', 'oom_killed'))::bigint AS failed_count,
		    COALESCE(AVG(duration_ms), 0)::float8 AS avg_duration_ms,
		    COALESCE(PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY duration_ms), 0)::float8 AS p50_duration_ms,
		    COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY duration_ms), 0)::float8 AS p95_duration_ms,
		    COALESCE(PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY duration_ms), 0)::float8 AS p99_duration_ms
		 FROM invocations
		 WHERE snippet_id = $1
		   AND created_at >= $2
		   AND duration_ms IS NOT NULL`,
		snippetID, since,
	)

	if err := aggRow.Scan(
		&m.TotalCount,
		&m.Completed,
		&m.Failed,
		&m.AvgDurationMs,
		&m.P50DurationMs,
		&m.P95DurationMs,
		&m.P99DurationMs,
	); err != nil {
		return nil, fmt.Errorf("GetSnippetMetrics aggregate scan: %w", err)
	}

	rows, err := s.pool.Query(ctx,
		`SELECT
		    date_trunc('hour', created_at) AS bucket_start,
		    COUNT(*)::bigint AS count,
		    COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY duration_ms), 0)::float8 AS p95_duration_ms
		 FROM invocations
		 WHERE snippet_id = $1
		   AND created_at >= $2
		   AND duration_ms IS NOT NULL
		 GROUP BY date_trunc('hour', created_at)
		 ORDER BY bucket_start ASC`,
		snippetID, since,
	)
	if err != nil {
		return nil, fmt.Errorf("GetSnippetMetrics series query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var point SnippetMetricsPoint
		if scanErr := rows.Scan(&point.BucketStart, &point.Count, &point.P95DurationMs); scanErr != nil {
			return nil, fmt.Errorf("GetSnippetMetrics series scan: %w", scanErr)
		}
		m.Series = append(m.Series, point)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetSnippetMetrics series rows: %w", err)
	}

	return m, nil
}

// GetInvocation retrieves a single invocation by its primary key.
func (s *Store) GetInvocation(ctx context.Context, id string) (*models.Invocation, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, snippet_id, version_id, environment, tenant_id, status,
		        input_payload, input_ref, output, output_ref, error, stderr, stderr_ref, duration_ms, peak_memory_mb, cpu_ms,
		        created_at, completed_at, callback_url, invoke_mode, payload_state
		 FROM invocations WHERE id = $1`,
		id,
	)
	inv, err := scanInvocation(row)
	if err != nil {
		return nil, fmt.Errorf("GetInvocation: %w", err)
	}
	return s.hydrateInvocation(ctx, inv)
}

func invocationObjectKey(tenantID, workflowID, invocationID string, createdAt time.Time) string {
	return fmt.Sprintf("tenants/%s/invocations/%s/%04d/%02d/%02d/%s.json.gz",
		tenantID, workflowID, createdAt.Year(), createdAt.Month(), createdAt.Day(), invocationID)
}

func encodeInvocationPayload(document invocationPayloadObject) ([]byte, error) {
	raw, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("marshal invocation payload: %w", err)
	}
	var buf bytes.Buffer
	writer := gzip.NewWriter(&buf)
	if _, err := writer.Write(raw); err != nil {
		return nil, fmt.Errorf("compress invocation payload: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("finish invocation payload compression: %w", err)
	}
	return buf.Bytes(), nil
}

func decodeInvocationPayload(encoded []byte) (*invocationPayloadObject, error) {
	reader, err := gzip.NewReader(bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("open invocation payload: %w", err)
	}
	defer reader.Close()
	var document invocationPayloadObject
	if err := json.NewDecoder(reader).Decode(&document); err != nil {
		return nil, fmt.Errorf("decode invocation payload: %w", err)
	}
	return &document, nil
}

func (s *Store) hydrateInvocation(ctx context.Context, invocation *models.Invocation) (*models.Invocation, error) {
	if s.objects == nil || invocation == nil {
		return invocation, nil
	}
	var payloadRef, payloadState, checksum *string
	var outbox []byte
	if err := s.pool.QueryRow(ctx,
		`SELECT payload_ref, payload_state, payload_outbox, payload_checksum FROM invocations WHERE id = $1`,
		invocation.ID,
	).Scan(&payloadRef, &payloadState, &outbox, &checksum); err != nil {
		return nil, fmt.Errorf("get invocation payload reference: %w", err)
	}
	if payloadState != nil {
		invocation.PayloadState = *payloadState
	}
	if invocation.PayloadState == "purged" {
		return invocation, nil
	}
	var encoded []byte
	var err error
	if len(outbox) > 0 {
		encoded = outbox
	} else if payloadRef != nil && *payloadRef != "" {
		encoded, err = s.objects.Get(ctx, *payloadRef)
		if err != nil {
			return nil, fmt.Errorf("load invocation payload object: %w", err)
		}
	} else {
		return invocation, nil
	}
	if checksum != nil && *checksum != "" {
		sum := sha256.Sum256(encoded)
		if hex.EncodeToString(sum[:]) != *checksum {
			return nil, fmt.Errorf("invocation payload object checksum mismatch")
		}
	}
	document, err := decodeInvocationPayload(encoded)
	if err != nil {
		return nil, err
	}
	if document.InvocationID != invocation.ID || document.TenantID != invocation.TenantID {
		return nil, fmt.Errorf("invocation payload object identity mismatch")
	}
	invocation.InputPayload = document.Input
	invocation.Output = document.Output
	invocation.Error = document.Error
	invocation.Stderr = document.Stderr
	return invocation, nil
}

// ListInvocationsBySnippet returns recent invocations for a snippet.
func (s *Store) ListInvocationsBySnippet(ctx context.Context, snippetID string, limit int) ([]*models.Invocation, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, snippet_id, version_id, environment, tenant_id, status,
		        input_payload, input_ref, output, output_ref, error, stderr, stderr_ref, duration_ms, peak_memory_mb, cpu_ms,
		        created_at, completed_at, callback_url, invoke_mode
		 FROM invocations WHERE snippet_id = $1
		 ORDER BY created_at DESC LIMIT $2`,
		snippetID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("ListInvocationsBySnippet query: %w", err)
	}
	defer rows.Close()

	var invocations []*models.Invocation
	for rows.Next() {
		inv, err := scanInvocationWithPayloadState(rows)
		if err != nil {
			return nil, err
		}
		invocations = append(invocations, inv)
	}
	return invocations, rows.Err()
}

func scanInvocation(s scannable) (*models.Invocation, error) {
	var inv models.Invocation
	var inputRef, output, outputRef, errMsg, stderr, stderrRef, callbackURL *string
	var durationMs, peakMemoryMB, cpuMs *int
	if err := s.Scan(
		&inv.ID, &inv.SnippetID, &inv.VersionID, &inv.Environment, &inv.TenantID,
		&inv.Status, &inv.InputPayload,
		&inputRef, &output, &outputRef, &errMsg, &stderr, &stderrRef,
		&durationMs, &peakMemoryMB, &cpuMs,
		&inv.CreatedAt, &inv.CompletedAt,
		&callbackURL, &inv.InvokeMode,
	); err != nil {
		return nil, err
	}
	if inputRef != nil {
		inv.InputRef = *inputRef
	}
	if output != nil {
		inv.Output = *output
	}
	if outputRef != nil {
		inv.OutputRef = *outputRef
	}
	if errMsg != nil {
		inv.Error = *errMsg
	}
	if stderr != nil {
		inv.Stderr = *stderr
	}
	if stderrRef != nil {
		inv.StderrRef = *stderrRef
	}
	if durationMs != nil {
		inv.DurationMs = *durationMs
	}
	if peakMemoryMB != nil {
		inv.PeakMemoryMB = *peakMemoryMB
	}
	if cpuMs != nil {
		inv.CPUMs = *cpuMs
	}
	if callbackURL != nil {
		inv.CallbackURL = *callbackURL
	}
	return &inv, nil
}

func scanInvocationWithPayloadState(s scannable) (*models.Invocation, error) {
	var inv models.Invocation
	var inputRef, output, outputRef, errMsg, stderr, stderrRef, callbackURL *string
	var durationMs, peakMemoryMB, cpuMs *int
	if err := s.Scan(
		&inv.ID, &inv.SnippetID, &inv.VersionID, &inv.Environment, &inv.TenantID,
		&inv.Status, &inv.InputPayload,
		&inputRef, &output, &outputRef, &errMsg, &stderr, &stderrRef,
		&durationMs, &peakMemoryMB, &cpuMs,
		&inv.CreatedAt, &inv.CompletedAt, &callbackURL, &inv.InvokeMode, &inv.PayloadState,
	); err != nil {
		return nil, err
	}
	if durationMs != nil {
		inv.DurationMs = *durationMs
	}
	if peakMemoryMB != nil {
		inv.PeakMemoryMB = *peakMemoryMB
	}
	if cpuMs != nil {
		inv.CPUMs = *cpuMs
	}
	if callbackURL != nil {
		inv.CallbackURL = *callbackURL
	}
	// List endpoints deliberately omit payload and log content.
	return &inv, nil
}

// nullableString converts an empty string to nil (SQL NULL).
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
