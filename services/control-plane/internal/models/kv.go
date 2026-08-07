package models

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const KVDefaultNamespace = "default"

const maxKVKeyBytes = 512
const maxKVNamespaceBytes = 64
const maxKVTTLSeconds int64 = 31536000

// KVEntry is one cross-invocation record. Value is nil in list responses (structural redaction).
type KVEntry struct {
	ID        string          `json:"id"`
	TenantID  string          `json:"tenant_id"`
	Namespace string          `json:"namespace"`
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value,omitempty"`
	SizeBytes int64           `json:"size_bytes"`
	ExpiresAt *time.Time      `json:"expires_at,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type KVNamespaceSummary struct {
	Namespace string `json:"namespace"`
	Keys      int64  `json:"keys"`
	SizeBytes int64  `json:"size_bytes"`
}

type KVUsage struct {
	Keys      int64 `json:"keys"`
	SizeBytes int64 `json:"size_bytes"`
}

type KVLimits struct {
	MaxKeys       int64 `json:"max_keys"`
	MaxValueBytes int64 `json:"max_value_bytes"`
	MaxTotalBytes int64 `json:"max_total_bytes"`
}

// DefaultKVLimits returns the platform storage limits for a tenant.
func DefaultKVLimits() KVLimits {
	return KVLimits{
		MaxKeys:       10000,
		MaxValueBytes: 131072,
		MaxTotalBytes: 67108864,
	}
}

// Normalize applies defaults for zero or negative stored values.
func (l KVLimits) Normalize() KVLimits {
	defaults := DefaultKVLimits()
	if l.MaxKeys <= 0 {
		l.MaxKeys = defaults.MaxKeys
	}
	if l.MaxValueBytes <= 0 {
		l.MaxValueBytes = defaults.MaxValueBytes
	}
	if l.MaxTotalBytes <= 0 {
		l.MaxTotalBytes = defaults.MaxTotalBytes
	}
	return l
}

// NormalizeKVNamespace maps an omitted entry namespace to the default namespace.
func NormalizeKVNamespace(ns string) string {
	if ns == "" {
		return KVDefaultNamespace
	}
	return strings.TrimSpace(ns)
}

// ValidateKVNamespace checks the namespace's bounded lowercase slug format.
func ValidateKVNamespace(ns string) error {
	if len(ns) == 0 || len(ns) > maxKVNamespaceBytes {
		return fmt.Errorf("namespace must be between 1 and %d bytes", maxKVNamespaceBytes)
	}
	if strings.HasPrefix(strings.ToLower(ns), "velane") {
		return fmt.Errorf("namespace prefix velane is reserved")
	}
	for i := 0; i < len(ns); i++ {
		c := ns[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || (i > 0 && (c == '_' || c == '-')) {
			continue
		}
		return fmt.Errorf("namespace must match ^[a-z0-9][a-z0-9_-]{0,63}$")
	}
	return nil
}

// ValidateKVKey checks the transport-safe key constraints.
func ValidateKVKey(key string) error {
	if len(key) == 0 || len(key) > maxKVKeyBytes {
		return fmt.Errorf("key must be between 1 and %d bytes", maxKVKeyBytes)
	}
	if strings.TrimSpace(key) != key {
		return fmt.Errorf("key must not have leading or trailing whitespace")
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x20 || key[i] == 0x7f {
			return fmt.Errorf("key must not contain ASCII control characters")
		}
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "." || segment == ".." {
			return fmt.Errorf("key must not contain . or .. path segments")
		}
	}
	return nil
}

// ValidateKVTTLSeconds checks an optional expiration duration.
func ValidateKVTTLSeconds(ttl *int64) error {
	if ttl == nil {
		return nil
	}
	if *ttl <= 0 || *ttl > maxKVTTLSeconds {
		return fmt.Errorf("ttl_seconds must be between 1 and %d", maxKVTTLSeconds)
	}
	return nil
}
