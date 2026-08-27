package callerid_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/callerid"
)

var key = []byte("0123456789abcdef0123456789abcdef")

func TestSignVerifyRoundTrip(t *testing.T) {
	token, err := callerid.Sign(key, callerid.Claims{
		TenantID: "tenant-1",
		UserID:   "user-1",
		Role:     "member",
		GroupIDs: []string{"group-1", "group-2"},
	}, time.Minute)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	claims, err := callerid.Verify(key, token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.TenantID != "tenant-1" || claims.UserID != "user-1" || claims.Role != "member" {
		t.Errorf("claims = %+v; want the signed identity", claims)
	}
	if len(claims.GroupIDs) != 2 || claims.GroupIDs[0] != "group-1" {
		t.Errorf("GroupIDs = %v; want the signed groups", claims.GroupIDs)
	}
	if claims.ExpiresAt <= time.Now().Unix() {
		t.Errorf("ExpiresAt = %d; want a future expiry", claims.ExpiresAt)
	}
}

func TestVerifyRejectsTamperedAndForeignTokens(t *testing.T) {
	token, err := callerid.Sign(key, callerid.Claims{TenantID: "tenant-1", GroupIDs: []string{"group-1"}}, time.Minute)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	forged, err := callerid.Sign([]byte("ffffffffffffffffffffffffffffffff"), callerid.Claims{
		TenantID: "tenant-1",
		GroupIDs: []string{"group-1"},
	}, time.Minute)
	if err != nil {
		t.Fatalf("Sign with other key: %v", err)
	}

	cases := map[string]string{
		"empty":            "",
		"malformed":        "not-a-token",
		"tampered payload": "eyJ0aWQiOiJ0ZW5hbnQtMSJ9." + token[len(token)-43:],
		"foreign key":      forged,
	}
	for name, tok := range cases {
		if _, err := callerid.Verify(key, tok); err == nil {
			t.Errorf("Verify(%s) = nil error; want rejection", name)
		}
	}
}

func TestVerifyRejectsExpiredToken(t *testing.T) {
	payload, err := json.Marshal(callerid.Claims{
		TenantID:  "tenant-1",
		ExpiresAt: time.Now().Add(-time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(encoded))
	token := encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	if _, err := callerid.Verify(key, token); err == nil {
		t.Error("Verify(expired) = nil error; want rejection")
	}
}
