package postgres

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/abskrj/velane/services/control-plane/internal/ids"
	"github.com/abskrj/velane/services/control-plane/internal/models"
)

// encrypt encrypts plaintext with AES-256-GCM using the provided 32-byte key.
// The returned string is base64(nonce + ciphertext).
func encrypt(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes.NewCipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("cipher.NewGCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("read nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt decrypts a value previously encrypted with encrypt().
func decrypt(key []byte, encoded string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("aes.NewCipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("cipher.NewGCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("gcm.Open: %w", err)
	}

	return string(plaintext), nil
}

// CreateSecret encrypts the plainValue and inserts a new secret/variable record.
// When isSecret is false, Value will be returned in list responses.
func (s *Store) CreateSecret(ctx context.Context, tenantID string, snippetID *string, name string, plainValue string, isSecret bool, environments []string, encKey []byte) (*models.Secret, error) {
	encrypted, err := encrypt(encKey, plainValue)
	if err != nil {
		return nil, fmt.Errorf("encrypt secret: %w", err)
	}

	if environments == nil {
		environments = []string{}
	}

	row := s.pool.QueryRow(ctx,
		`INSERT INTO secrets (id, tenant_id, snippet_id, name, value_encrypted, is_secret, environments)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 RETURNING id, tenant_id, snippet_id, name, is_secret, environments, created_at, updated_at`,
		ids.New(), tenantID, snippetID, name, encrypted, isSecret, environments,
	)

	sec, err := scanSecret(row)
	if err != nil {
		return nil, fmt.Errorf("CreateSecret scan: %w", err)
	}
	// For variables, populate Value so caller can return it in the response.
	if !isSecret {
		sec.Value = &plainValue
	}
	return sec, nil
}

// ListSecrets returns all variables and credentials for a tenant.
// For variables (is_secret=false), Value is populated with the decrypted plaintext.
// For credentials (is_secret=true), Value is nil — values are never returned.
func (s *Store) ListSecrets(ctx context.Context, tenantID string, encKey []byte) ([]*models.Secret, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, snippet_id, name, is_secret, value_encrypted, environments, created_at, updated_at
		 FROM secrets WHERE tenant_id = $1
		 ORDER BY is_secret ASC, created_at DESC`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("ListSecrets query: %w", err)
	}
	defer rows.Close()

	var secrets []*models.Secret
	for rows.Next() {
		var sec models.Secret
		var encrypted string
		if err := rows.Scan(&sec.ID, &sec.TenantID, &sec.SnippetID, &sec.Name, &sec.IsSecret, &encrypted, &sec.Environments, &sec.CreatedAt, &sec.UpdatedAt); err != nil {
			return nil, fmt.Errorf("ListSecrets scan: %w", err)
		}
		if sec.Environments == nil {
			sec.Environments = []string{}
		}
		if !sec.IsSecret {
			plain, err := decrypt(encKey, encrypted)
			if err != nil {
				return nil, fmt.Errorf("decrypt variable %q: %w", sec.Name, err)
			}
			sec.Value = &plain
		}
		secrets = append(secrets, &sec)
	}
	return secrets, rows.Err()
}

// UpdateSecret replaces the name and/or value of an existing secret/variable.
func (s *Store) UpdateSecret(ctx context.Context, id, tenantID string, name *string, plainValue *string, encKey []byte) (*models.Secret, error) {
	if name == nil && plainValue == nil {
		return nil, fmt.Errorf("nothing to update")
	}

	var encrypted *string
	if plainValue != nil {
		enc, err := encrypt(encKey, *plainValue)
		if err != nil {
			return nil, fmt.Errorf("encrypt: %w", err)
		}
		encrypted = &enc
	}

	row := s.pool.QueryRow(ctx,
		`UPDATE secrets SET
		   name            = COALESCE($3, name),
		   value_encrypted = COALESCE($4, value_encrypted),
		   updated_at      = now()
		 WHERE id = $1 AND tenant_id = $2
		 RETURNING id, tenant_id, snippet_id, name, is_secret, value_encrypted, environments, created_at, updated_at`,
		id, tenantID, name, encrypted,
	)

	var sec models.Secret
	var encryptedResult string
	var envs []string
	if err := row.Scan(&sec.ID, &sec.TenantID, &sec.SnippetID, &sec.Name, &sec.IsSecret, &encryptedResult, &envs, &sec.CreatedAt, &sec.UpdatedAt); err != nil {
		return nil, fmt.Errorf("UpdateSecret scan: %w", err)
	}
	if envs == nil {
		envs = []string{}
	}
	sec.Environments = envs
	if !sec.IsSecret {
		plain, err := decrypt(encKey, encryptedResult)
		if err != nil {
			return nil, fmt.Errorf("decrypt updated variable: %w", err)
		}
		sec.Value = &plain
	}
	return &sec, nil
}

// DeleteSecret removes a secret by ID, scoped to a tenant for safety.
func (s *Store) DeleteSecret(ctx context.Context, id, tenantID string) error {
	result, err := s.pool.Exec(ctx,
		`DELETE FROM secrets WHERE id = $1 AND tenant_id = $2`,
		id, tenantID,
	)
	if err != nil {
		return fmt.Errorf("DeleteSecret: %w", err)
	}
	if result.RowsAffected() == 0 {
		return fmt.Errorf("secret not found")
	}
	return nil
}

// GetSecretsForInvocation returns all secrets applicable to a snippet invocation.
// It returns snippet-specific secrets for the given env, falling back to
// tenant-wide secrets. Decrypts values using encKey.
// Returns map[name]plainValue. Snippet-specific secrets override tenant-wide
// ones with the same name.
func (s *Store) GetSecretsForInvocation(ctx context.Context, tenantID, snippetID, env string, encKey []byte) (map[string]string, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id, snippet_id, name, value_encrypted, environments, created_at
		 FROM secrets
		 WHERE tenant_id = $1
		   AND (snippet_id = $2 OR snippet_id IS NULL)
		   AND ($3 = ANY(environments) OR environments = '{}')
		 ORDER BY snippet_id NULLS FIRST`,
		tenantID, snippetID, env,
	)
	if err != nil {
		return nil, fmt.Errorf("GetSecretsForInvocation query: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var sec models.Secret
		var encrypted string
		if err := rows.Scan(&sec.ID, &sec.TenantID, &sec.SnippetID, &sec.Name, &encrypted, &sec.Environments, &sec.CreatedAt); err != nil {
			return nil, fmt.Errorf("GetSecretsForInvocation scan: %w", err)
		}

		plain, err := decrypt(encKey, encrypted)
		if err != nil {
			return nil, fmt.Errorf("decrypt secret %q: %w", sec.Name, err)
		}

		// Snippet-specific secrets override tenant-wide ones (they are ordered
		// tenant-wide first due to NULLS FIRST, then snippet-specific overwrites).
		result[sec.Name] = plain
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// EncryptForTest is an exported wrapper around encrypt for use in tests.
// It should not be used in production code.
func EncryptForTest(key []byte, plaintext string) (string, error) {
	return encrypt(key, plaintext)
}

// EncryptValue encrypts sensitive configuration using the service AES-256-GCM key.
func EncryptValue(key []byte, plaintext string) (string, error) { return encrypt(key, plaintext) }

// DecryptForTest is an exported wrapper around decrypt for use in tests.
// It should not be used in production code.
func DecryptForTest(key []byte, encoded string) (string, error) {
	return decrypt(key, encoded)
}

// DecryptValue decrypts configuration previously encrypted by EncryptValue.
func DecryptValue(key []byte, encoded string) (string, error) { return decrypt(key, encoded) }

// scanSecret scans a secret row from RETURNING clauses (no value_encrypted).
func scanSecret(s scannable) (*models.Secret, error) {
	var sec models.Secret
	if err := s.Scan(&sec.ID, &sec.TenantID, &sec.SnippetID, &sec.Name, &sec.IsSecret, &sec.Environments, &sec.CreatedAt, &sec.UpdatedAt); err != nil {
		return nil, err
	}
	if sec.Environments == nil {
		sec.Environments = []string{}
	}
	return &sec, nil
}
