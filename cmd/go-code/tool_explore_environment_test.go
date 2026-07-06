package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestBuildExploreOutput_EnvironmentField_PresentWithManifest guards the
// ADR-0002 Phase 0 wiring: explore's output gains an additive `environment`
// block when the repo has a recognized manifest, without disturbing any
// pre-existing field.
func TestBuildExploreOutput_EnvironmentField_PresentWithManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := buildExploreOutput(t.Context(), root, ExploreInput{})
	if err != nil {
		t.Fatalf("buildExploreOutput: %v", err)
	}

	if out.Environment == nil {
		t.Fatal("want a non-nil Environment for a repo with go.mod")
	}
	if len(out.Environment.Toolchains) != 1 {
		t.Fatalf("want 1 toolchain, got %d: %+v", len(out.Environment.Toolchains), out.Environment.Toolchains)
	}
	if out.Environment.Toolchains[0].Language != "go" {
		t.Errorf("want go toolchain, got %+v", out.Environment.Toolchains[0])
	}

	// Base explore fields must be populated exactly as before this change —
	// this wiring must not perturb explore.Run's own output.
	if out.Result == nil {
		t.Fatal("Result must not be nil")
	}
	if out.FileCount == 0 {
		t.Errorf("want at least 1 ingested file, got 0")
	}

	body, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if _, ok := raw["environment"]; !ok {
		t.Error("marshaled output must contain an \"environment\" key when toolchains are detected")
	}
	if _, ok := raw["file_count"]; !ok {
		t.Error("pre-existing \"file_count\" field must remain present")
	}
}

// TestBuildExploreOutput_EnvironmentField_OmittedWithoutManifest is the
// additive-cold-path guard: no recognized manifest -> the environment block
// is omitted entirely (not an empty object), and every pre-existing field
// stays exactly as explore.Run + freshness produce it.
func TestBuildExploreOutput_EnvironmentField_OmittedWithoutManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.txt"), []byte("no manifest here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := buildExploreOutput(t.Context(), root, ExploreInput{})
	if err != nil {
		t.Fatalf("buildExploreOutput: %v", err)
	}

	if out.Environment != nil {
		t.Fatalf("want nil Environment for a repo with no recognized manifest, got %+v", out.Environment)
	}
	if out.Freshness != nil {
		t.Fatalf("want nil Freshness for a repo with no recognized manifest, got %+v", out.Freshness)
	}

	body, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("json marshal: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if _, ok := raw["environment"]; ok {
		t.Error("marshaled output must omit \"environment\" entirely when no toolchains are detected")
	}
	if _, ok := raw["freshness"]; ok {
		t.Error("marshaled output must omit \"freshness\" entirely when no manifests are found")
	}
}
