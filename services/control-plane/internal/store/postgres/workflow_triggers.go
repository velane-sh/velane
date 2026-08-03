package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/ids"
	"github.com/abskrj/velane/services/control-plane/internal/models"
)

const triggerColumns = `id, tenant_id, workflow_id, connection_id, provider_config_key, model, change_types, environment, enabled, activated_at, last_delivery_at, last_error, created_at, updated_at`

func scanTrigger(row interface{ Scan(...any) error }) (*models.WorkflowTrigger, error) {
	var t models.WorkflowTrigger
	err := row.Scan(&t.ID, &t.TenantID, &t.WorkflowID, &t.ConnectionID, &t.ProviderConfigKey, &t.Model, &t.ChangeTypes, &t.Environment, &t.Enabled, &t.ActivatedAt, &t.LastDeliveryAt, &t.LastError, &t.CreatedAt, &t.UpdatedAt)
	return &t, err
}

func (s *Store) GetConnectionByID(ctx context.Context, tenantID, id string) (*models.Connection, error) {
	row := s.pool.QueryRow(ctx, `SELECT id, tenant_id, provider, alias, provider_config_key, credential_profile_id, COALESCE(nango_connection_id,''), display_name, created_at, updated_at FROM connections WHERE id=$1 AND tenant_id=$2`, id, tenantID)
	var c models.Connection
	if err := row.Scan(&c.ID, &c.TenantID, &c.Provider, &c.Alias, &c.ProviderConfigKey, &c.CredentialProfileID, &c.NangoConnectionID, &c.DisplayName, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, fmt.Errorf("GetConnectionByID: %w", err)
	}
	return &c, nil
}

func (s *Store) FindConnectionForNangoEvent(ctx context.Context, nangoID, configKey string) (*models.Connection, error) {
	row := s.pool.QueryRow(ctx, `SELECT id, tenant_id, provider, alias, provider_config_key, credential_profile_id, COALESCE(nango_connection_id,''), display_name, created_at, updated_at FROM connections WHERE nango_connection_id=$1 AND provider_config_key=$2`, nangoID, configKey)
	var c models.Connection
	if err := row.Scan(&c.ID, &c.TenantID, &c.Provider, &c.Alias, &c.ProviderConfigKey, &c.CredentialProfileID, &c.NangoConnectionID, &c.DisplayName, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, fmt.Errorf("FindConnectionForNangoEvent: %w", err)
	}
	return &c, nil
}

func (s *Store) CreateWorkflowTrigger(ctx context.Context, t *models.WorkflowTrigger) (*models.WorkflowTrigger, error) {
	if t.ID == "" {
		t.ID = ids.New()
	}
	now := time.Now()
	row := s.pool.QueryRow(ctx, `INSERT INTO workflow_triggers (id,tenant_id,workflow_id,connection_id,provider_config_key,model,change_types,environment,enabled,activated_at,created_at,updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,FALSE,NULL,$9,$9) RETURNING `+triggerColumns, t.ID, t.TenantID, t.WorkflowID, t.ConnectionID, t.ProviderConfigKey, t.Model, t.ChangeTypes, t.Environment, now)
	r, err := scanTrigger(row)
	if err != nil {
		return nil, fmt.Errorf("CreateWorkflowTrigger: %w", err)
	}
	return r, nil
}

func (s *Store) ListWorkflowTriggers(ctx context.Context, tenantID, workflowID string) ([]models.WorkflowTrigger, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+triggerColumns+` FROM workflow_triggers WHERE tenant_id=$1 AND workflow_id=$2 ORDER BY created_at`, tenantID, workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []models.WorkflowTrigger{}
	for rows.Next() {
		t, e := scanTrigger(rows)
		if e != nil {
			return nil, e
		}
		result = append(result, *t)
	}
	return result, rows.Err()
}

func (s *Store) ListEnabledTriggersForEvent(ctx context.Context, connectionID, model string) ([]models.WorkflowTrigger, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+triggerColumns+` FROM workflow_triggers WHERE connection_id=$1 AND model=$2 AND enabled=TRUE ORDER BY created_at`, connectionID, model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []models.WorkflowTrigger{}
	for rows.Next() {
		t, e := scanTrigger(rows)
		if e != nil {
			return nil, e
		}
		result = append(result, *t)
	}
	return result, rows.Err()
}

