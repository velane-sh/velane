package models

import (
	"strings"
	"testing"
)

func TestKVLimitsNormalize(t *testing.T) {
	defaults := DefaultKVLimits()
	got := (KVLimits{MaxKeys: -1, MaxValueBytes: 0, MaxTotalBytes: -2}).Normalize()
	if got != defaults {
		t.Fatalf("Normalize() = %+v; want %+v", got, defaults)
	}
}

func TestNormalizeKVNamespace(t *testing.T) {
	if got := NormalizeKVNamespace(""); got != KVDefaultNamespace {
		t.Fatalf("empty namespace = %q; want %q", got, KVDefaultNamespace)
	}
	if got := NormalizeKVNamespace(" sync "); got != "sync" {
		t.Fatalf("trimmed namespace = %q; want sync", got)
	}
}

func TestValidateKVNamespace(t *testing.T) {
	valid := []string{"default", "sync", "a", "a_b-2", strings.Repeat("a", 64)}
	for _, namespace := range valid {
		if err := ValidateKVNamespace(namespace); err != nil {
			t.Errorf("ValidateKVNamespace(%q): %v", namespace, err)
		}
	}
	invalid := []string{"", "Upper", "-start", "a space", "velane", "Velane-x", strings.Repeat("a", 65)}
	for _, namespace := range invalid {
		if err := ValidateKVNamespace(namespace); err == nil {
			t.Errorf("ValidateKVNamespace(%q) succeeded; want error", namespace)
		}
	}
}

func TestValidateKVKey(t *testing.T) {
	valid := []string{"a..b", "run.2026-08-04", "..leading-dots-in-one-segment", "user/42/profile", "50%off", "a_b", `back\slash`}
	for _, key := range valid {
		if err := ValidateKVKey(key); err != nil {
			t.Errorf("ValidateKVKey(%q): %v", key, err)
		}
	}
	invalid := []string{"", " leading", "trailing ", "a\nb", "a/../b", "../b", "a/..", "a/./b", "..", strings.Repeat("a", 513)}
	for _, key := range invalid {
		if err := ValidateKVKey(key); err == nil {
			t.Errorf("ValidateKVKey(%q) succeeded; want error", key)
		}
	}
}

func TestValidateKVTTLSeconds(t *testing.T) {
	if err := ValidateKVTTLSeconds(nil); err != nil {
		t.Fatalf("nil TTL: %v", err)
	}
	for _, ttl := range []int64{1, 31536000} {
		if err := ValidateKVTTLSeconds(&ttl); err != nil {
			t.Errorf("TTL %d: %v", ttl, err)
		}
	}
	for _, ttl := range []int64{0, -1, 31536001} {
		if err := ValidateKVTTLSeconds(&ttl); err == nil {
			t.Errorf("TTL %d succeeded; want error", ttl)
		}
	}
}
