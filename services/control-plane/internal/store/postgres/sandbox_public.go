package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/abskrj/velane/services/control-plane/internal/ids"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/jackc/pgx/v5"
	"strconv"
)

func page(limit, offset int) (int, int) {
	if limit < 1 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
func (s *Store) ListSandboxProfiles(ctx context.Context) ([]models.SandboxProfileDTO, error) {
	rows, e := s.pool.Query(ctx, `SELECT id,profile_family,name,version,status,vcpu,memory_mb FROM sandbox_profile_versions WHERE status='active' ORDER BY profile_family,version`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []models.SandboxProfileDTO
	for rows.Next() {
		var v models.SandboxProfileDTO
		if e := rows.Scan(&v.ID, &v.ProfileFamily, &v.Name, &v.Version, &v.Status, &v.VCPU, &v.MemoryMB); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) ListSandboxSnapshots(ctx context.Context, tenant, sandbox string, limit, offset int) ([]models.SandboxSnapshot, int, error) {
	limit, offset = page(limit, offset)
	var n int
	if e := s.pool.QueryRow(ctx, `SELECT count(*) FROM sandbox_snapshots WHERE tenant_id=$1 AND sandbox_id=$2 AND state<>'deleted'`, tenant, sandbox).Scan(&n); e != nil {
		return nil, 0, e
	}
	rows, e := s.pool.Query(ctx, `SELECT id,tenant_id,sandbox_id,operation_id,generation,kind,state,lineage_id,source_host_compatibility_key,vm_restore_descriptor_digest,snapshot_compatibility_key,manifest_version,total_bytes,failure_code,failure_message,created_at FROM sandbox_snapshots WHERE tenant_id=$1 AND sandbox_id=$2 AND state<>'deleted' ORDER BY created_at DESC LIMIT $3 OFFSET $4`, tenant, sandbox, limit, offset)
	if e != nil {
		return nil, 0, e
	}
	defer rows.Close()
	var out []models.SandboxSnapshot
	for rows.Next() {
		var v models.SandboxSnapshot
		if e := rows.Scan(&v.ID, &v.TenantID, &v.SandboxID, &v.OperationID, &v.Generation, &v.Kind, &v.State, &v.LineageID, &v.SourceHostCompatibilityKey, &v.VMRestoreDescriptorDigest, &v.SnapshotCompatibilityKey, &v.ManifestVersion, &v.TotalBytes, &v.FailureCode, &v.FailureMessage, &v.CreatedAt); e != nil {
			return nil, 0, e
		}
		out = append(out, v)
	}
	return out, n, rows.Err()
}
func (s *Store) GetSandboxSnapshot(ctx context.Context, tenant, sandbox, id string) (*models.SandboxSnapshot, error) {
	var v models.SandboxSnapshot
	e := s.pool.QueryRow(ctx, `SELECT id,tenant_id,sandbox_id,operation_id,generation,kind,state,lineage_id,source_host_compatibility_key,vm_restore_descriptor_digest,snapshot_compatibility_key,manifest_version,total_bytes,failure_code,failure_message,created_at FROM sandbox_snapshots WHERE tenant_id=$1 AND sandbox_id=$2 AND id=$3 AND state<>'deleted'`, tenant, sandbox, id).Scan(&v.ID, &v.TenantID, &v.SandboxID, &v.OperationID, &v.Generation, &v.Kind, &v.State, &v.LineageID, &v.SourceHostCompatibilityKey, &v.VMRestoreDescriptorDigest, &v.SnapshotCompatibilityKey, &v.ManifestVersion, &v.TotalBytes, &v.FailureCode, &v.FailureMessage, &v.CreatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &v, e
}
func (s *Store) GetSandboxOperation(ctx context.Context, tenant, id string) (*models.SandboxOperation, error) {
	var o models.SandboxOperation
	e := s.pool.QueryRow(ctx, `SELECT id,tenant_id,sandbox_id,recipe_version_id,snapshot_id,retry_of_operation_id,kind,state,requested_generation,attempt,max_attempts,deadline_at,failure_code,failure_message,result,created_at,updated_at FROM sandbox_operations WHERE tenant_id=$1 AND id=$2`, tenant, id).Scan(&o.ID, &o.TenantID, &o.SandboxID, &o.RecipeVersionID, &o.SnapshotID, &o.RetryOfOperationID, &o.Kind, &o.State, &o.RequestedGeneration, &o.Attempt, &o.MaxAttempts, &o.DeadlineAt, &o.FailureCode, &o.FailureMessage, &o.Result, &o.CreatedAt, &o.UpdatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &o, e
}
func (s *Store) ListSandboxEvents(ctx context.Context, tenant, sandbox, after string, limit int) ([]models.SandboxEventDTO, string, error) {
	limit, _ = page(limit, 0)
	rows, e := s.pool.Query(ctx, `SELECT id,type,level,message,details,created_at,sequence FROM sandbox_events WHERE tenant_id=$1 AND sandbox_id=$2 AND ($3='' OR sequence>$3::bigint) ORDER BY sequence LIMIT $4`, tenant, sandbox, after, limit)
	if e != nil {
		return nil, "", e
	}
	defer rows.Close()
	var out []models.SandboxEventDTO
	var next string
	for rows.Next() {
		var x models.SandboxEventDTO
		var seq int64
		if e := rows.Scan(&x.ID, &x.Type, &x.Level, &x.Message, &x.Details, &x.CreatedAt, &seq); e != nil {
			return nil, "", e
		}
		next = strconv.FormatInt(seq, 10)
		out = append(out, x)
	}
	return out, next, rows.Err()
}
func (s *Store) ListSandboxLogs(ctx context.Context, tenant, sandbox, after string, limit int) ([]models.SandboxLogDTO, string, error) {
	limit, _ = page(limit, 0)
	rows, e := s.pool.Query(ctx, `SELECT id,source,stream,message,created_at,sequence FROM sandbox_logs WHERE tenant_id=$1 AND sandbox_id=$2 AND ($3='' OR sequence>$3::bigint) ORDER BY sequence LIMIT $4`, tenant, sandbox, after, limit)
	if e != nil {
		return nil, "", e
	}
	defer rows.Close()
	var out []models.SandboxLogDTO
	var next string
	for rows.Next() {
		var x models.SandboxLogDTO
		var seq int64
		if e := rows.Scan(&x.ID, &x.Source, &x.Stream, &x.Message, &x.CreatedAt, &seq); e != nil {
			return nil, "", e
		}
		next = strconv.FormatInt(seq, 10)
		out = append(out, x)
	}
	return out, next, rows.Err()
}
func (s *Store) ListRecipes(ctx context.Context, tenant string, limit, offset int) ([]models.SandboxRecipeDTO, int, error) {
	limit, offset = page(limit, offset)
	var n int
	if e := s.pool.QueryRow(ctx, `SELECT count(*) FROM sandbox_image_recipes WHERE tenant_id=$1 AND deleted_at IS NULL`, tenant).Scan(&n); e != nil {
		return nil, 0, e
	}
	rows, e := s.pool.Query(ctx, `SELECT id,name,slug,description,created_at,updated_at FROM sandbox_image_recipes WHERE tenant_id=$1 AND deleted_at IS NULL ORDER BY created_at DESC LIMIT $2 OFFSET $3`, tenant, limit, offset)
	if e != nil {
		return nil, 0, e
	}
	defer rows.Close()
	var out []models.SandboxRecipeDTO
	for rows.Next() {
		var x models.SandboxRecipeDTO
		if e := rows.Scan(&x.ID, &x.Name, &x.Slug, &x.Description, &x.CreatedAt, &x.UpdatedAt); e != nil {
			return nil, 0, e
		}
		out = append(out, x)
	}
	return out, n, rows.Err()
}
func (s *Store) GetRecipe(ctx context.Context, tenant, id string) (*models.SandboxRecipeDTO, error) {
	var x models.SandboxRecipeDTO
	e := s.pool.QueryRow(ctx, `SELECT id,name,slug,description,created_at,updated_at FROM sandbox_image_recipes WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenant, id).Scan(&x.ID, &x.Name, &x.Slug, &x.Description, &x.CreatedAt, &x.UpdatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &x, e
}
func (s *Store) CreateRecipe(ctx context.Context, tenant, name, slug, description, actor string) (*models.SandboxRecipeDTO, error) {
	x := &models.SandboxRecipeDTO{ID: ids.New(), Name: name, Slug: slug, Description: description}
	e := s.pool.QueryRow(ctx, `INSERT INTO sandbox_image_recipes(id,tenant_id,name,slug,description,created_by) VALUES($1,$2,$3,$4,$5,$6) RETURNING created_at,updated_at`, x.ID, tenant, name, slug, description, actor).Scan(&x.CreatedAt, &x.UpdatedAt)
	return x, e
}
func (s *Store) CreateRecipeIntent(ctx context.Context, tenant, name, slug, description, actor, key, hash string) (*models.SandboxRecipeDTO, bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var existingHash, recipeID string
	err = tx.QueryRow(ctx, `SELECT request_hash,recipe_id FROM sandbox_recipe_request_keys WHERE tenant_id=$1 AND idempotency_key=$2 FOR UPDATE`, tenant, key).Scan(&existingHash, &recipeID)
	if err == nil {
		if existingHash != hash {
			return nil, false, fmt.Errorf("idempotency conflict")
		}
		var recipe models.SandboxRecipeDTO
		err = tx.QueryRow(ctx, `SELECT id,name,slug,description,created_at,updated_at FROM sandbox_image_recipes WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tenant, recipeID).Scan(&recipe.ID, &recipe.Name, &recipe.Slug, &recipe.Description, &recipe.CreatedAt, &recipe.UpdatedAt)
		if err != nil {
			return nil, false, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, false, err
		}
		return &recipe, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, err
	}
	recipe := &models.SandboxRecipeDTO{ID: ids.New(), Name: name, Slug: slug, Description: description}
	if err = tx.QueryRow(ctx, `INSERT INTO sandbox_image_recipes(id,tenant_id,name,slug,description,created_by) VALUES($1,$2,$3,$4,$5,$6) RETURNING created_at,updated_at`, recipe.ID, tenant, name, slug, description, actor).Scan(&recipe.CreatedAt, &recipe.UpdatedAt); err != nil {
		return nil, false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO sandbox_recipe_request_keys(tenant_id,idempotency_key,request_hash,recipe_id) VALUES($1,$2,$3,$4)`, tenant, key, hash, recipe.ID); err != nil {
		return nil, false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(id,tenant_id,actor_id,actor_type,action,resource_id,metadata) VALUES($1,$2,$3,'user','sandbox_image_recipe.created',$4,$5)`, ids.New(), tenant, actor, recipe.ID, fmt.Sprintf(`{"slug":%q}`, recipe.Slug)); err != nil {
		return nil, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	return recipe, false, nil
}
func (s *Store) DeleteRecipe(ctx context.Context, tenant, id string) error {
	tag, e := s.pool.Exec(ctx, `UPDATE sandbox_image_recipes SET deleted_at=now(),updated_at=now() WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL AND NOT EXISTS(SELECT 1 FROM sandboxes WHERE recipe_version_id IN(SELECT id FROM sandbox_image_recipe_versions WHERE recipe_id=$2))`, tenant, id)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) ListRecipeVersions(ctx context.Context, tenant, recipe string, limit, offset int) ([]models.SandboxRecipeVersionDTO, int, error) {
	limit, offset = page(limit, offset)
	var n int
	if e := s.pool.QueryRow(ctx, `SELECT count(*) FROM sandbox_image_recipe_versions WHERE tenant_id=$1 AND recipe_id=$2`, tenant, recipe).Scan(&n); e != nil {
		return nil, 0, e
	}
	rows, e := s.pool.Query(ctx, `SELECT id,recipe_id,version_number,schema_version,status,failure_code,failure_message,created_at FROM sandbox_image_recipe_versions WHERE tenant_id=$1 AND recipe_id=$2 ORDER BY version_number DESC LIMIT $3 OFFSET $4`, tenant, recipe, limit, offset)
	if e != nil {
		return nil, 0, e
	}
	defer rows.Close()
	var out []models.SandboxRecipeVersionDTO
	for rows.Next() {
		var x models.SandboxRecipeVersionDTO
		if e := rows.Scan(&x.ID, &x.RecipeID, &x.VersionNumber, &x.SchemaVersion, &x.Status, &x.FailureCode, &x.FailureMessage, &x.CreatedAt); e != nil {
			return nil, 0, e
		}
		out = append(out, x)
	}
	return out, n, rows.Err()
}
func (s *Store) GetRecipeVersion(ctx context.Context, tenant, recipe string, version int) (*models.SandboxRecipeVersionDTO, error) {
	var x models.SandboxRecipeVersionDTO
	e := s.pool.QueryRow(ctx, `SELECT id,recipe_id,version_number,schema_version,status,failure_code,failure_message,document,created_at FROM sandbox_image_recipe_versions WHERE tenant_id=$1 AND recipe_id=$2 AND version_number=$3`, tenant, recipe, version).Scan(&x.ID, &x.RecipeID, &x.VersionNumber, &x.SchemaVersion, &x.Status, &x.FailureCode, &x.FailureMessage, &x.Document, &x.CreatedAt)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &x, e
}
func (s *Store) CreateRecipeVersionIntent(ctx context.Context, tenant, recipe, schema string, document json.RawMessage, digest, base, actor, key, hash string) (*models.SandboxRecipeVersionDTO, *models.SandboxOperation, bool, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var existing models.SandboxOperation
	var existingHash string
	err = tx.QueryRow(ctx, `SELECT id,tenant_id,sandbox_id,recipe_version_id,snapshot_id,retry_of_operation_id,kind,state,requested_generation,attempt,max_attempts,deadline_at,failure_code,failure_message,result,created_at,updated_at,request_hash FROM sandbox_operations WHERE tenant_id=$1 AND idempotency_key=$2 FOR UPDATE`, tenant, key).Scan(&existing.ID, &existing.TenantID, &existing.SandboxID, &existing.RecipeVersionID, &existing.SnapshotID, &existing.RetryOfOperationID, &existing.Kind, &existing.State, &existing.RequestedGeneration, &existing.Attempt, &existing.MaxAttempts, &existing.DeadlineAt, &existing.FailureCode, &existing.FailureMessage, &existing.Result, &existing.CreatedAt, &existing.UpdatedAt, &existingHash)
	if err == nil {
		if existingHash != hash || existing.Kind != "recipe_build" || existing.RecipeVersionID == nil {
			return nil, nil, false, fmt.Errorf("idempotency conflict")
		}
		var version models.SandboxRecipeVersionDTO
		err = tx.QueryRow(ctx, `SELECT id,recipe_id,version_number,schema_version,status,failure_code,failure_message,created_at FROM sandbox_image_recipe_versions WHERE tenant_id=$1 AND id=$2`, tenant, *existing.RecipeVersionID).Scan(&version.ID, &version.RecipeID, &version.VersionNumber, &version.SchemaVersion, &version.Status, &version.FailureCode, &version.FailureMessage, &version.CreatedAt)
		if err != nil {
			return nil, nil, false, err
		}
		if err = tx.Commit(ctx); err != nil {
			return nil, nil, false, err
		}
		return &version, &existing, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, false, err
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sandbox_image_recipes WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL)`, tenant, recipe).Scan(&exists); err != nil || !exists {
		if err == nil {
			err = ErrNotFound
		}
		return nil, nil, false, err
	}
	var number int
	if err = tx.QueryRow(ctx, `SELECT coalesce(max(version_number),0)+1 FROM sandbox_image_recipe_versions WHERE tenant_id=$1 AND recipe_id=$2 FOR UPDATE`, tenant, recipe).Scan(&number); err != nil {
		return nil, nil, false, err
	}
	version := &models.SandboxRecipeVersionDTO{ID: ids.New(), RecipeID: recipe, VersionNumber: number, SchemaVersion: schema, Status: "queued"}
	if err = tx.QueryRow(ctx, `INSERT INTO sandbox_image_recipe_versions(id,tenant_id,recipe_id,version_number,schema_version,document,document_digest,resolved_base_digest,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'queued') RETURNING created_at`, version.ID, tenant, recipe, number, schema, document, digest, base).Scan(&version.CreatedAt); err != nil {
		return nil, nil, false, err
	}
	op := &models.SandboxOperation{ID: ids.New(), TenantID: tenant, RecipeVersionID: &version.ID, Kind: "recipe_build", State: "queued", MaxAttempts: 3}
	if err = tx.QueryRow(ctx, `INSERT INTO sandbox_operations(id,tenant_id,recipe_version_id,kind,state,idempotency_key,request_hash,actor_id,actor_type,max_attempts) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'user',$9) RETURNING created_at,updated_at`, op.ID, tenant, version.ID, op.Kind, op.State, key, hash, actor, op.MaxAttempts).Scan(&op.CreatedAt, &op.UpdatedAt); err != nil {
		return nil, nil, false, err
	}
	if err = recipeEventTx(ctx, tx, tenant, version.ID, op.ID, "sandbox_image_recipe.version_build_requested", "image recipe build requested"); err != nil {
		return nil, nil, false, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_log(id,tenant_id,actor_id,actor_type,action,resource_id,metadata) VALUES($1,$2,$3,'user','sandbox_image_recipe.version_build_requested',$4,$5)`, ids.New(), tenant, actor, recipe, fmt.Sprintf(`{"operation_id":%q,"recipe_version_id":%q,"document_digest":%q}`, op.ID, version.ID, digest)); err != nil {
		return nil, nil, false, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, nil, false, err
	}
	return version, op, false, nil
}
func (s *Store) ListRecipeEvents(ctx context.Context, tenant, recipe string, version int, after string, limit int) ([]models.SandboxEventDTO, string, error) {
	limit, _ = page(limit, 0)
	rows, err := s.pool.Query(ctx, `SELECT e.id,e.type,e.level,e.message,e.details,e.created_at,e.sequence FROM sandbox_events e JOIN sandbox_image_recipe_versions v ON v.id=e.recipe_version_id AND v.tenant_id=e.tenant_id WHERE e.tenant_id=$1 AND v.recipe_id=$2 AND v.version_number=$3 AND ($4='' OR e.sequence>$4::bigint) ORDER BY e.sequence LIMIT $5`, tenant, recipe, version, after, limit)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []models.SandboxEventDTO
	var next string
	for rows.Next() {
		var item models.SandboxEventDTO
		var sequence int64
		if err := rows.Scan(&item.ID, &item.Type, &item.Level, &item.Message, &item.Details, &item.CreatedAt, &sequence); err != nil {
			return nil, "", err
		}
		out = append(out, item)
		next = strconv.FormatInt(sequence, 10)
	}
	return out, next, rows.Err()
}
func (s *Store) ListRecipeLogs(ctx context.Context, tenant, recipe string, version int, after string, limit int) ([]models.SandboxLogDTO, string, error) {
	limit, _ = page(limit, 0)
	rows, err := s.pool.Query(ctx, `SELECT l.id,l.source,l.stream,l.message,l.created_at,l.sequence FROM sandbox_logs l JOIN sandbox_image_recipe_versions v ON v.id=l.recipe_version_id AND v.tenant_id=l.tenant_id WHERE l.tenant_id=$1 AND v.recipe_id=$2 AND v.version_number=$3 AND ($4='' OR l.sequence>$4::bigint) ORDER BY l.sequence LIMIT $5`, tenant, recipe, version, after, limit)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	var out []models.SandboxLogDTO
	var next string
	for rows.Next() {
		var item models.SandboxLogDTO
		var sequence int64
		if err := rows.Scan(&item.ID, &item.Source, &item.Stream, &item.Message, &item.CreatedAt, &sequence); err != nil {
			return nil, "", err
		}
		out = append(out, item)
		next = strconv.FormatInt(sequence, 10)
	}
	return out, next, rows.Err()
}
func (s *Store) CreateRecipeVersion(ctx context.Context, tenant, recipe, schema string, doc json.RawMessage, digest, base string) (*models.SandboxRecipeVersionDTO, error) {
	var n int
	if e := s.pool.QueryRow(ctx, `SELECT coalesce(max(version_number),0)+1 FROM sandbox_image_recipe_versions WHERE tenant_id=$1 AND recipe_id=$2`, tenant, recipe).Scan(&n); e != nil {
		return nil, e
	}
	x := &models.SandboxRecipeVersionDTO{ID: ids.New(), RecipeID: recipe, VersionNumber: n, SchemaVersion: schema, Status: "queued"}
	e := s.pool.QueryRow(ctx, `INSERT INTO sandbox_image_recipe_versions(id,tenant_id,recipe_id,version_number,schema_version,document,document_digest,resolved_base_digest,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'queued') RETURNING created_at`, x.ID, tenant, recipe, n, schema, doc, digest, base).Scan(&x.CreatedAt)
	return x, e
}
func eventTx(ctx context.Context, tx pgx.Tx, tenant, sandbox, operation, typ, msg string, generation int64) error {
	var seq int64
	if e := tx.QueryRow(ctx, `SELECT coalesce(max(sequence),0)+1 FROM sandbox_events WHERE tenant_id=$1 AND sandbox_id=$2`, tenant, sandbox).Scan(&seq); e != nil {
		return e
	}
	eid := ids.New()
	if _, e := tx.Exec(ctx, `INSERT INTO sandbox_events(id,tenant_id,sandbox_id,operation_id,sequence,type,source,level,message,generation) VALUES($1,$2,$3,$4,$5,$6,'control_plane','info',$7,$8)`, eid, tenant, sandbox, operation, seq, typ, msg, generation); e != nil {
		return e
	}
	_, e := tx.Exec(ctx, `INSERT INTO sandbox_outbox(id,event_id,topic,payload) VALUES($1,$2,$3,$4)`, ids.New(), eid, "sandbox.events", fmt.Sprintf(`{"event_id":%q}`, eid))
	return e
}
func recipeEventTx(ctx context.Context, tx pgx.Tx, tenant, version, operation, typ, msg string) error {
	var sequence int64
	if err := tx.QueryRow(ctx, `SELECT coalesce(max(sequence),0)+1 FROM sandbox_events WHERE tenant_id=$1 AND recipe_version_id=$2`, tenant, version).Scan(&sequence); err != nil {
		return err
	}
	id := ids.New()
	if _, err := tx.Exec(ctx, `INSERT INTO sandbox_events(id,tenant_id,recipe_version_id,operation_id,sequence,type,source,level,message) VALUES($1,$2,$3,$4,$5,$6,'control_plane','info',$7)`, id, tenant, version, operation, sequence, typ, msg); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO sandbox_outbox(id,event_id,topic,payload) VALUES($1,$2,$3,$4)`, ids.New(), id, "sandbox.events", fmt.Sprintf(`{"event_id":%q}`, id))
	return err
}
