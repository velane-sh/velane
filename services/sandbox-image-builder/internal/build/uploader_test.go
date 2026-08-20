package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectoryUploaderPublishesVerifiedContentAddressedObject(t *testing.T) {
	content := "artifact"
	digestBytes := sha256.Sum256([]byte(content))
	digest := hex.EncodeToString(digestBytes[:])
	uploader := DirectoryUploader{Root: t.TempDir()}

	descriptor, err := uploader.Upload(context.Background(), "ignored", strings.NewReader(content), int64(len(content)), digest)
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.Ref != "sha256/"+digest[:2]+"/"+digest || descriptor.Version != digest {
		t.Fatalf("unexpected immutable descriptor: %#v", descriptor)
	}
	published, err := os.ReadFile(filepath.Join(uploader.Root, filepath.FromSlash(descriptor.Ref)))
	if err != nil {
		t.Fatal(err)
	}
	if string(published) != content {
		t.Fatal("published object content differs")
	}
}

func TestDirectoryUploaderRejectsMismatchedContent(t *testing.T) {
	uploader := DirectoryUploader{Root: t.TempDir()}
	if _, err := uploader.Upload(context.Background(), "ignored", strings.NewReader("artifact"), 8, strings.Repeat("a", 64)); err == nil {
		t.Fatal("accepted mismatched content digest")
	}
}
