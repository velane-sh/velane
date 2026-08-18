// Package artifacts deliberately handles streaming sandbox snapshot bytes; it does not reuse objectstore.Store.
package artifacts

import (
	"context"
	"io"
	"time"
)

type UploadPart struct {
	Number    int
	URL       string
	ExpiresAt time.Time
}
type UploadPlan struct {
	UploadID string
	Parts    []UploadPart
}
type CompletedPart struct {
	Number   int
	Checksum string
	Size     int64
}
type Object struct {
	Ref, Version, SHA256 string
	Size                 int64
}
type Store interface {
	BeginMultipart(context.Context, string, int64) (UploadPlan, error)
	RenewPartURLs(context.Context, string, []int) (UploadPlan, error)
	CompleteMultipart(context.Context, string, []CompletedPart) (Object, error)
	AbortMultipart(context.Context, string) error
	Open(context.Context, Object) (io.ReadCloser, error)
}
