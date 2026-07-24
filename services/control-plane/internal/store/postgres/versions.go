package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/abskrj/velane/services/control-plane/internal/ids"
	"github.com/abskrj/velane/services/control-plane/internal/models"
)

type workflowVersionObject struct {
	FormatVersion int    `json:"format_version"`
	WorkflowID    string `json:"workflow_id"`
	VersionID     string `json:"version_id"`
	VersionNumber int    `json:"version_number"`
	Code          string `json:"code"`
	InputSchema   string `json:"input_schema"`
	OutputSchema  string `json:"output_schema"`
}

// CreateVersion inserts a new snippet version. The version number is
// auto-assigned as max(version_number)+1 for the snippet.
func (s *Store) CreateVersion(ctx context.Context, snippetID, code, inputSchema, outputSchema, createdBy string, timeoutMs, maxMemoryMB, maxCPUPercent int) (*models.SnippetVersion, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Determine next version number atomically.
	var nextNum int
	err = tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(version_number), 0) + 1 FROM snippet_versions WHERE snippet_id = $1`,
		snippetID,
	).Scan(&nextNum)
	if err != nil {
		return nil, fmt.Errorf("compute version number: %w", err)
	}

	versionID := ids.New()
	objectRef := ""
	contentState := "legacy"
	if s.objects != nil {
		var tenantID string
		if err := tx.QueryRow(ctx, `SELECT tenant_id FROM snippets WHERE id = $1`, snippetID).Scan(&tenantID); err != nil {
			return nil, fmt.Errorf("get workflow tenant: %w", err)
		}
		objectRef = fmt.Sprintf("tenants/%s/workflows/%s/versions/%s.json", tenantID, snippetID, versionID)
		document, marshalErr := json.Marshal(workflowVersionObject{
			FormatVersion: 1, WorkflowID: snippetID, VersionID: versionID, VersionNumber: nextNum,
			Code: code, InputSchema: inputSchema, OutputSchema: outputSchema,
		})
		if marshalErr != nil {
			return nil, fmt.Errorf("marshal workflow version: %w", marshalErr)
		}
		if putErr := s.objects.Put(ctx, objectRef, "application/json", "", document); putErr != nil {
			return nil, fmt.Errorf("store workflow version: %w", putErr)
		}
		sum := sha256.Sum256(document)
		_, err = tx.Exec(ctx,
			`INSERT INTO snippet_versions
			   (id, snippet_id, version_number, code, input_schema, output_schema, timeout_ms, max_memory_mb, max_cpu_percent,
			    created_by, object_ref, object_checksum, object_size, content_state)
			 VALUES ($1, $2, $3, '', '{}', '{}', $4, $5, $6, $7, $8, $9, $10, 'ready')`,
			versionID, snippetID, nextNum, timeoutMs, maxMemoryMB, maxCPUPercent, createdBy,
			objectRef, hex.EncodeToString(sum[:]), len(document),
		)
		if err != nil {
			return nil, fmt.Errorf("insert object-backed version: %w", err)
		}
		contentState = "ready"
	}

	var row scannable
	if contentState == "legacy" {
		row = tx.QueryRow(ctx,
			`INSERT INTO snippet_versions
			   (id, snippet_id, version_number, code, input_schema, output_schema, timeout_ms, max_memory_mb, max_cpu_percent, created_by)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			 RETURNING id, snippet_id, version_number, code, input_schema, output_schema,
			           timeout_ms, max_memory_mb, max_cpu_percent, status, created_at, created_by`,
			versionID, snippetID, nextNum, code, inputSchema, outputSchema, timeoutMs, maxMemoryMB, maxCPUPercent, createdBy,
		)
	} else {
		row = tx.QueryRow(ctx,
			`SELECT id, snippet_id, version_number, code, input_schema, output_schema,
			        timeout_ms, max_memory_mb, max_cpu_percent, status, created_at, created_by
			 FROM snippet_versions WHERE id = $1`,
			versionID,
		)
	}

	v, err := scanVersion(row)
	if err != nil {
		return nil, fmt.Errorf("CreateVersion scan: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return s.hydrateVersion(ctx, v)
}

// GetVersion retrieves a version by its primary key.
func (s *Store) GetVersion(ctx context.Context, id string) (*models.SnippetVersion, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, snippet_id, version_number, code, input_schema, output_schema,
		        timeout_ms, max_memory_mb, max_cpu_percent, status, created_at, created_by
		 FROM snippet_versions WHERE id = $1`,
		id,
	)
	v, err := scanVersion(row)
	if err != nil {
		return nil, fmt.Errorf("GetVersion: %w", err)
	}
	return s.hydrateVersion(ctx, v)
}

