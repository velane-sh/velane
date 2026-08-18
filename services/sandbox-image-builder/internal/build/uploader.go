package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// DirectoryUploader is a production one-shot sink for deployments that stage
// immutable artifacts into a separately published directory. It uses
// content-addressed paths, fsync, and an atomic rename, then re-opens the final
// object and verifies its size and digest before reporting success.
type DirectoryUploader struct {
	Root string
}

func (u DirectoryUploader) Upload(ctx context.Context, _ string, source io.Reader, size int64, digest string) (ObjectDescriptor, error) {
	if u.Root == "" || size <= 0 || len(digest) != sha256.Size*2 {
		return ObjectDescriptor{}, fmt.Errorf("invalid directory upload configuration")
	}
	if err := ctx.Err(); err != nil {
		return ObjectDescriptor{}, err
	}
	directory := filepath.Join(filepath.Clean(u.Root), "sha256", digest[:2])
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return ObjectDescriptor{}, err
	}
	final := filepath.Join(directory, digest)
	temporary, err := os.CreateTemp(directory, ".upload-*")
	if err != nil {
		return ObjectDescriptor{}, err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)

	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(source, size+1))
	if copyErr != nil {
		temporary.Close()
		return ObjectDescriptor{}, copyErr
	}
	if written != size || hex.EncodeToString(hash.Sum(nil)) != digest {
		temporary.Close()
		return ObjectDescriptor{}, fmt.Errorf("directory upload content differs from descriptor")
	}
	if err := temporary.Chmod(0o440); err != nil {
		temporary.Close()
		return ObjectDescriptor{}, err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return ObjectDescriptor{}, err
	}
	if err := temporary.Close(); err != nil {
		return ObjectDescriptor{}, err
	}
	if err := os.Rename(temporaryName, final); err != nil {
		return ObjectDescriptor{}, err
	}
	if err := syncDirectory(directory); err != nil {
		return ObjectDescriptor{}, err
	}
	if err := verifyObject(final, size, digest); err != nil {
		return ObjectDescriptor{}, err
	}
	return ObjectDescriptor{Ref: "sha256/" + digest[:2] + "/" + digest, Version: digest, SHA256: digest, Size: size}, nil
}

func verifyObject(path string, size int64, digest string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() != size || !info.Mode().IsRegular() {
		return fmt.Errorf("published object size or type mismatch")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != digest {
		return fmt.Errorf("published object digest mismatch")
	}
	return nil
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
