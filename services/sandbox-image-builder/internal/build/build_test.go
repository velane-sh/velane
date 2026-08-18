package build

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abskrj/velane/services/sandbox-image-builder/internal/inputs"
	"github.com/abskrj/velane/services/sandbox-image-builder/internal/recipe"
)

type fakeFS struct{}

func (fakeFS) CopyTree(_, destination string) error { return os.MkdirAll(destination, 0o750) }
func (fakeFS) MakeExt4(_ context.Context, source, output, _ string) error {
	data, err := os.ReadFile(filepath.Join(source, "installed"))
	if err != nil {
		return err
	}
	return os.WriteFile(output, data, 0o640)
}

type fakeInstaller struct{}

func (fakeInstaller) Install(_ context.Context, root string, _ recipe.InstallGroup) error {
	return os.WriteFile(filepath.Join(root, "installed"), []byte("locked"), 0o640)
}

type fakeBootstrap struct {
	called bool
	env    map[string]string
}

func (f *fakeBootstrap) Run(_ context.Context, _, _ string, env map[string]string) error {
	f.called = true
	f.env = env
	return nil
}

type fakeFetcher struct{}
type fakeResponse struct{ io.Reader }

func (fakeResponse) Close() error { return nil }
func (fakeFetcher) Fetch(_ context.Context, input inputs.ExternalInput) (inputs.Response, error) {
	return fakeResponse{Reader: &reader{value: []byte("x")}}, nil
}

type reader struct{ value []byte }

func (r *reader) Read(p []byte) (int, error) {
	if len(r.value) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.value)
	r.value = r.value[n:]
	return n, nil
}

type fakeUploader struct{}

func (fakeUploader) Upload(_ context.Context, name string, r io.Reader, size int64, digest string) (ObjectDescriptor, error) {
	_, err := io.ReadAll(r)
	return ObjectDescriptor{Ref: "objects/" + name, Version: "v1", SHA256: digest, Size: size}, err
}

type countingFS struct{ copies int }

func (f *countingFS) CopyTree(_, destination string) error {
	f.copies++
	return os.MkdirAll(destination, 0o750)
}
func (*countingFS) MakeExt4(context.Context, string, string, string) error { return nil }