// GetVersionByNumber retrieves a version by snippet ID + human-readable number.
func (s *Store) GetVersionByNumber(ctx context.Context, snippetID string, num int) (*models.SnippetVersion, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, snippet_id, version_number, code, input_schema, output_schema,
		        timeout_ms, max_memory_mb, max_cpu_percent, status, created_at, created_by
		 FROM snippet_versions WHERE snippet_id = $1 AND version_number = $2`,
		snippetID, num,
	)
	v, err := scanVersion(row)
	if err != nil {
		return nil, fmt.Errorf("GetVersionByNumber: %w", err)
	}
	return v, nil
}

// ListVersions returns all versions for a snippet ordered by version_number.
func (s *Store) ListVersions(ctx context.Context, snippetID string) ([]*models.SnippetVersion, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, snippet_id, version_number, code, input_schema, output_schema,
		        timeout_ms, max_memory_mb, max_cpu_percent, status, created_at, created_by
		 FROM snippet_versions WHERE snippet_id = $1
		 ORDER BY version_number ASC`,
		snippetID,
	)
	if err != nil {
		return nil, fmt.Errorf("ListVersions query: %w", err)
	}
	defer rows.Close()

	var versions []*models.SnippetVersion
	for rows.Next() {
		v, err := scanVersion(rows)
		if err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, version := range versions {
		if _, err := s.hydrateVersion(ctx, version); err != nil {
			return nil, err
		}
	}
	return versions, nil
}

