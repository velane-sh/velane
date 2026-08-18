package snapshot

import (
	"context"
	"fmt"
	"io"
)

// MultipartStore is deliberately cloud-neutral. The host only receives scoped
// upload capabilities; it does not require broad object-store credentials.
type MultipartStore interface {
	PutPart(ctx context.Context, artifactRef, objectVersion string, part int, body io.Reader, size int64) (ciphertextSHA256 string, err error)
	Complete(ctx context.Context, artifactRef, objectVersion string, parts int) error
}

type ChunkUpload struct {
	ArtifactRef, ObjectVersion string
	Part                       int
	Ciphertext                 []byte
	ExpectedSHA256             string
}

func UploadChunks(ctx context.Context, store MultipartStore, chunks []ChunkUpload) error {
	for _, chunk := range chunks {
		if chunk.ArtifactRef == "" || chunk.ObjectVersion == "" || chunk.Part < 1 {
			return fmt.Errorf("invalid scoped upload plan")
		}
		got, err := store.PutPart(ctx, chunk.ArtifactRef, chunk.ObjectVersion, chunk.Part, bytesReader(chunk.Ciphertext), int64(len(chunk.Ciphertext)))
		if err != nil {
			return err
		}
		if got != chunk.ExpectedSHA256 {
			return fmt.Errorf("uploaded ciphertext checksum mismatch")
		}
	}
	return nil
}

type byteReader []byte

func bytesReader(b []byte) *byteReader { v := byteReader(b); return &v }
func (b *byteReader) Read(p []byte) (int, error) {
	if len(*b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, *b)
	*b = (*b)[n:]
	return n, nil
}