func validSpec() recipe.SpecV1 {
	return recipe.SpecV1{SchemaVersion: "1", Platform: "linux", Architecture: "amd64", BaseImage: "example.invalid/base@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ProfileVersionIDs: []string{"profile"}, GuestProtocol: "v1", InstallGroups: []recipe.InstallGroup{{RepositorySnapshot: "https://example.invalid/snapshot", IndexDigest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", LockDigest: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Packages: []recipe.Package{{Name: "pkg", Version: "1.0", Digest: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}}}}, ExternalInputs: []inputs.ExternalInput{{URL: "https://example.com/input", SHA256: "2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881", Size: 1}}, Bootstrap: &recipe.Bootstrap{Script: "true", TimeoutSeconds: 1}}
}
func TestRunBuildsSignedVerifiedArtifacts(t *testing.T) {
	dir := t.TempDir()
	kernel := filepath.Join(dir, "kernel")
	init := filepath.Join(dir, "init")
	if err := os.WriteFile(kernel, []byte("kernel"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(init, []byte("init"), 0o640); err != nil {
		t.Fatal(err)
	}
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	bootstrap := &fakeBootstrap{}
	result, err := Run(context.Background(), validSpec(), Config{BaseRootfsDir: dir, GuestKernel: kernel, GuestInit: init, OutputDir: filepath.Join(dir, "out"), SigningKey: key, Fetcher: fakeFetcher{}, Installer: fakeInstaller{}, Bootstrap: bootstrap, Filesystem: fakeFS{}, Uploader: fakeUploader{}, Now: func() time.Time { return time.Unix(0, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	if !bootstrap.called {
		t.Fatal("bootstrap was not run")
	}
	if bootstrap.env["VELANE_BUILD_INPUT_DIR"] != "/run/velane-build-inputs" {
		t.Fatal("bootstrap input path is not rooted inside the isolated rootfs")
	}
	if result.Rootfs.ObjectDescriptor.Ref == "" || result.Manifest.Signature == "" {
		t.Fatal("missing uploaded signed outputs")
	}
	if result.ManifestObject.Ref == "" || result.ManifestObject.SHA256 == "" {
		t.Fatal("missing verified uploaded manifest descriptor")
	}
	if _, err := os.Stat(result.ManifestPath); err != nil {
		t.Fatal(err)
	}
	unsigned := result.Manifest
	signature, err := base64.RawStdEncoding.DecodeString(unsigned.Signature)
	if err != nil {
		t.Fatal(err)
	}
	unsigned.Signature = ""
	payload, err := json.Marshal(unsigned)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(public, payload, signature) {
		t.Fatal("manifest signature does not verify")
	}
}

func TestRunEmitsRuntimeArtifactManifest(t *testing.T) {
	dir := t.TempDir()
	kernel := filepath.Join(dir, "kernel")
	init := filepath.Join(dir, "init")
	if err := os.WriteFile(kernel, []byte("kernel"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(init, []byte("init"), 0o640); err != nil {
		t.Fatal(err)
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	result, err := Run(context.Background(), validSpec(), Config{BaseRootfsDir: dir, GuestKernel: kernel, GuestInit: init, OutputDir: filepath.Join(dir, "out"), SigningKey: key, Fetcher: fakeFetcher{}, Installer: fakeInstaller{}, Bootstrap: &fakeBootstrap{}, Filesystem: fakeFS{}, Uploader: fakeUploader{}, Now: func() time.Time { return time.Unix(0, 0) }})
	if err != nil {
		t.Fatal(err)
	}
	if result.Manifest.Runtime.Guest.Kernel.ObjectRef != "objects/guest-kernel" || result.Manifest.Runtime.Guest.Rootfs.ObjectRef != "objects/rootfs.ext4" || result.Manifest.Runtime.Guest.Init.ObjectRef != "objects/guest-init" {
		t.Fatalf("runtime artifact manifest is incomplete: %#v", result.Manifest.Runtime)
	}
	if result.Manifest.Runtime.Guest.Rootfs.Digest != result.Rootfs.SHA256 || result.Manifest.Runtime.Guest.Rootfs.SizeBytes != result.Rootfs.Size {
		t.Fatal("runtime rootfs does not bind the built artifact")
	}
	if len(result.Manifest.Runtime.ImmutableDrives) != 1 || !result.Manifest.Runtime.ImmutableDrives[0].Root || result.Manifest.Runtime.ImmutableDrives[0].Artifact.Digest != result.Rootfs.SHA256 {
		t.Fatal("runtime manifest lacks a pinned root drive")
	}
}

func TestRunRejectsMissingSigningKeyBeforeSideEffects(t *testing.T) {
	_, err := Run(context.Background(), validSpec(), Config{BaseRootfsDir: t.TempDir(), GuestKernel: "missing", GuestInit: "missing", OutputDir: t.TempDir(), Fetcher: fakeFetcher{}, Installer: fakeInstaller{}, Bootstrap: &fakeBootstrap{}, Filesystem: fakeFS{}})
	if err == nil {
		t.Fatal("accepted missing signing key")
	}
}

func TestRunRequiresUploaderBeforeFilesystemSideEffects(t *testing.T) {
	dir := t.TempDir()
	kernel := filepath.Join(dir, "kernel")
	init := filepath.Join(dir, "init")
	if err := os.WriteFile(kernel, []byte("kernel"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(init, []byte("init"), 0o640); err != nil {
		t.Fatal(err)
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	filesystem := &countingFS{}

	_, err = Run(context.Background(), validSpec(), Config{BaseRootfsDir: dir, GuestKernel: kernel, GuestInit: init, OutputDir: filepath.Join(dir, "out"), SigningKey: key, Fetcher: fakeFetcher{}, Installer: fakeInstaller{}, Bootstrap: &fakeBootstrap{}, Filesystem: filesystem})
	if err == nil {
		t.Fatal("accepted a build without durable artifact upload")
	}
	if filesystem.copies != 0 {
		t.Fatal("performed filesystem side effects before rejecting missing uploader")
	}
}
