package freshness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverManifests(t *testing.T) {
	dir := t.TempDir()

	// Create go.mod.
	writeTestFile(t, dir, "go.mod", `module example.com/m
go 1.22
require github.com/foo/bar v1.0.0
`)

	// Create package.json in subdir.
	subDir := filepath.Join(dir, "frontend")
	mustMkdir(t, subDir)
	writeTestFile(t, subDir, "package.json", `{
  "dependencies": {"react": "^18.0.0"}
}`)

	// Create a file in vendor (should be skipped).
	vendorDir := filepath.Join(dir, "vendor")
	mustMkdir(t, vendorDir)
	writeTestFile(t, vendorDir, "go.mod", `module vendored
go 1.20
`)

	manifests := DiscoverManifests(dir)

	wantCount := 2
	if len(manifests) != wantCount {
		t.Fatalf("manifests count = %d, want %d", len(manifests), wantCount)
	}

	// Verify paths are relative.
	for _, m := range manifests {
		if filepath.IsAbs(m.ManifestPath) {
			t.Errorf("ManifestPath should be relative, got %q", m.ManifestPath)
		}
	}
}

func TestDiscoverManifests_SkipDirs(t *testing.T) {
	dir := t.TempDir()

	for _, skip := range []string{"node_modules", ".git", "testdata"} {
		d := filepath.Join(dir, skip)
		mustMkdir(t, d)
		writeTestFile(t, d, "package.json", `{"dependencies":{"x":"1.0"}}`)
	}

	manifests := DiscoverManifests(dir)
	if len(manifests) != 0 {
		t.Errorf("manifests count = %d, want 0 (all in skip dirs)", len(manifests))
	}
}

func TestDiscoverManifests_Csproj(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, dir, "MyApp.csproj", `<Project Sdk="Microsoft.NET.Sdk">
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" Version="13.0.3" />
  </ItemGroup>
</Project>`)

	manifests := DiscoverManifests(dir)
	if len(manifests) != 1 {
		t.Fatalf("manifests count = %d, want 1", len(manifests))
	}
	if manifests[0].Language != "csharp" {
		t.Errorf("Language = %q, want %q", manifests[0].Language, "csharp")
	}
}

func TestCollectDeps(t *testing.T) {
	manifests := []ManifestInfo{
		{Dependencies: []Dependency{{Name: "a"}, {Name: "b"}}},
		{Dependencies: []Dependency{{Name: "c"}}},
	}
	deps := CollectDeps(manifests)
	if len(deps) != 3 {
		t.Errorf("deps count = %d, want 3", len(deps))
	}
}

func TestCollectDeps_Empty(t *testing.T) {
	deps := CollectDeps(nil)
	if len(deps) != 0 {
		t.Errorf("deps count = %d, want 0", len(deps))
	}
}

func TestDiscoverManifests_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	manifests := DiscoverManifests(dir)
	if len(manifests) != 0 {
		t.Errorf("manifests count = %d, want 0", len(manifests))
	}
}

func writeTestFile(t *testing.T, dir, name, content string) {
	t.Helper()
	err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)
	if err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
