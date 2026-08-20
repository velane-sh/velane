package recipe

import (
	"encoding/json"
	"testing"

	"github.com/abskrj/velane/services/sandbox-image-builder/internal/inputs"
)

func TestRecipeRequiresLockedInputs(t *testing.T) {
	s := SpecV1{
		SchemaVersion:     "1",
		Platform:          "linux",
		Architecture:      "amd64",
		BaseImage:         "registry.example/base@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ProfileVersionIDs: []string{"p1"},
		GuestProtocol:     "v1",
		InstallGroups: []InstallGroup{{
			RepositorySnapshot: "snapshot-1",
			IndexDigest:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			LockDigest:         "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			Packages:           []Package{{Name: "x", Version: "1.0.0", Digest: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"}},
		}},
		Bootstrap:      &Bootstrap{Script: "true", TimeoutSeconds: 1},
		ExternalInputs: []inputs.ExternalInput{{URL: "https://example.com/a", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: 1}},
	}
	if e := Validate(s); e != nil {
		t.Fatal(e)
	}
	s.InstallGroups[0].Packages[0].Version = ""
	if e := Validate(s); e == nil {
		t.Fatal("accepted unversioned package")
	}
}

func TestSpecV1JSONMatchesControlPlaneContract(t *testing.T) {
	spec := SpecV1{
		SchemaVersion: "1",
		Platform:      "linux",
		Architecture:  "amd64",
		BaseImage:     "registry.example/base@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Environment:   map[string]string{"LANG": "C"},
		InstallGroups: []InstallGroup{{
			RepositorySnapshot: "snapshot-1",
			IndexDigest:        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			LockDigest:         "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			Packages:           []Package{{Name: "curl", Version: "8.0.0-1", Digest: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}},
		}},
		ProfileVersionIDs: []string{"profile-v1"},
		Bootstrap:         &Bootstrap{Script: "true", TimeoutSeconds: 30},
		ExternalInputs:    []inputs.ExternalInput{{URL: "https://example.com/input", SHA256: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", Size: 1}},
		GuestProtocol:     "v1",
	}

	got, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"schema_version":"1","platform":"linux","architecture":"amd64","base_image":"registry.example/base@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","environment":{"LANG":"C"},"install_groups":[{"repository_snapshot":"snapshot-1","index_digest":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","lock_digest":"cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","packages":[{"name":"curl","version":"8.0.0-1","digest":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}]}],"profile_version_ids":["profile-v1"],"bootstrap":{"script":"true","timeout_seconds":30},"external_inputs":[{"url":"https://example.com/input","sha256":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee","size":1}],"guest_protocol":"v1"}`
	if string(got) != want {
		t.Fatalf("recipe schema drifted\n got: %s\nwant: %s", got, want)
	}
}

func TestDecodeRejectsTrailingDocument(t *testing.T) {
	const document = `{"schema_version":"1","platform":"linux","architecture":"amd64","base_image":"registry.example/base@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","profile_version_ids":["p1"],"guest_protocol":"v1"}`
	if _, err := Decode([]byte(document + ` {}`)); err == nil {
		t.Fatal("accepted a second recipe document")
	}
}

func TestValidateRejectsMalformedDigestPinning(t *testing.T) {
	spec := SpecV1{SchemaVersion: "1", Platform: "linux", Architecture: "amd64", BaseImage: "registry.example/base@sha256:not-a-digest", ProfileVersionIDs: []string{"p1"}, GuestProtocol: "v1"}
	if err := Validate(spec); err == nil {
		t.Fatal("accepted malformed base digest")
	}
}
