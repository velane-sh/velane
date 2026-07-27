package postgres

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

func TestInvocationPayloadRoundTrip(t *testing.T) {
	original := invocationPayloadObject{
		FormatVersion: 1,
		InvocationID:  "inv-1",
		TenantID:      "tenant-1",
		WorkflowID:    "workflow-1",
		VersionID:     "version-1",
		Input:         `{"prompt":"hello"}`,
		Output:        `{"ok":true}`,
		Error:         "",
		Stderr:        "debug line",
	}

	encoded, err := encodeInvocationPayload(original)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	decoded, err := decodeInvocationPayload(encoded)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if *decoded != original {
		t.Fatalf("round trip mismatch: got %#v want %#v", *decoded, original)
	}
}

func TestInvocationPayloadChecksumCompatibility(t *testing.T) {
	document := invocationPayloadObject{
		FormatVersion: 1,
		InvocationID:  "inv-1",
		TenantID:      "tenant-1",
		WorkflowID:    "workflow-1",
		VersionID:     "version-1",
		Input:         `{"prompt":"hello"}`,
	}
	encoded, err := encodeInvocationPayload(document)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	raw, err := decompressInvocationPayload(encoded)
	if err != nil {
		t.Fatalf("decompress payload: %v", err)
	}

	legacySum := sha256.Sum256(encoded)
	legacyChecksum := hex.EncodeToString(legacySum[:])
	logicalChecksum, err := logicalInvocationPayloadChecksum(encoded)
	if err != nil {
		t.Fatalf("logical checksum: %v", err)
	}

	for _, test := range []struct {
		name     string
		body     []byte
		checksum string
	}{
		{name: "legacy compressed", body: encoded, checksum: legacyChecksum},
		{name: "legacy transparently decoded", body: raw, checksum: legacyChecksum},
		{name: "logical compressed", body: encoded, checksum: logicalChecksum},
		{name: "logical transparently decoded", body: raw, checksum: logicalChecksum},
	} {
		t.Run(test.name, func(t *testing.T) {
			if !invocationPayloadChecksumMatches(test.body, test.checksum) {
				t.Fatal("checksum did not match")
			}
			decoded, err := decodeInvocationPayload(test.body)
			if err != nil {
				t.Fatalf("decode payload: %v", err)
			}
			if *decoded != document {
				t.Fatalf("decoded payload = %#v; want %#v", *decoded, document)
			}
		})
	}
}

func TestInvocationObjectKeyIsTenantScoped(t *testing.T) {
	createdAt := time.Date(2026, time.July, 24, 1, 2, 3, 0, time.UTC)
	got := invocationObjectKey("tenant-1", "workflow-1", "inv-1", createdAt)
	want := "tenants/tenant-1/invocations/workflow-1/2026/07/24/inv-1.json.gz"
	if got != want {
		t.Fatalf("key = %q; want %q", got, want)
	}
}
