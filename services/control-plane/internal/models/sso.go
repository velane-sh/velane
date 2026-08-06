package models

import "time"

type SSOConnection struct {
	ID               string     `json:"id"`
	TenantID         string     `json:"tenant_id"`
	Protocol         string     `json:"protocol"`
	DisplayName      string     `json:"display_name"`
	ConfigEncrypted  string     `json:"-"`
	Enabled          bool       `json:"enabled"`
	Enforced         bool       `json:"enforced"`
	TestedAt         *time.Time `json:"tested_at,omitempty"`
	DefaultRole      string     `json:"default_role"`
	BreakGlassUserID *string    `json:"break_glass_user_id,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type SessionAssurance struct {
	AuthMethod      string `json:"auth_method"`
	SSOTenantID     string `json:"sso_tenant_id,omitempty"`
	SSOConnectionID string `json:"sso_connection_id,omitempty"`
}
