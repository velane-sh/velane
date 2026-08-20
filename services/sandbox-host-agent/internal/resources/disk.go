package resources

import (
	"fmt"
	"os"
	"path/filepath"
)

// CreateRawSparse creates only raw, fixed-size mutable drives. Firecracker is
// never given qcow2 or a path outside the per-sandbox disk directory.
func CreateRawSparse(root, sandboxID, driveID string, size int64) (string, error) {
	if root == "" || sandboxID == "" || driveID == "" || size <= 0 || filepath.Base(driveID) != driveID {
		return "", fmt.Errorf("invalid mutable raw disk request")
	}
	path := filepath.Join(root, sandboxID, driveID+".raw")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := f.Truncate(size); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	if err := f.Sync(); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

type RawDiskDriver struct{}

func (RawDiskDriver) Create(root, sandboxID, driveID string, size int64) (string, error) {
	return CreateRawSparse(root, sandboxID, driveID, size)
}
func (RawDiskDriver) Remove(path string) error {
	if path == "" {
		return nil
	}
	return os.Remove(path)
}

func CopyStable(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := out.ReadFrom(in); err != nil {
		_ = out.Close()
		_ = os.Remove(destination)
		return err
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(destination)
		return err
	}
	return out.Close()
}
