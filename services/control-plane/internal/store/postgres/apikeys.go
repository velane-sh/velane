package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/ids"
	"github.com/abskrj/velane/services/control-plane/internal/models"
	"golang.org/x/crypto/bcrypt"
)

// generateRawKey produces a cryptographically random plain-text API key in the
// format "vl_<32 hex chars>" and returns the plain key plus an 8-char prefix
// derived from the hex portion for efficient DB lookups.
func generateRawKey() (plain, prefix string, err error) {
	buf := make([]byte, 16)
	if _, err = rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("rand.Read: %w", err)
	}
	hexPart := hex.EncodeToString(buf) // 32 chars
	plain = "vl_" + hexPart
	prefix = hexPart[:8]
	return plain, prefix, nil
}

// CreateAPIKeyWithPlain generates a key, hashes it, persists the record, and
// returns both the model and the one-time plain-text key. The plain key is
// never stored — surface it to the caller immediately. The key is created
// without an owning user, so it carries tenant-wide access.
func (s *Store) CreateAPIKeyWithPlain(ctx context.Context, tenantID, name string, scopes []string) (*models.APIKey, string, error) {
	return s.CreateAPIKeyWithPlainForUser(ctx, tenantID, name, scopes, nil)
}

// CreateAPIKeyWithPlainForUser creates an API key owned by userID. The key
// inherits that user's group access when integrations are checked at runtime.
func (s *Store) CreateAPIKeyWithPlainForUser(ctx context.Context, tenantID, name string, scopes []string, userID *string) (*models.APIKey, string, error) {
	plain, prefix, err := generateRawKey()
	if err != nil {
		return nil, "", err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", fmt.Errorf("bcrypt: %w", err)
	}

	row := s.pool.QueryRow(ctx,
		`INSERT INTO api_keys (id, tenant_id, key_hash, key_prefix, name, scopes, user_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, tenant_id, key_hash, key_prefix, name, scopes, user_id, expires_at, last_used_at, created_at`,
		ids.New(), tenantID, string(hash), prefix, name, scopes, userID,
	)

	k, err := scanAPIKey(row)
	if err != nil {
		return nil, "", fmt.Errorf("CreateAPIKeyWithPlain scan: %w", err)
	}

	return k, plain, nil
}

// ValidateAPIKey accepts a plain-text key, finds the matching row by prefix,
// verifies the bcrypt hash, updates last_used_at, and returns the key record.
func (s *Store) ValidateAPIKey(ctx context.Context, plain string) (*models.APIKey, error) {
	if !strings.HasPrefix(plain, "vl_") || len(plain) < 11 {
		return nil, fmt.Errorf("invalid key format")
	}
	prefix := plain[3:11]

	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, key_hash, key_prefix, name, scopes, user_id, expires_at, last_used_at, created_at
		 FROM api_keys
		 WHERE key_prefix = $1`,
		prefix,
	)
	if err != nil {
		return nil, fmt.Errorf("ValidateAPIKey query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}

		if err := bcrypt.CompareHashAndPassword([]byte(k.KeyHash), []byte(plain)); err != nil {
			// Hash mismatch — prefix collision is extremely rare but handled.
			continue
		}

		// Check expiry.
		if k.ExpiresAt != nil && k.ExpiresAt.Before(time.Now()) {
			return nil, fmt.Errorf("api key has expired")
		}

		// Update last_used_at asynchronously so we don't block the request.
		go func(id string) {
			_, _ = s.pool.Exec(context.Background(),
				`UPDATE api_keys SET last_used_at = now() WHERE id = $1`,
				id,
			)
		}(k.ID)

		return k, nil
	}

	return nil, fmt.Errorf("invalid api key")
}

// GetAPIKey retrieves a key record by its primary key.
func (s *Store) GetAPIKey(ctx context.Context, id string) (*models.APIKey, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id, key_hash, key_prefix, name, scopes, user_id, expires_at, last_used_at, created_at
		 FROM api_keys WHERE id = $1`,
		id,
	)
	k, err := scanAPIKey(row)
	if err != nil {
		return nil, fmt.Errorf("GetAPIKey: %w", err)
	}
	return k, nil
}

// ListAPIKeys returns all API keys for a given tenant.
func (s *Store) ListAPIKeys(ctx context.Context, tenantID string) ([]*models.APIKey, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, key_hash, key_prefix, name, scopes, user_id, expires_at, last_used_at, created_at
		 FROM api_keys WHERE tenant_id = $1
		 ORDER BY created_at DESC`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("ListAPIKeys query: %w", err)
	}
	defer rows.Close()

	var keys []*models.APIKey
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, fmt.Errorf("ListAPIKeys scan: %w", err)
		}
		keys = append(keys, k)
	}
	if keys == nil {
		keys = []*models.APIKey{}
	}
	return keys, rows.Err()
}

// DeleteAPIKey removes an API key by primary key, scoped to the owning tenant.
// Returns an error if the key does not exist or belongs to a different tenant.
func (s *Store) DeleteAPIKey(ctx context.Context, tenantID, id string) error {
	result, err := s.pool.Exec(ctx,
		`DELETE FROM api_keys WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("api key not found")
	}
	return nil
}

// ---------------------------------------------------------------------------
// Scan helpers
// ---------------------------------------------------------------------------

// scannable is satisfied by both pgx Row and Rows so scan functions work for
// both single-row and multi-row queries.
type scannable interface {
	Scan(dest ...any) error
}

func scanAPIKey(s scannable) (*models.APIKey, error) {
	var k models.APIKey
	if err := s.Scan(
		&k.ID, &k.TenantID, &k.KeyHash, &k.KeyPrefix,
		&k.Name, &k.Scopes, &k.UserID, &k.ExpiresAt, &k.LastUsedAt, &k.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &k, nil
}
