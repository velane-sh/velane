package models

import "time"

// APIKey represents an authentication credential scoped to a tenant.
type APIKey struct {
	ID         string     `json:"id"`
	TenantID   string     `json:"tenant_id"`
	KeyHash    string     `json:"-"`          // bcrypt hash — never serialised
	KeyPrefix  string     `json:"key_prefix"` // first 8 chars after "vl_" prefix
	Name       string     `json:"name"`
	Scopes     []string   `json:"scopes"`            // "invoke", "manage", "admin"
	UserID     *string    `json:"user_id,omitempty"` // creating user; nil = legacy tenant-wide key
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// HasScope reports whether the key carries the requested scope.
func (k *APIKey) HasScope(scope string) bool {
	for _, s := range k.Scopes {
		if s == scope || s == "admin" {
			return true
		}
	}
	return false
}
