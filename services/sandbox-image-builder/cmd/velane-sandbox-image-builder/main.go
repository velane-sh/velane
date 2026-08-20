package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/abskrj/velane/services/sandbox-image-builder/internal/build"
	"github.com/abskrj/velane/services/sandbox-image-builder/internal/inputs"
	"github.com/abskrj/velane/services/sandbox-image-builder/internal/recipe"
)

// The builder is a one-shot privileged worker. A missing signing key, pinned
// inputs, or isolation tooling is a hard failure; it never emits placeholder
// artifacts that could later be treated as ready.
func main() {
	recipePath := os.Getenv("VELANE_SANDBOX_IMAGE_RECIPE_JSON")
	data, err := os.ReadFile(recipePath)
	if recipePath == "" || err != nil {
		log.Printf("configuration error: readable VELANE_SANDBOX_IMAGE_RECIPE_JSON is required")
		os.Exit(2)
	}
	spec, err := recipe.Decode(data)
	if err != nil {
		log.Printf("invalid immutable recipe: %v", err)
		os.Exit(2)
	}
	key, err := signingKey(os.Getenv("VELANE_SANDBOX_IMAGE_SIGNING_PRIVATE_KEY_BASE64"))
	if err != nil {
		log.Printf("configuration error: %v", err)
		os.Exit(2)
	}
	output := os.Getenv("VELANE_SANDBOX_IMAGE_OUTPUT_DIR")
	if output == "" {
		output = filepath.Join(os.TempDir(), "velane-sandbox-image-output")
	}
	artifactDir := os.Getenv("VELANE_SANDBOX_IMAGE_ARTIFACT_DIR")
	if artifactDir == "" {
		log.Printf("configuration error: VELANE_SANDBOX_IMAGE_ARTIFACT_DIR is required")
		os.Exit(2)
	}
	tools := build.LinuxTools{PackageInstallerCommand: os.Getenv("VELANE_SANDBOX_IMAGE_PACKAGE_INSTALLER")}
	result, err := build.Run(context.Background(), spec, build.Config{
		BaseRootfsDir: os.Getenv("VELANE_SANDBOX_IMAGE_BASE_ROOTFS_DIR"),
		GuestKernel:   os.Getenv("VELANE_SANDBOX_IMAGE_GUEST_KERNEL"),
		GuestInit:     os.Getenv("VELANE_SANDBOX_IMAGE_GUEST_INIT"),
		OutputDir:     output,
		SigningKey:    key,
		Fetcher:       inputs.SafeHTTPFetcher{},
		Installer:     tools,
		Bootstrap:     tools,
		Filesystem:    tools,
		Uploader:      build.DirectoryUploader{Root: artifactDir},
	})
	if err != nil {
		log.Printf("sandbox image build failed: %v", err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(struct {
		Manifest       build.Manifest         `json:"manifest"`
		ManifestObject build.ObjectDescriptor `json:"manifest_object"`
	}{Manifest: result.Manifest, ManifestObject: result.ManifestObject}); err != nil {
		log.Printf("write result: %v", err)
		os.Exit(1)
	}
}

func signingKey(value string) (ed25519.PrivateKey, error) {
	key, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(key) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("VELANE_SANDBOX_IMAGE_SIGNING_PRIVATE_KEY_BASE64 must be a base64 Ed25519 private key")
	}
	return ed25519.PrivateKey(key), nil
}
