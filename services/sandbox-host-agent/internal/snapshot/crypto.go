package snapshot

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

type DataKeyProvider interface {
	NewDataKey(ctx Context, encryptionContext []byte) (plaintext, wrapped []byte, err error)
}
type Context interface {
	Done() <-chan struct{}
	Err() error
}

type EncryptedChunk struct {
	Ciphertext, Nonce                 []byte
	PlaintextSHA256, CiphertextSHA256 string
}

func EncryptChunk(key, plaintext, associatedData []byte) (EncryptedChunk, error) {
	if len(key) != 32 {
		return EncryptedChunk{}, errors.New("snapshot data key must be AES-256")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return EncryptedChunk{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return EncryptedChunk{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return EncryptedChunk{}, err
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, associatedData)
	p := sha256.Sum256(plaintext)
	c := sha256.Sum256(ciphertext)
	return EncryptedChunk{ciphertext, nonce, hex.EncodeToString(p[:]), hex.EncodeToString(c[:])}, nil
}
func DecryptChunk(key, nonce, ciphertext, associatedData []byte, expectedPlaintextSHA256 string) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("snapshot data key must be AES-256")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, errors.New("invalid chunk nonce")
	}
	plaintext, err := gcm.Open(nil, nonce, ciphertext, associatedData)
	if err != nil {
		return nil, fmt.Errorf("chunk authentication failed: %w", err)
	}
	d := sha256.Sum256(plaintext)
	if hex.EncodeToString(d[:]) != expectedPlaintextSHA256 {
		return nil, errors.New("chunk plaintext checksum mismatch")
	}
	return plaintext, nil
}
