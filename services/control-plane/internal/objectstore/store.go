package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

var ErrNotFound = errors.New("object not found")

// Store is the provider-neutral storage used for immutable workflow versions
// and invocation payloads. Keys are opaque outside the persistence layer.
type Store interface {
	Put(ctx context.Context, key, contentType, contentEncoding string, body []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
}

type Config struct {
	Driver                string
	Bucket                string
	Prefix                string
	S3Region              string
	S3Endpoint            string
	S3ForcePathStyle      bool
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
