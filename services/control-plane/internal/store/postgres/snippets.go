package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/ids"
	"github.com/abskrj/velane/services/control-plane/internal/models"
)

// CreateSnippet inserts a new snippet and ensures dev/prod environment rows
// exist for it (with no active version yet). The workflow ID and slug are both
// assigned as a new UUID v7; callers cannot set slug.
func (s *Store) CreateSnippet(ctx context.Context, tenantID, name, language, createdBy string) (*models.Snippet, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	id := ids.New()

	row := tx.QueryRow(ctx,
		`INSERT INTO snippets (id, tenant_id, name, slug, language, created_by)
		 VALUES ($1, $2, $3, $1, $4, $5)
		 RETURNING id, tenant_id, name, slug, language, created_at, created_by`,
		id, tenantID, name, language, createdBy,
	)

	var sn models.Snippet
	if err := row.Scan(&sn.ID, &sn.TenantID, &sn.Name, &sn.Slug, &sn.Language, &sn.CreatedAt, &sn.CreatedBy); err != nil {
		return nil, fmt.Errorf("CreateSnippet scan: %w", err)
	}

	// Seed environment rows for dev, staging, and prod.
	for _, env := range []string{"dev", "staging", "prod"} {
		_, err := tx.Exec(ctx,
			`INSERT INTO snippet_environments (snippet_id, env)
			 VALUES ($1, $2)
			 ON CONFLICT DO NOTHING`,
			sn.ID, env,
		)
		if err != nil {
			return nil, fmt.Errorf("seed env %s: %w", env, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return &sn, nil
}

// GetSnippetByID retrieves a snippet by its primary key.
func (s *Store) GetSnippetByID(ctx context.Context, id string) (*models.Snippet, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, name, slug, language, created_at, created_by
		 FROM snippets WHERE id = $1 AND deleted_at IS NULL`,
		id,
	)
	return scanSnippet(row)
}

// GetSnippetBySlug retrieves a snippet by tenant + slug.
func (s *Store) GetSnippetBySlug(ctx context.Context, tenantID, slug string) (*models.Snippet, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, name, slug, language, created_at, created_by
		 FROM snippets WHERE tenant_id = $1 AND slug = $2 AND deleted_at IS NULL`,
		tenantID, slug,
	)
	return scanSnippet(row)
}

// ListSnippets returns all non-archived snippets belonging to a tenant.
func (s *Store) ListSnippets(ctx context.Context, tenantID string) ([]*models.Snippet, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, name, slug, language, created_at, created_by
		 FROM snippets WHERE tenant_id = $1 AND deleted_at IS NULL
		 ORDER BY created_at DESC`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("ListSnippets query: %w", err)
	}
	defer rows.Close()

	var snippets []*models.Snippet
	for rows.Next() {
		sn, err := scanSnippet(rows)
		if err != nil {
			return nil, err
		}
		snippets = append(snippets, sn)
	}
	return snippets, rows.Err()
}

// DeleteSnippet soft-deletes a snippet by setting deleted_at.
func (s *Store) DeleteSnippet(ctx context.Context, id string) error {
	grace := s.objectGCGrace
	if grace <= 0 {
		grace = 7 * 24 * time.Hour
	}
	_, err := s.pool.Exec(ctx,
		`UPDATE snippets SET deleted_at = now(), objects_delete_after = now() + $2::interval WHERE id = $1`,
		id, durationInterval(grace),
	)
	return err
}

// GetSnippetEnvironment retrieves the environment record for a snippet+env pair.
func (s *Store) GetSnippetEnvironment(ctx context.Context, snippetID, env string) (*models.SnippetEnvironment, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT snippet_id, env, active_version_id, min_instances, canary_version_id, canary_pct
		 FROM snippet_environments WHERE snippet_id = $1 AND env = $2`,
		snippetID, env,
	)
	var se models.SnippetEnvironment
	if err := row.Scan(&se.SnippetID, &se.Env, &se.ActiveVersionID, &se.MinInstances, &se.CanaryVersionID, &se.CanaryPct); err != nil {
		return nil, fmt.Errorf("GetSnippetEnvironment scan: %w", err)
	}
	return &se, nil
}

func scanSnippet(s scannable) (*models.Snippet, error) {
	var sn models.Snippet
	if err := s.Scan(&sn.ID, &sn.TenantID, &sn.Name, &sn.Slug, &sn.Language, &sn.CreatedAt, &sn.CreatedBy); err != nil {
		return nil, fmt.Errorf("scanSnippet: %w", err)
	}
	return &sn, nil
}
