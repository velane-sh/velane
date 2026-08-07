package platformlibs

import "testing"

func TestLoadStoreLibraries(t *testing.T) {
	libs, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	byLanguageAndSlug := make(map[string]PlatformLib, len(libs))
	for _, lib := range libs {
		byLanguageAndSlug[lib.Language+"/"+lib.Slug] = lib
	}

	for _, tc := range []struct {
		language   string
		importPath string
	}{
		{language: "bun", importPath: "@velane/store"},
		{language: "python", importPath: "velane.store"},
	} {
		t.Run(tc.language, func(t *testing.T) {
			lib, ok := byLanguageAndSlug[tc.language+"/store"]
			if !ok {
				t.Fatalf("store library for %s was not loaded", tc.language)
			}
			if got := ImportPath(lib.Language, lib.Slug); got != tc.importPath {
				t.Errorf("ImportPath(%q, %q) = %q; want %q", lib.Language, lib.Slug, got, tc.importPath)
			}
			if lib.Integration != "Velane" {
				t.Errorf("Integration = %q; want %q", lib.Integration, "Velane")
			}
			if lib.Code == "" {
				t.Error("Code is empty")
			}
			if lib.Docs == "" {
				t.Error("Docs from README.md is empty")
			}
		})
	}
}