// PublishVersion sets a version's status to "published" and updates the
// snippet_environments row for the given env to point at this version.
// Previously published versions for the same snippet are archived.
func (s *Store) PublishVersion(ctx context.Context, versionID, env string) (*models.SnippetVersion, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	// Fetch the version to get its snippet_id.
	row := tx.QueryRow(ctx,
		`SELECT id, snippet_id, version_number, code, input_schema, output_schema,
		        timeout_ms, max_memory_mb, max_cpu_percent, status, created_at, created_by
		 FROM snippet_versions WHERE id = $1`,
		versionID,
	)
	v, err := scanVersion(row)
	if err != nil {
		return nil, fmt.Errorf("PublishVersion fetch: %w", err)
	}

	// Archive any currently published version for this snippet (in any env).
	_, err = tx.Exec(ctx,
		`UPDATE snippet_versions SET status = 'archived'
		 WHERE snippet_id = $1 AND status = 'published' AND id != $2`,
		v.SnippetID, versionID,
	)
	if err != nil {
		return nil, fmt.Errorf("archive old versions: %w", err)
	}

	// Publish the target version.
	_, err = tx.Exec(ctx,
		`UPDATE snippet_versions SET status = 'published' WHERE id = $1`,
		versionID,
	)
	if err != nil {
		return nil, fmt.Errorf("publish version: %w", err)
	}

	// Update the environment pointer.
	_, err = tx.Exec(ctx,
		`INSERT INTO snippet_environments (snippet_id, env, active_version_id)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (snippet_id, env) DO UPDATE SET active_version_id = EXCLUDED.active_version_id`,
		v.SnippetID, env, versionID,
	)
	if err != nil {
		return nil, fmt.Errorf("update env pointer: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	v.Status = models.StatusPublished
	return s.hydrateVersion(ctx, v)
}

func (s *Store) hydrateVersion(ctx context.Context, version *models.SnippetVersion) (*models.SnippetVersion, error) {
	if s.objects == nil || version == nil {
		return version, nil
	}
	var objectRef, checksum *string
	if err := s.pool.QueryRow(ctx, `SELECT object_ref, object_checksum FROM snippet_versions WHERE id = $1`, version.ID).Scan(&objectRef, &checksum); err != nil {
		return nil, fmt.Errorf("get workflow object reference: %w", err)
	}
	if objectRef == nil || *objectRef == "" {
		return version, nil
	}
	body, err := s.objects.Get(ctx, *objectRef)
	if err != nil {
		return nil, fmt.Errorf("load workflow version object: %w", err)
	}
	if checksum != nil && *checksum != "" {
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != *checksum {
			return nil, fmt.Errorf("workflow version object checksum mismatch")
		}
	}
	var document workflowVersionObject
	if err := json.Unmarshal(body, &document); err != nil {
		return nil, fmt.Errorf("decode workflow version object: %w", err)
	}
	if document.WorkflowID != version.SnippetID || document.VersionID != version.ID {
		return nil, fmt.Errorf("workflow version object identity mismatch")
	}
	version.Code = document.Code
	version.InputSchema = document.InputSchema
	version.OutputSchema = document.OutputSchema
	return version, nil
}

// SetCanary sets the canary version and traffic percentage for an environment.
// pct=0 effectively disables canary. pct=100 sends all traffic to canary.
func (s *Store) SetCanary(ctx context.Context, snippetID, env, canaryVersionID string, pct int) (*models.SnippetEnvironment, error) {
	row := s.pool.QueryRow(ctx,
		`UPDATE snippet_environments
		 SET canary_version_id = $3, canary_pct = $4
		 WHERE snippet_id = $1 AND env = $2
		 RETURNING snippet_id, env, active_version_id, min_instances, canary_version_id, canary_pct`,
		snippetID, env, canaryVersionID, pct,
	)

	var se models.SnippetEnvironment
	if err := row.Scan(&se.SnippetID, &se.Env, &se.ActiveVersionID, &se.MinInstances, &se.CanaryVersionID, &se.CanaryPct); err != nil {
		return nil, fmt.Errorf("SetCanary scan: %w", err)
	}
	return &se, nil
}

// ClearCanary removes the canary config from an environment.
func (s *Store) ClearCanary(ctx context.Context, snippetID, env string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE snippet_environments
		 SET canary_version_id = NULL, canary_pct = 0
		 WHERE snippet_id = $1 AND env = $2`,
		snippetID, env,
	)
	if err != nil {
		return fmt.Errorf("ClearCanary: %w", err)
	}
	return nil
}

// GetSnippetEnvironments returns the active version number per environment for a snippet.
// Only environments that have had a version published are returned.
func (s *Store) GetSnippetEnvironments(ctx context.Context, snippetID string) ([]models.SnippetEnvironment, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT se.env, sv.version_number
		 FROM snippet_environments se
		 LEFT JOIN snippet_versions sv ON sv.id = se.active_version_id
		 WHERE se.snippet_id = $1
		 ORDER BY se.env`,
		snippetID,
	)
	if err != nil {
		return nil, fmt.Errorf("GetSnippetEnvironments query: %w", err)
	}
	defer rows.Close()

	var envs []models.SnippetEnvironment
	for rows.Next() {
		var env models.SnippetEnvironment
		var versionNumber *int
		if err := rows.Scan(&env.Env, &versionNumber); err != nil {
			return nil, fmt.Errorf("GetSnippetEnvironments scan: %w", err)
		}
		env.SnippetID = snippetID
		env.ActiveVersionNumber = versionNumber
		envs = append(envs, env)
	}
	return envs, rows.Err()
}

func scanVersion(s scannable) (*models.SnippetVersion, error) {
	var v models.SnippetVersion
	if err := s.Scan(
		&v.ID, &v.SnippetID, &v.VersionNumber, &v.Code,
		&v.InputSchema, &v.OutputSchema,
		&v.TimeoutMs, &v.MaxMemoryMB, &v.MaxCPUPercent,
		&v.Status, &v.CreatedAt, &v.CreatedBy,
	); err != nil {
		return nil, err
	}
	return &v, nil
}