func (s *Store) UpdateWorkflowTrigger(ctx context.Context, tenantID, workflowID, id, model string, changes []string, env string, enabled bool) (*models.WorkflowTrigger, error) {
	row := s.pool.QueryRow(ctx, `UPDATE workflow_triggers t SET model=$4,change_types=$5,environment=$6,enabled=$7,activated_at=CASE WHEN $7 AND NOT t.enabled THEN NOW() WHEN NOT $7 THEN NULL ELSE t.activated_at END,updated_at=NOW() WHERE t.id=$1 AND t.tenant_id=$2 AND t.workflow_id=$3 AND (NOT $7 OR EXISTS(SELECT 1 FROM snippet_environments se WHERE se.snippet_id=t.workflow_id AND se.env=$6 AND se.active_version_id IS NOT NULL)) RETURNING `+triggerColumns, id, tenantID, workflowID, model, changes, env, enabled)
	t, err := scanTrigger(row)
	if err != nil {
		return nil, fmt.Errorf("UpdateWorkflowTrigger: %w", err)
	}
	return t, nil
}

func (s *Store) DeleteWorkflowTrigger(ctx context.Context, tenantID, workflowID, id string) error {
	r, e := s.pool.Exec(ctx, `DELETE FROM workflow_triggers WHERE id=$1 AND tenant_id=$2 AND workflow_id=$3`, id, tenantID, workflowID)
	if e != nil {
		return e
	}
	if r.RowsAffected() == 0 {
		return fmt.Errorf("trigger not found")
	}
	return nil
}

func (s *Store) CreateIntegrationEventReceipt(ctx context.Context, r *models.IntegrationEventReceipt) (bool, error) {
	if r.ID == "" {
		r.ID = ids.New()
	}
	tag, e := s.pool.Exec(ctx, `INSERT INTO integration_event_receipts(id,deduplication_key,tenant_id,connection_id,provider_config_key,model,modified_after,initial_sync,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(deduplication_key) DO NOTHING`, r.ID, r.DeduplicationKey, r.TenantID, r.ConnectionID, r.ProviderConfigKey, r.Model, r.ModifiedAfter, r.InitialSync, r.Payload)
	if e != nil {
		return false, e
	}
	return tag.RowsAffected() == 1, nil
}
func (s *Store) DeletePendingIntegrationEventReceipt(ctx context.Context, id string) error {
	_, e := s.pool.Exec(ctx, `DELETE FROM integration_event_receipts WHERE id=$1 AND status='pending'`, id)
	return e
}
func (s *Store) GetIntegrationEventReceipt(ctx context.Context, id string) (*models.IntegrationEventReceipt, error) {
	var r models.IntegrationEventReceipt
	e := s.pool.QueryRow(ctx, `SELECT id,deduplication_key,tenant_id,connection_id,provider_config_key,model,modified_after,initial_sync,payload,status,attempts FROM integration_event_receipts WHERE id=$1`, id).Scan(&r.ID, &r.DeduplicationKey, &r.TenantID, &r.ConnectionID, &r.ProviderConfigKey, &r.Model, &r.ModifiedAfter, &r.InitialSync, &r.Payload, &r.Status, &r.Attempts)
	return &r, e
}
func (s *Store) MarkIntegrationEvent(ctx context.Context, id, status, errMsg string) error {
	_, e := s.pool.Exec(ctx, `UPDATE integration_event_receipts SET status=$2,last_error=$3,attempts=attempts+CASE WHEN $2='processing' THEN 1 ELSE 0 END,updated_at=NOW() WHERE id=$1`, id, status, errMsg)
	return e
}
func (s *Store) UpsertTriggerDispatch(ctx context.Context, receiptID, triggerID string, invocationIDs []string, status, errMsg string) error {
	_, e := s.pool.Exec(ctx, `INSERT INTO workflow_trigger_dispatches(id,receipt_id,trigger_id,invocation_ids,status,last_error) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(receipt_id,trigger_id) DO UPDATE SET invocation_ids=CASE WHEN cardinality(workflow_trigger_dispatches.invocation_ids)=0 THEN EXCLUDED.invocation_ids ELSE workflow_trigger_dispatches.invocation_ids END,status=EXCLUDED.status,last_error=EXCLUDED.last_error,updated_at=NOW()`, ids.New(), receiptID, triggerID, invocationIDs, status, errMsg)
	if e == nil {
		_, e = s.pool.Exec(ctx, `UPDATE workflow_triggers SET last_delivery_at=CASE WHEN $2='completed' THEN NOW() ELSE last_delivery_at END,last_error=$3,updated_at=NOW() WHERE id=$1`, triggerID, status, errMsg)
	}
	return e
}
