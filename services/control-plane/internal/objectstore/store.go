package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

var ErrNotFound = errors.New("object not found")

// Store is the provider-neutral storage used for immutable workflow versions
// and invocation payloads. Keys are opaque outside the persistence layer.
type Store interface {
	Put(ctx context.Context, key, contentType, contentEncoding string, body []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
}

// SnapshotStore only exposes scoped multipart capabilities to snapshot hosts.
// Broad object-store credentials never leave the control plane.
type SnapshotStore interface {
	BeginSnapshotMultipart(context.Context, string, []SnapshotPartExpectation, time.Duration) (SnapshotMultipartUpload, error)
	CompleteSnapshotMultipart(context.Context, SnapshotMultipartUpload, []SnapshotCompletedPart) (SnapshotObject, error)
	AbortSnapshotMultipart(context.Context, SnapshotMultipartUpload) error
	HeadSnapshotObject(context.Context, string, string) (SnapshotObject, error)
}

type SnapshotPartExpectation struct {
	Number         int
	Size           int64
	ChecksumSHA256 string // base64-encoded SHA-256, as required by S3.
}

type SnapshotPartURL struct {
	Number         int       `json:"number"`
	URL            string    `json:"url"`
	ChecksumSHA256 string    `json:"checksum_sha256"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type SnapshotMultipartUpload struct {
	ObjectRef string            `json:"object_ref"`
	UploadID  string            `json:"upload_id"`
	Parts     []SnapshotPartURL `json:"parts"`
}

type SnapshotCompletedPart struct {
	Number         int    `json:"number"`
	ETag           string `json:"etag"`
	ChecksumSHA256 string `json:"checksum_sha256"`
}

type SnapshotObject struct {
	ObjectRef      string
	Version        string
	Size           int64
	ETag           string
	ChecksumSHA256 string
}

type Config struct {
	Driver                string
	Bucket                string
	Prefix                string
	S3Region              string
	S3Endpoint            string
	S3ForcePathStyle      bool
	S3KMSKeyID            string
	AzureAccountURL       string
	AzureConnectionString string
}

func New(ctx context.Context, cfg Config) (Store, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Driver)) {
	case "s3":
		return NewS3(ctx, cfg)
	case "azure":
		return NewAzure(cfg)
	case "":
		return nil, errors.New("OBJECT_STORAGE_DRIVER is required")
	default:
		return nil, fmt.Errorf("unsupported object storage driver %q", cfg.Driver)
	}
}

func key(prefix, name string) string {
	return strings.Trim(strings.Trim(prefix, "/")+"/"+strings.TrimLeft(name, "/"), "/")
}

func readAll(body io.ReadCloser) ([]byte, error) {
	defer body.Close()
	return io.ReadAll(body)
}
