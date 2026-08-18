package domain

import (
	"encoding/json"
	"testing"
)

func TestRecipeSpecV1JSONMatchesBuilderContract(t *testing.T) {
	spec := RecipeSpecV1{
		SchemaVersion: "1",
		Platform:      "linux",
		Architecture:  "amd64",
		BaseImage:     "registry.example/base@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Environment:   map[string]string{"LANG": "C"},
		InstallGroups: []InstallGroup{{
			RepositorySnapshot: "snapshot-1",
			IndexDigest:        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			LockDigest:         "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
			Packages:           []PackageSpec{{Name: "curl", Version: "8.0.0-1", Digest: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}},
		}},
		ProfileVersionIDs: []string{"profile-v1"},
		Bootstrap:         &BootstrapSpec{Script: "true", TimeoutSeconds: 30},
		ExternalInputs:    []ExternalInputSpec{{URL: "https://example.com/input", SHA256: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", Size: 1}},
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

func TestDecodeRecipeSpecV1RejectsTrailingDocument(t *testing.T) {
	const document = `{"schema_version":"1","platform":"linux","architecture":"amd64","base_image":"registry.example/base@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","profile_version_ids":["p1"],"guest_protocol":"v1"}`
	if _, err := DecodeRecipeSpecV1([]byte(document + ` {}`)); err == nil {
		t.Fatal("accepted a second recipe document")
	}
}

func TestValidateRecipeSpecV1RejectsMalformedDigestPinning(t *testing.T) {
	spec := RecipeSpecV1{SchemaVersion: "1", Platform: "linux", Architecture: "amd64", BaseImage: "registry.example/base@sha256:not-a-digest", ProfileVersionIDs: []string{"p1"}, GuestProtocol: "v1"}
	if err := ValidateRecipeSpecV1(spec); err == nil {
		t.Fatal("accepted malformed base digest")
	}
}
