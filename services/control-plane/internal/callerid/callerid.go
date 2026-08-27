// Package callerid mints and verifies the short-lived, integrity-protected
// token that carries the invoking principal's identity into a sandbox.
//
// The /v1/proxy trust boundary is the X-Velane-Tenant header, so snippet code
// could otherwise claim any group membership. The token is signed with the
// control plane's encryption key and verified on every proxy call, which makes
// the caller's groups unforgeable from inside the sandbox.
package callerid

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// EnvVar is the sandbox environment variable holding the signed token.
const EnvVar = "VELANE_CALLER_TOKEN"

// Header is the HTTP header the platform libraries forward it on.
const Header = "X-Velane-Caller"

// DefaultTTL bounds how long a minted token stays usable. It must outlive the
// longest invocation but stays short so a leaked token expires quickly.
const DefaultTTL = time.Hour

// Claims is the caller identity carried by the token.
type Claims struct {
	TenantID  string   `json:"tid"`
	UserID    string   `json:"uid,omitempty"`
	Role      string   `json:"role,omitempty"`
	GroupIDs  []string `json:"groups,omitempty"`
	ExpiresAt int64    `json:"exp"`
}

// Sign encodes and signs the claims with an expiry of now+ttl.
func Sign(key []byte, claims Claims, ttl time.Duration) (string, error) {
	if len(key) == 0 {
		return "", fmt.Errorf("callerid: signing key is empty")
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	claims.ExpiresAt = time.Now().Add(ttl).Unix()

	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("callerid: marshal claims: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + base64.RawURLEncoding.EncodeToString(sign(key, encoded)), nil
}

// Verify checks the signature and expiry and returns the claims.
func Verify(key []byte, token string) (*Claims, error) {
	if len(key) == 0 {
		return nil, fmt.Errorf("callerid: verification key is empty")
	}
	encoded, sig, ok := strings.Cut(token, ".")
	if !ok || encoded == "" || sig == "" {
		return nil, fmt.Errorf("callerid: malformed token")
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return nil, fmt.Errorf("callerid: malformed signature")
	}
	if !hmac.Equal(got, sign(key, encoded)) {
		return nil, fmt.Errorf("callerid: signature mismatch")
	}

	payload, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("callerid: malformed payload")
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("callerid: decode claims: %w", err)
	}
	if claims.ExpiresAt > 0 && time.Now().After(time.Unix(claims.ExpiresAt, 0)) {
		return nil, fmt.Errorf("callerid: token has expired")
	}
	return &claims, nil
}

func sign(key []byte, encoded string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(encoded))
	return mac.Sum(nil)
}
