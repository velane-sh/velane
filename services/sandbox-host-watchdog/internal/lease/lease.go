// Package lease validates signed, control-plane-issued connectivity grants.
package lease

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var ErrInvalidGrant = errors.New("invalid watchdog lease grant")

type Grant struct {
	SandboxID          string `json:"sandbox_id"`
	AllocationID       string `json:"allocation_id"`
	HostID             string `json:"host_id"`
	HostIncarnation    uint64 `json:"host_incarnation"`
	FenceEpoch         uint64 `json:"fence_epoch"`
	IssuedAtUnixMilli  int64  `json:"issued_at_unix_milli"`
	ExpiresAtUnixMilli int64  `json:"expires_at_unix_milli"`
	Signature          []byte `json:"signature"`
}
type signedGrant struct {
	SandboxID          string `json:"sandbox_id"`
	AllocationID       string `json:"allocation_id"`
	HostID             string `json:"host_id"`
	HostIncarnation    uint64 `json:"host_incarnation"`
	FenceEpoch         uint64 `json:"fence_epoch"`
	IssuedAtUnixMilli  int64  `json:"issued_at_unix_milli"`
	ExpiresAtUnixMilli int64  `json:"expires_at_unix_milli"`
}

func (g Grant) payload() ([]byte, error) {
	return json.Marshal(signedGrant{g.SandboxID, g.AllocationID, g.HostID, g.HostIncarnation, g.FenceEpoch, g.IssuedAtUnixMilli, g.ExpiresAtUnixMilli})
}
func Sign(g Grant, privateKey ed25519.PrivateKey) (Grant, error) {
	b, e := g.payload()
	if e != nil {
		return Grant{}, e
	}
	g.Signature = ed25519.Sign(privateKey, b)
	return g, nil
}
func Validate(g Grant, publicKey ed25519.PublicKey, now time.Time, maxTTL time.Duration) error {
	if g.SandboxID == "" || g.AllocationID == "" || g.HostID == "" || g.HostIncarnation == 0 || g.FenceEpoch == 0 || len(g.Signature) == 0 {
		return ErrInvalidGrant
	}
	b, e := g.payload()
	if e != nil || !ed25519.Verify(publicKey, b, g.Signature) {
		return ErrInvalidGrant
	}
	issued := time.UnixMilli(g.IssuedAtUnixMilli)
	expires := time.UnixMilli(g.ExpiresAtUnixMilli)
	if !expires.After(now) || issued.After(now.Add(time.Minute)) || expires.Sub(issued) <= 0 || expires.Sub(issued) > maxTTL {
		return fmt.Errorf("%w: invalid deadline", ErrInvalidGrant)
	}
	return nil
}
