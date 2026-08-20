package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
)

// ArtifactReader yields exactly the encrypted chunks declared by the manifest.
type ArtifactReader interface {
	Open(context.Context, ChunkDescriptor) (io.ReadCloser, error)
}

func VerifyRestoreBundle(ctx context.Context, m SnapshotManifestV1, drives []DriveDescriptor, reader ArtifactReader, key []byte, associatedData func(SnapshotArtifact, ChunkDescriptor) []byte) error {
	if err := ValidateFullSnapshotManifest(m, drives); err != nil {
		return err
	}
	if reader == nil || associatedData == nil {
		return errors.New("restore reader and associated-data builder are required")
	}
	for _, a := range m.Artifacts {
		hash := sha256.New()
		var logicalSize int64
		for _, chunk := range a.Chunks {
			r, err := reader.Open(ctx, chunk)
			if err != nil {
				return err
			}
			ciphertext, readErr := io.ReadAll(io.LimitReader(r, chunk.CiphertextSize+1))
			closeErr := r.Close()
			if readErr != nil {
				return readErr
			}
			if closeErr != nil {
				return closeErr
			}
			if int64(len(ciphertext)) != chunk.CiphertextSize {
				return errors.New("ciphertext size mismatch")
			}
			ciphertextDigest := sha256.Sum256(ciphertext)
			if hex.EncodeToString(ciphertextDigest[:]) != chunk.CiphertextSHA256 {
				return errors.New("ciphertext checksum mismatch")
			}
			plaintext, err := DecryptChunk(key, decodeNonce(chunk.Nonce), ciphertext, associatedData(a, chunk), chunk.PlaintextSHA256)
			if err != nil {
				return err
			}
			logicalSize += int64(len(plaintext))
			_, _ = hash.Write(plaintext)
		}
		if logicalSize != a.LogicalSize || hex.EncodeToString(hash.Sum(nil)) != a.SHA256 {
			return errors.New("full artifact checksum or size mismatch")
		}
	}
	return nil
}

// Nonces are opaque transport strings in the manifest. The actual uploader uses
// hex encoding so validation remains unambiguous across object stores.
func decodeNonce(v string) []byte {
	b, _ := hex.DecodeString(v)
	return b
}
