package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/abskrj/velane/services/control-plane/internal/ids"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/jackc/pgx/v5"
)

func (s *Store) GetSSOConnection(ctx context.Context, tenantID string) (*models.SSOConnection, error) {
	row := s.pool.QueryRow(ctx, `SELECT id, tenant_id, protocol, display_name, config_encrypted, enabled, enforced,
		tested_at, default_role, break_glass_user_id, created_at, updated_at FROM sso_connections WHERE tenant_id=$1`, tenantID)
	return scanSSOConnection(row)
}

func (s *Store) GetEnabledSSOConnectionBySlug(ctx context.Context, slug string) (*models.Tenant, *models.SSOConnection, error) {
	row := s.pool.QueryRow(ctx, `SELECT t.id, t.name, t.slug, t.created_at, t.egress_policy, t.replay_enabled, t.branding,
		t.runtime_limits, t.license_key, c.id, c.tenant_id, c.protocol, c.display_name, c.config_encrypted, c.enabled,
		c.enforced, c.tested_at, c.default_role, c.break_glass_user_id, c.created_at, c.updated_at
		FROM tenants t JOIN sso_connections c ON c.tenant_id=t.id WHERE t.slug=$1 AND c.enabled=true`, slug)
	var t models.Tenant
	var c models.SSOConnection
	var egress, branding, limits []byte
	if err := row.Scan(&t.ID, &t.Name, &t.Slug, &t.CreatedAt, &egress, &t.ReplayEnabled, &branding, &limits, &t.LicenseKey,
		&c.ID, &c.TenantID, &c.Protocol, &c.DisplayName, &c.ConfigEncrypted, &c.Enabled, &c.Enforced, &c.TestedAt,
		&c.DefaultRole, &c.BreakGlassUserID, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, nil, err
	}
	// Reuse the normal tenant scanner semantics by loading the complete record.
	full, err := s.GetTenantByID(ctx, t.ID)
	if err != nil {
		return nil, nil, err
	}
	return full, &c, nil
}

func (s *Store) UpsertSSOConnection(ctx context.Context, c *models.SSOConnection) (*models.SSOConnection, error) {
	if c.ID == "" {
		c.ID = ids.New()
	}
	row := s.pool.QueryRow(ctx, `INSERT INTO sso_connections
		(id,tenant_id,protocol,display_name,config_encrypted,enabled,enforced,tested_at,default_role,break_glass_user_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT(tenant_id) DO UPDATE SET protocol=EXCLUDED.protocol,display_name=EXCLUDED.display_name,
		config_encrypted=EXCLUDED.config_encrypted,enabled=EXCLUDED.enabled,enforced=EXCLUDED.enforced,
		tested_at=EXCLUDED.tested_at,default_role=EXCLUDED.default_role,break_glass_user_id=EXCLUDED.break_glass_user_id,updated_at=now()
		RETURNING id,tenant_id,protocol,display_name,config_encrypted,enabled,enforced,tested_at,default_role,break_glass_user_id,created_at,updated_at`,
		c.ID, c.TenantID, c.Protocol, c.DisplayName, c.ConfigEncrypted, c.Enabled, c.Enforced, c.TestedAt, c.DefaultRole, c.BreakGlassUserID)
	return scanSSOConnection(row)
}

func (s *Store) DeleteSSOConnection(ctx context.Context, tenantID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sso_connections WHERE tenant_id=$1`, tenantID)
	return err
}

func (s *Store) GetUserBySSOIdentity(ctx context.Context, connectionID, subject string) (*models.User, error) {
	row := s.pool.QueryRow(ctx, `SELECT u.id,u.email,u.password_hash,u.created_at,u.updated_at FROM users u
		JOIN sso_external_identities i ON i.user_id=u.id WHERE i.connection_id=$1 AND i.subject=$2`, connectionID, subject)
	return scanUser(row)
}

func (s *Store) CreateSSOIdentity(ctx context.Context, tenantID, connectionID, userID, subject, email string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO sso_external_identities(id,tenant_id,connection_id,user_id,subject,email)
		VALUES($1,$2,$3,$4,$5,$6)`, ids.New(), tenantID, connectionID, userID, subject, email)
	return err
}

func (s *Store) AddMemberIfMissing(ctx context.Context, tenantID, userID, role string) (bool, error) {
	tag, err := s.pool.Exec(ctx, `INSERT INTO tenant_members(tenant_id,user_id,role) VALUES($1,$2,$3)
		ON CONFLICT(tenant_id,user_id) DO NOTHING`, tenantID, userID, role)
	return err == nil && tag.RowsAffected() == 1, err
}

func IsNotFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

func scanSSOConnection(row scannable) (*models.SSOConnection, error) {
	var c models.SSOConnection
	if err := row.Scan(&c.ID, &c.TenantID, &c.Protocol, &c.DisplayName, &c.ConfigEncrypted, &c.Enabled, &c.Enforced,
		&c.TestedAt, &c.DefaultRole, &c.BreakGlassUserID, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, fmt.Errorf("scan SSO connection: %w", err)
	}
	return &c, nil
}
