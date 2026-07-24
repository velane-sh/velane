package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

func durationInterval(value time.Duration) string {
	return fmt.Sprintf("%f seconds", value.Seconds())
}

// MaintainObjectStorage retries durable outbox uploads and applies configured
// retention and delayed workflow-deletion policies. Every operation is
// idempotent so interrupted runs are safe to repeat.
func (s *Store) MaintainObjectStorage(ctx context.Context) error {
	if s.objects == nil {
		return nil
	}
	if err := s.backfillWorkflowVersions(ctx, 50); err != nil {
		return err
	}
	if err := s.backfillInvocationPayloads(ctx, 50); err != nil {
		return err
	}
	if err := s.retryPayloadOutbox(ctx, 100); err != nil {
		return err
	}
	if s.invocationRetention > 0 {
		if err := s.purgeExpiredInvocationPayloads(ctx, 100); err != nil {
			return err
		}
	}
	return s.collectDeletedWorkflowObjects(ctx, 20)
}

func (s *Store) backfillWorkflowVersions(ctx context.Context, limit int) error {
	rows, err := s.pool.Query(ctx,
		`SELECT sv.id, sv.snippet_id, sv.version_number, sv.code, sv.input_schema, sv.output_schema, s.tenant_id
		 FROM snippet_versions sv
		 JOIN snippets s ON s.id = sv.snippet_id
		 WHERE sv.content_state = 'legacy'
		 ORDER BY sv.created_at LIMIT $1`,
		limit,
	)
	if err != nil {
		return fmt.Errorf("query legacy workflow versions: %w", err)
	}
	defer rows.Close()
	type legacyVersion struct {
		id, workflowID, code, inputSchema, outputSchema, tenantID string
		number                                                    int
	}
	var items []legacyVersion
	for rows.Next() {
		var item legacyVersion
		if err := rows.Scan(&item.id, &item.workflowID, &item.number, &item.code, &item.inputSchema, &item.outputSchema, &item.tenantID); err != nil {
			return err
		}
		items = append(items, item)
	}
	for _, item := range items {
		ref := fmt.Sprintf("tenants/%s/workflows/%s/versions/%s.json", item.tenantID, item.workflowID, item.id)
		body, err := json.Marshal(workflowVersionObject{
			FormatVersion: 1, WorkflowID: item.workflowID, VersionID: item.id, VersionNumber: item.number,
			Code: item.code, InputSchema: item.inputSchema, OutputSchema: item.outputSchema,
		})
		if err != nil {
			return err
		}
		if err := s.objects.Put(ctx, ref, "application/json", "", body); err != nil {
			continue
		}
		sum := sha256.Sum256(body)
		_, err = s.pool.Exec(ctx,
			`UPDATE snippet_versions
			 SET code = '', input_schema = '{}', output_schema = '{}',
			     object_ref = $2, object_checksum = $3, object_size = $4, content_state = 'ready'
			 WHERE id = $1 AND content_state = 'legacy'`,
			item.id, ref, hex.EncodeToString(sum[:]), len(body),
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) backfillInvocationPayloads(ctx context.Context, limit int) error {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, snippet_id, version_id, input_payload,
		        COALESCE(output, ''), COALESCE(error, ''), COALESCE(stderr, ''), created_at
		 FROM invocations
		 WHERE payload_state = 'legacy'
		 ORDER BY created_at LIMIT $1`,
		limit,
	)
	if err != nil {
		return fmt.Errorf("query legacy invocation payloads: %w", err)
	}
	defer rows.Close()
	type legacyInvocation struct {
		id, tenantID, workflowID, versionID, input, output, errText, stderr string
		createdAt                                                           time.Time
	}
	var items []legacyInvocation
	for rows.Next() {
		var item legacyInvocation
		if err := rows.Scan(&item.id, &item.tenantID, &item.workflowID, &item.versionID, &item.input,
			&item.output, &item.errText, &item.stderr, &item.createdAt); err != nil {
			return err
		}
		items = append(items, item)
	}
	for _, item := range items {
		ref := invocationObjectKey(item.tenantID, item.workflowID, item.id, item.createdAt)
		body, err := encodeInvocationPayload(invocationPayloadObject{
			FormatVersion: 1, InvocationID: item.id, TenantID: item.tenantID,
			WorkflowID: item.workflowID, VersionID: item.versionID,
			Input: item.input, Output: item.output, Error: item.errText, Stderr: item.stderr,
		})
		if err != nil {
			return err
		}
		if err := s.objects.Put(ctx, ref, "application/json", "gzip", body); err != nil {
			continue
		}
		sum := sha256.Sum256(body)
		_, err = s.pool.Exec(ctx,
			`UPDATE invocations
			 SET input_payload = '', output = NULL, error = NULL, stderr = NULL,
			     payload_ref = $2, payload_checksum = $3, payload_size = $4, payload_state = 'stored'
			 WHERE id = $1 AND payload_state = 'legacy'`,
			item.id, ref, hex.EncodeToString(sum[:]), len(body),
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) retryPayloadOutbox(ctx context.Context, limit int) error {
	rows, err := s.pool.Query(ctx,
		`SELECT id, payload_ref, payload_outbox
		 FROM invocations
		 WHERE payload_outbox IS NOT NULL
		   AND (payload_retry_at IS NULL OR payload_retry_at <= now())
		 ORDER BY payload_retry_at NULLS FIRST
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return fmt.Errorf("query payload outbox: %w", err)
	}
	defer rows.Close()
	type pending struct {
		id, ref string
		body    []byte
	}
	var items []pending
	for rows.Next() {
		var item pending
		if err := rows.Scan(&item.id, &item.ref, &item.body); err != nil {
			return err
		}
		items = append(items, item)
	}
	for _, item := range items {
		if err := s.objects.Put(ctx, item.ref, "application/json", "gzip", item.body); err != nil {
			_, _ = s.pool.Exec(ctx,
				`UPDATE invocations
				 SET payload_attempts = payload_attempts + 1,
				     payload_retry_at = now() + (LEAST(payload_attempts + 1, 10) * interval '1 minute')
				 WHERE id = $1`,
				item.id,
			)
			continue
		}
		_, err := s.pool.Exec(ctx,
			`UPDATE invocations
			 SET payload_state = 'stored', payload_outbox = NULL, payload_retry_at = NULL
			 WHERE id = $1`,
			item.id,
		)
		if err != nil {
			return fmt.Errorf("complete payload outbox: %w", err)
		}
	}
	return nil
}

func (s *Store) purgeExpiredInvocationPayloads(ctx context.Context, limit int) error {
	rows, err := s.pool.Query(ctx,
		`SELECT id, payload_ref
		 FROM invocations
		 WHERE completed_at < now() - $1::interval
		   AND payload_state IN ('stored', 'failed')
		 ORDER BY completed_at
		 LIMIT $2`,
		durationInterval(s.invocationRetention), limit,
	)
	if err != nil {
		return fmt.Errorf("query expired invocation payloads: %w", err)
	}
	defer rows.Close()
	var items [][2]string
	for rows.Next() {
		var id, ref string
		if err := rows.Scan(&id, &ref); err != nil {
			return err
		}
		items = append(items, [2]string{id, ref})
	}
	for _, item := range items {
		if item[1] != "" {
			if err := s.objects.Delete(ctx, item[1]); err != nil {
				continue
			}
		}
		_, err := s.pool.Exec(ctx,
			`UPDATE invocations
			 SET payload_state = 'purged', payload_outbox = NULL, payload_retry_at = NULL
			 WHERE id = $1`,
			item[0],
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) collectDeletedWorkflowObjects(ctx context.Context, limit int) error {
	rows, err := s.pool.Query(ctx,
		`SELECT id FROM snippets
		 WHERE objects_delete_after <= now() AND objects_deleted_at IS NULL
		 ORDER BY objects_delete_after LIMIT $1`,
		limit,
	)
	if err != nil {
		return fmt.Errorf("query deleted workflows: %w", err)
	}
	defer rows.Close()
	var workflowIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		workflowIDs = append(workflowIDs, id)
	}
	for _, workflowID := range workflowIDs {
		refs, err := s.workflowObjectReferences(ctx, workflowID)
		if err != nil {
			return err
		}
		allDeleted := true
		for _, ref := range refs {
			if ref != "" {
				if err := s.objects.Delete(ctx, ref); err != nil {
					allDeleted = false
				}
			}
		}
		if allDeleted {
			_, err := s.pool.Exec(ctx,
				`UPDATE snippets SET objects_deleted_at = now() WHERE id = $1`,
				workflowID,
			)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) workflowObjectReferences(ctx context.Context, workflowID string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT object_ref FROM snippet_versions WHERE snippet_id = $1 AND object_ref IS NOT NULL
		 UNION ALL
		 SELECT payload_ref FROM invocations WHERE snippet_id = $1 AND payload_ref IS NOT NULL`,
		workflowID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var refs []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}
