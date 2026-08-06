package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/ids"
	"github.com/abskrj/velane/services/control-plane/internal/models"
)

// CreateUser inserts a new user row and returns the created record.
func (s *Store) CreateUser(ctx context.Context, email, passwordHash string) (*models.User, error) {
	row := s.pool.QueryRow(ctx,
		`INSERT INTO users (id, email, password_hash)
		 VALUES ($1, $2, $3)
		 RETURNING id, email, password_hash, created_at, updated_at`,
		ids.New(), email, passwordHash,
	)
	return scanUser(row)
}

// CreateUserNoPassword inserts a user without a password (OAuth-only account).
func (s *Store) CreateUserNoPassword(ctx context.Context, email string) (*models.User, error) {
	row := s.pool.QueryRow(ctx,
		`INSERT INTO users (id, email)
		 VALUES ($1, $2)
		 RETURNING id, email, password_hash, created_at, updated_at`,
		ids.New(), email,
	)
	return scanUser(row)
}

// GetUserByOAuthIdentity returns the user linked to the given provider+subject pair.
func (s *Store) GetUserByOAuthIdentity(ctx context.Context, provider, subject string) (*models.User, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT u.id, u.email, u.password_hash, u.created_at, u.updated_at
		 FROM users u
		 JOIN oauth_identities oi ON oi.user_id = u.id
		 WHERE oi.provider = $1 AND oi.subject = $2`,
		provider, subject,
	)
	u, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("GetUserByOAuthIdentity: %w", err)
	}
	return u, nil
}

// CreateOAuthIdentity links an external identity to a user. It is idempotent on
// the (provider, subject) pair.
func (s *Store) CreateOAuthIdentity(ctx context.Context, userID, provider, subject, email string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO oauth_identities (id, user_id, provider, subject, email)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (provider, subject) DO NOTHING`,
		ids.New(), userID, provider, subject, email,
	)
	return err
}

// GetUserByEmail retrieves a user by email address. Returns an error if not found.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, created_at, updated_at FROM users WHERE email = $1`,
		email,
	)
	u, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("GetUserByEmail: %w", err)
	}
	return u, nil
}

// GetUserByID retrieves a user by its primary key.
func (s *Store) GetUserByID(ctx context.Context, id string) (*models.User, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, email, password_hash, created_at, updated_at FROM users WHERE id = $1`,
		id,
	)
	u, err := scanUser(row)
	if err != nil {
		return nil, fmt.Errorf("GetUserByID: %w", err)
	}
	return u, nil
}

// CreateSession inserts a new session row and returns the session record.
func (s *Store) CreateSession(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (*models.Session, error) {
	row := s.pool.QueryRow(ctx,
		`INSERT INTO user_sessions (id, user_id, token_hash, expires_at)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, user_id, token_hash, expires_at, created_at`,
		ids.New(), userID, tokenHash, expiresAt,
	)
	return scanSession(row)
}

// GetSessionByTokenHash retrieves a session by its hashed token value.
func (s *Store) GetSessionByTokenHash(ctx context.Context, tokenHash string) (*models.Session, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, user_id, token_hash, expires_at, created_at FROM user_sessions WHERE token_hash = $1`,
		tokenHash,
	)
	sess, err := scanSession(row)
	if err != nil {
		return nil, fmt.Errorf("GetSessionByTokenHash: %w", err)
	}
	return sess, nil
}

// DeleteSession removes a session by its hashed token value.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM user_sessions WHERE token_hash = $1`,
		tokenHash,
	)
	return err
}

// CreateRefreshToken inserts a new refresh token record and returns it.
func (s *Store) CreateRefreshToken(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (*models.RefreshToken, error) {
	return s.CreateRefreshTokenWithAssurance(ctx, userID, tokenHash, expiresAt, models.SessionAssurance{AuthMethod: "local"})
}

func (s *Store) CreateRefreshTokenWithAssurance(ctx context.Context, userID, tokenHash string, expiresAt time.Time, assurance models.SessionAssurance) (*models.RefreshToken, error) {
	row := s.pool.QueryRow(ctx,
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, auth_method, sso_tenant_id, sso_connection_id)
		 VALUES ($1, $2, $3, $4, $5, NULLIF($6,''), NULLIF($7,''))
		 RETURNING id, user_id, token_hash, expires_at, revoked_at, created_at, auth_method,
		 COALESCE(sso_tenant_id,''), COALESCE(sso_connection_id,'')`,
		ids.New(), userID, tokenHash, expiresAt, assurance.AuthMethod, assurance.SSOTenantID, assurance.SSOConnectionID,
	)
	return scanRefreshToken(row)
}

// GetRefreshTokenByHash retrieves a refresh token by its hashed value.
// Returns an error if the token does not exist, is revoked, or has expired.
func (s *Store) GetRefreshTokenByHash(ctx context.Context, tokenHash string) (*models.RefreshToken, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, user_id, token_hash, expires_at, revoked_at, created_at, auth_method,
		 COALESCE(sso_tenant_id,''), COALESCE(sso_connection_id,'')
		 FROM refresh_tokens
		 WHERE token_hash = $1`,
		tokenHash,
	)
	rt, err := scanRefreshToken(row)
	if err != nil {
		return nil, fmt.Errorf("GetRefreshTokenByHash: %w", err)
	}
	if rt.RevokedAt != nil {
		return nil, fmt.Errorf("refresh token has been revoked")
	}
	if rt.ExpiresAt.Before(time.Now()) {
		return nil, fmt.Errorf("refresh token has expired")
	}
	return rt, nil
}

// RevokeRefreshToken marks a refresh token as revoked by setting revoked_at.
func (s *Store) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE token_hash = $1`,
		tokenHash,
	)
	return err
}

func scanRefreshToken(s scannable) (*models.RefreshToken, error) {
	var rt models.RefreshToken
	if err := s.Scan(&rt.ID, &rt.UserID, &rt.TokenHash, &rt.ExpiresAt, &rt.RevokedAt, &rt.CreatedAt,
		&rt.AuthMethod, &rt.SSOTenantID, &rt.SSOConnectionID); err != nil {
		return nil, err
	}
	return &rt, nil
}

func scanUser(s scannable) (*models.User, error) {
	var u models.User
	var passwordHash sql.NullString
	if err := s.Scan(&u.ID, &u.Email, &passwordHash, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return nil, err
	}
	u.PasswordHash = passwordHash.String
	return &u, nil
}

func scanSession(s scannable) (*models.Session, error) {
	var sess models.Session
	if err := s.Scan(&sess.ID, &sess.UserID, &sess.TokenHash, &sess.ExpiresAt, &sess.CreatedAt); err != nil {
		return nil, err
	}
	return &sess, nil
}
