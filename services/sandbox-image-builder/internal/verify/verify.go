package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

func DigestMatches(bytes []byte, expected string) error {
	d := sha256.Sum256(bytes)
	if hex.EncodeToString(d[:]) != expected {
		return errors.New("artifact digest mismatch")
	}
	return nil
}
