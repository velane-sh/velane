// Package build implements the privileged, fail-closed image materialization pipeline.
package build

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/abskrj/velane/services/sandbox-image-builder/internal/inputs"
	"github.com/abskrj/velane/services/sandbox-image-builder/internal/recipe"
)

const schemaVersion = "1"

type PackageInstaller interface {
	Install(context.Context, string, recipe.InstallGroup) error
}

type BootstrapRunner interface {
	Run(context.Context, string, string, map[string]string) error
}

type FilesystemTool interface {
	CopyTree(source, destination string) error
	MakeExt4(ctx context.Context, sourceDir, output, uuid string) error
}

type Uploader interface {
	Upload(context.Context, string, io.Reader, int64, string) (ObjectDescriptor, error)
}

type ObjectDescriptor struct {
	Ref     string `json:"ref"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}

type Config struct {
	BaseRootfsDir string
	GuestKernel   string
	GuestInit     string
	OutputDir     string
	SigningKey    ed25519.PrivateKey
	Fetcher       inputs.Fetcher
	Installer     PackageInstaller
	Bootstrap     BootstrapRunner
	Filesystem    FilesystemTool
	Uploader      Uploader
	Now           func() time.Time
}

type Artifact struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	ObjectDescriptor
}

type RuntimeArtifact struct {
	ObjectRef string `json:"object_ref"`
	Digest    string `json:"digest"`
	SizeBytes int64  `json:"size_bytes"`
}

type RuntimeGuestArtifacts struct {
	Kernel RuntimeArtifact `json:"kernel"`
	Rootfs RuntimeArtifact `json:"rootfs"`
	Init   RuntimeArtifact `json:"init"`
}

type RuntimeManifest struct {
	Guest           RuntimeGuestArtifacts   `json:"guest"`
	ImmutableDrives []RuntimeImmutableDrive `json:"immutable_drives"`
}

type RuntimeImmutableDrive struct {
	ID        string          `json:"id"`
	Mutable   bool            `json:"mutable"`
	Root      bool            `json:"root"`
	Order     int             `json:"order"`
	SizeBytes int64           `json:"size_bytes"`
	Artifact  RuntimeArtifact `json:"artifact"`
}

type Manifest struct {
	SchemaVersion string          `json:"schema_version"`
	RecipeDigest  string          `json:"recipe_digest"`
	BuildID       string          `json:"build_id"`
	Runtime       RuntimeManifest `json:"runtime"`
	Artifacts     []Artifact      `json:"artifacts"`
	SBOM          Artifact        `json:"sbom"`
	Provenance    Artifact        `json:"provenance"`
	Signature     string          `json:"signature"`
}

type Result struct {
	Manifest       Manifest
	ManifestPath   string
	ManifestObject ObjectDescriptor
	Rootfs         Artifact
	Kernel         Artifact
	GuestInit      Artifact
}

// Run validates every input before creating a work directory or contacting a
// package installer. It never returns a ready-looking result after a partial
// failure: callers receive only an error and must mark the version failed.
func Run(ctx context.Context, spec recipe.SpecV1, cfg Config) (Result, error) {
	if err := recipe.Validate(spec); err != nil {
		return Result{}, err
	}
	if err := validateConfig(cfg); err != nil {
		return Result{}, err
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if err := verifyPinnedFile(cfg.GuestKernel, "guest kernel"); err != nil {
		return Result{}, err
	}
	if err := verifyPinnedFile(cfg.GuestInit, "guest init"); err != nil {
		return Result{}, err
	}

	recipeDigest, err := canonicalDigest(spec)
	if err != nil {
		return Result{}, err
	}
	buildID := recipeDigest[:24]
	work, err := os.MkdirTemp("", "velane-image-build-"+buildID+"-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(work)
	root := filepath.Join(work, "root")
	if err := cfg.Filesystem.CopyTree(cfg.BaseRootfsDir, root); err != nil {
		return Result{}, fmt.Errorf("materialize pinned base rootfs: %w", err)
	}

	inputDir := filepath.Join(root, "run", "velane-build-inputs")
	if _, err := fetchInputs(ctx, cfg.Fetcher, spec.ExternalInputs, inputDir); err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(inputDir)
	for _, group := range spec.InstallGroups {
		if err := cfg.Installer.Install(ctx, root, group); err != nil {
			return Result{}, fmt.Errorf("install locked packages: %w", err)
		}
	}
	if spec.Bootstrap != nil {
		bootstrapCtx, cancel := context.WithTimeout(ctx, time.Duration(spec.Bootstrap.TimeoutSeconds)*time.Second)
		err := cfg.Bootstrap.Run(bootstrapCtx, root, spec.Bootstrap.Script, map[string]string{"VELANE_BUILD_INPUT_DIR": "/run/velane-build-inputs", "SOURCE_DATE_EPOCH": "0"})
		cancel()
		if err != nil {
			return Result{}, fmt.Errorf("bounded network-disabled bootstrap: %w", err)
		}
	}
	if err := os.RemoveAll(inputDir); err != nil {
		return Result{}, fmt.Errorf("remove controlled build inputs: %w", err)
	}
	if err := normalizeTree(root); err != nil {
		return Result{}, fmt.Errorf("normalize rootfs: %w", err)
	}
	if err := os.MkdirAll(cfg.OutputDir, 0o750); err != nil {
		return Result{}, err
	}
	rootfsPath := filepath.Join(cfg.OutputDir, buildID+".rootfs.ext4")
	if err := cfg.Filesystem.MakeExt4(ctx, root, rootfsPath, deterministicUUID(recipeDigest)); err != nil {
		return Result{}, fmt.Errorf("build deterministic ext4 rootfs: %w", err)
	}

	rootfs, err := describe("rootfs.ext4", rootfsPath, cfg.Uploader, ctx)
	if err != nil {
		return Result{}, err
	}
	kernel, err := describe("guest-kernel", cfg.GuestKernel, cfg.Uploader, ctx)
	if err != nil {
		return Result{}, err
	}
	guestInit, err := describe("guest-init", cfg.GuestInit, cfg.Uploader, ctx)
	if err != nil {
		return Result{}, err
	}
	sbomPath := filepath.Join(cfg.OutputDir, buildID+".sbom.cdx.json")
	sbom := cycloneDX(spec, []Artifact{rootfs, kernel, guestInit})
	if err := writeCanonical(sbomPath, sbom); err != nil {
		return Result{}, err
	}
	sbomArtifact, err := describe("sbom.cyclonedx", sbomPath, cfg.Uploader, ctx)
	if err != nil {
		return Result{}, err
	}
	provenancePath := filepath.Join(cfg.OutputDir, buildID+".provenance.json")
	provenance := inTotoProvenance(recipeDigest, []Artifact{rootfs, kernel, guestInit, sbomArtifact}, cfg.Now().UTC())
	if err := writeCanonical(provenancePath, provenance); err != nil {
		return Result{}, err
	}
	provenanceArtifact, err := describe("provenance.intoto", provenancePath, cfg.Uploader, ctx)
	if err != nil {
		return Result{}, err
	}

	manifest := Manifest{SchemaVersion: schemaVersion, RecipeDigest: recipeDigest, BuildID: buildID, Runtime: runtimeManifest(rootfs, kernel, guestInit), Artifacts: []Artifact{rootfs, kernel, guestInit}, SBOM: sbomArtifact, Provenance: provenanceArtifact}
	unsigned, err := unsignedManifestJSON(manifest)
	if err != nil {
		return Result{}, err
	}
	manifest.Signature = base64.RawStdEncoding.EncodeToString(ed25519.Sign(cfg.SigningKey, unsigned))
	manifestPath := filepath.Join(cfg.OutputDir, buildID+".manifest.json")
	if err := writeCanonical(manifestPath, manifest); err != nil {
		return Result{}, err
	}
	manifestObject, err := describe("manifest", manifestPath, cfg.Uploader, ctx)
	if err != nil {
		return Result{}, err
	}
	return Result{Manifest: manifest, ManifestPath: manifestPath, ManifestObject: manifestObject.ObjectDescriptor, Rootfs: rootfs, Kernel: kernel, GuestInit: guestInit}, nil
}

func validateConfig(c Config) error {
	if c.BaseRootfsDir == "" || c.GuestKernel == "" || c.GuestInit == "" || c.OutputDir == "" {
		return errors.New("pinned base rootfs, kernel, guest init, and output directory are required")
	}
	if len(c.SigningKey) != ed25519.PrivateKeySize {
		return errors.New("configured Ed25519 signing key is required")
	}
	if c.Fetcher == nil || c.Installer == nil || c.Bootstrap == nil || c.Filesystem == nil || c.Uploader == nil {
		return errors.New("privileged fetch, package, bootstrap, filesystem, and upload tools are required")
	}
	return nil
}
func verifyPinnedFile(path, name string) error {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("%s is unavailable", name)
	}
	return nil
}
func fetchInputs(ctx context.Context, fetcher inputs.Fetcher, declared []inputs.ExternalInput, destination string) (string, error) {
	if err := os.MkdirAll(destination, 0o750); err != nil {
		return "", err
	}
	for i, input := range declared {
		data, err := inputs.FetchExact(ctx, fetcher, input, 10<<20)
		if err != nil {
			return "", fmt.Errorf("fetch declared input %d: %w", i, err)
		}
		path := filepath.Join(destination, fmt.Sprintf("%03d-%s", i, input.SHA256))
		if err := os.WriteFile(path, data, 0o444); err != nil {
			return "", err
		}
	}
	return destination, nil
}
func describe(name, path string, uploader Uploader, ctx context.Context) (Artifact, error) {
	file, err := os.Open(path)
	if err != nil {
		return Artifact{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Artifact{}, err
	}
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return Artifact{}, err
	}
	digest := hex.EncodeToString(h.Sum(nil))
	artifact := Artifact{Name: name, SHA256: digest, Size: info.Size()}
	if uploader != nil {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return Artifact{}, err
		}
		object, err := uploader.Upload(ctx, name, file, info.Size(), digest)
		if err != nil {
			return Artifact{}, err
		}
		if object.SHA256 != digest || object.Size != info.Size() || object.Ref == "" {
			return Artifact{}, errors.New("post-upload artifact verification failed")
		}
		artifact.ObjectDescriptor = object
	}
	return artifact, nil
}
func runtimeManifest(rootfs, kernel, guestInit Artifact) RuntimeManifest {
	rootArtifact := runtimeArtifact(rootfs)
	return RuntimeManifest{
		Guest:           RuntimeGuestArtifacts{Kernel: runtimeArtifact(kernel), Rootfs: rootArtifact, Init: runtimeArtifact(guestInit)},
		ImmutableDrives: []RuntimeImmutableDrive{{ID: "root", Root: true, Order: 0, SizeBytes: rootfs.Size, Artifact: rootArtifact}},
	}
}
func runtimeArtifact(artifact Artifact) RuntimeArtifact {
	return RuntimeArtifact{ObjectRef: artifact.Ref, Digest: artifact.SHA256, SizeBytes: artifact.Size}
}
func canonicalDigest(value any) (string, error) {
	b, err := canonicalJSON(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}
func canonicalJSON(value any) ([]byte, error) { return json.Marshal(value) }
func unsignedManifestJSON(manifest Manifest) ([]byte, error) {
	manifest.Signature = ""
	return canonicalJSON(manifest)
}
func writeCanonical(path string, value any) error {
	b, err := canonicalJSON(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o640)
}
func deterministicUUID(digest string) string {
	return fmt.Sprintf("%s-%s-4%s-8%s-%s", digest[:8], digest[8:12], digest[13:16], digest[17:20], digest[20:32])
}
func normalizeTree(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		return os.Chtimes(path, time.Unix(0, 0), time.Unix(0, 0))
	})
}
func cycloneDX(spec recipe.SpecV1, artifacts []Artifact) map[string]any {
	components := make([]map[string]any, 0, len(spec.InstallGroups)+len(artifacts))
	for _, group := range spec.InstallGroups {
		for _, p := range group.Packages {
			components = append(components, map[string]any{"type": "library", "name": p.Name, "version": p.Version, "hashes": []map[string]string{{"alg": "SHA-256", "content": p.Digest}}, "properties": []map[string]string{{"name": "velane.repository_snapshot", "value": group.RepositorySnapshot}, {"name": "velane.index_digest", "value": group.IndexDigest}, {"name": "velane.lock_digest", "value": group.LockDigest}}})
		}
	}
	for _, artifact := range artifacts {
		components = append(components, map[string]any{"type": "file", "name": artifact.Name, "hashes": []map[string]string{{"alg": "SHA-256", "content": artifact.SHA256}}})
	}
	sort.Slice(components, func(i, j int) bool { return components[i]["name"].(string) < components[j]["name"].(string) })
	return map[string]any{"bomFormat": "CycloneDX", "specVersion": "1.5", "serialNumber": "urn:uuid:" + deterministicUUID(mustDigest(spec)), "version": 1, "metadata": map[string]any{"component": map[string]string{"type": "container", "name": "velane-sandbox-image", "version": mustDigest(spec)}}, "components": components}
}
func mustDigest(value any) string { digest, _ := canonicalDigest(value); return digest }
func inTotoProvenance(recipeDigest string, artifacts []Artifact, at time.Time) map[string]any {
	subjects := make([]map[string]any, 0, len(artifacts))
	for _, artifact := range artifacts {
		subjects = append(subjects, map[string]any{"name": artifact.Name, "digest": map[string]string{"sha256": artifact.SHA256}})
	}
	return map[string]any{"_type": "https://in-toto.io/Statement/v1", "subject": subjects, "predicateType": "https://slsa.dev/provenance/v1", "predicate": map[string]any{"buildDefinition": map[string]any{"buildType": "https://velane.dev/sandbox-image-builder/v1", "externalParameters": map[string]string{"recipe_digest": recipeDigest}}, "runDetails": map[string]any{"builder": map[string]string{"id": "velane-sandbox-image-builder"}, "metadata": map[string]string{"invocationId": recipeDigest, "startedOn": at.Format(time.RFC3339)}}}}
}

// Ensure strings is retained in the public package API documentation generated by Go.
var _ = strings.Builder{}
