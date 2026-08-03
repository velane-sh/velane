package models

import "time"

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Session struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	Token          string    `json:"token,omitempty"` // raw access token, only set on creation
	RefreshToken   string    `json:"-"`               // raw refresh token, only set on creation
	TokenHash      string    `json:"-"`
	ExpiresAt      time.Time `json:"expires_at"`
	RefreshExpires time.Time `json:"-"`
	CreatedAt      time.Time `json:"created_at"`
}

type RefreshToken struct {
	ID              string     `json:"id"`
	UserID          string     `json:"user_id"`
	Token           string     `json:"token,omitempty"` // raw, only on creation
	TokenHash       string     `json:"-"`
	ExpiresAt       time.Time  `json:"expires_at"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	AuthMethod      string     `json:"auth_method"`
	SSOTenantID     string     `json:"sso_tenant_id,omitempty"`
	SSOConnectionID string     `json:"sso_connection_id,omitempty"`
}

type TenantMember struct {
	TenantID  string    `json:"tenant_id"`
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	InvitedAt time.Time `json:"invited_at"`
}

type UserTenantMembership struct {
	TenantID string `json:"tenant_id"`
	Slug     string `json:"slug"`
	Name     string `json:"name"`
	Role     string `json:"role"`
}

type InviteToken struct {
	ID         string     `json:"id"`
	TenantID   string     `json:"tenant_id"`
	Email      string     `json:"email"`
	Role       string     `json:"role"`
	Token      string     `json:"token,omitempty"` // raw token, only on creation
	TokenHash  string     `json:"-"`
	ExpiresAt  time.Time  `json:"expires_at"`
	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}
