# Dependency Freshness Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add dependency freshness checking to `code_health` — parse manifest files, check latest versions via public registry APIs, compute freshness ratio, factor into health grade.

**Architecture:** New `internal/freshness/` package handles manifest parsing + registry lookups. `RepoSnapshot` gains a `ManifestFiles` field populated during ingest. `ComputeMetrics` calls freshness checker, adds `DepFreshnessRatio` to `RepoMetrics`. Grade formula gets a 13th sub-score.

**Tech Stack:** Go stdlib `net/http` for registry APIs, `encoding/json`/`encoding/xml` for responses, `golang.org/x/mod/semver` for version comparison, Redis for caching.

---

### Task 1: Common Types and Manifest Parser Interface

**Files:**
- Create: `internal/freshness/freshness.go`
- Test: `internal/freshness/freshness_test.go`

**Step 1: Write the failing test**

```go
package freshness

import "testing"

func TestDependencyString(t *testing.T) {
	d := Dependency{Name: "github.com/foo/bar", Version: "v1.2.3", Language: "go"}
	if d.Name != "github.com/foo/bar" {
		t.Fatal("unexpected name")
	}
}

func TestManifestInfoEmpty(t *testing.T) {
	m := ManifestInfo{}
	if len(m.Dependencies) != 0 {
		t.Fatal("expected empty deps")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd $REPO_ROOT && go test ./internal/freshness/ -v -run TestDependency`
Expected: FAIL — package does not exist

**Step 3: Write minimal implementation**

```go
package freshness

// Dependency represents a single package dependency with its current version.
type Dependency struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Language string `json:"language"`
}

// ManifestInfo holds parsed dependency information from a manifest file.
type ManifestInfo struct {
	Language       string       `json:"language"`
	RuntimeVersion string       `json:"runtimeVersion,omitempty"`
	Dependencies   []Dependency `json:"dependencies"`
	ManifestPath   string       `json:"manifestPath"`
}

// FreshnessResult summarizes dependency freshness for a repository.
type FreshnessResult struct {
	Total         int            `json:"total"`
	UpToDate      int            `json:"upToDate"`
	MinorOutdated int            `json:"minorOutdated"`
	MajorOutdated int            `json:"majorOutdated"`
	Ratio         float64        `json:"ratio"` // UpToDate / Total, in [0,1]
	RuntimeStatus string         `json:"runtimeStatus"` // "current", "outdated", "unknown"
	Outdated      []OutdatedDep  `json:"outdated,omitempty"`
}

// OutdatedDep describes a single outdated dependency.
type OutdatedDep struct {
	Name    string `json:"name"`
	Current string `json:"current"`
	Latest  string `json:"latest"`
	Kind    string `json:"kind"` // "minor" or "major"
}
```

**Step 4: Run test to verify it passes**

Run: `cd $REPO_ROOT && go test ./internal/freshness/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/freshness/
git commit -m "feat(freshness): add common types for dependency freshness"
```

---

### Task 2: Go Manifest Parser (go.mod)

**Files:**
- Create: `internal/freshness/parse_gomod.go`
- Test: `internal/freshness/parse_gomod_test.go`

**Step 1: Write the failing test**

```go
package freshness

import "testing"

func TestParseGoMod(t *testing.T) {
	data := []byte(`module github.com/foo/bar

go 1.24

require (
	github.com/stretchr/testify v1.9.0
	golang.org/x/sync v0.7.0
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
)
`)
	info := ParseGoMod(data)
	if info.Language != "go" {
		t.Fatalf("expected go, got %s", info.Language)
	}
	if info.RuntimeVersion != "1.24" {
		t.Fatalf("expected 1.24, got %s", info.RuntimeVersion)
	}
	if len(info.Dependencies) != 3 {
		t.Fatalf("expected 3 deps, got %d", len(info.Dependencies))
	}
	// Check first dep.
	found := false
	for _, d := range info.Dependencies {
		if d.Name == "github.com/stretchr/testify" && d.Version == "v1.9.0" {
			found = true
		}
	}
	if !found {
		t.Fatal("testify dep not found")
	}
}

func TestParseGoModMinimal(t *testing.T) {
	data := []byte("module example.com/foo\n\ngo 1.22\n")
	info := ParseGoMod(data)
	if info.RuntimeVersion != "1.22" {
		t.Fatalf("expected 1.22, got %s", info.RuntimeVersion)
	}
	if len(info.Dependencies) != 0 {
		t.Fatal("expected 0 deps")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd $REPO_ROOT && go test ./internal/freshness/ -v -run TestParseGoMod`
Expected: FAIL — ParseGoMod undefined

**Step 3: Write minimal implementation**

```go
package freshness

import (
	"strings"
)

// ParseGoMod extracts Go version and dependencies from go.mod content.
func ParseGoMod(data []byte) ManifestInfo {
	info := ManifestInfo{Language: "go"}
	lines := strings.Split(string(data), "\n")

	inRequire := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "go ") {
			info.RuntimeVersion = strings.TrimPrefix(trimmed, "go ")
			continue
		}

		if trimmed == "require (" {
			inRequire = true
			continue
		}
		if trimmed == ")" {
			inRequire = false
			continue
		}

		// Single-line require.
		if strings.HasPrefix(trimmed, "require ") && !strings.Contains(trimmed, "(") {
			parts := strings.Fields(strings.TrimPrefix(trimmed, "require "))
			if len(parts) >= 2 {
				info.Dependencies = append(info.Dependencies, Dependency{
					Name: parts[0], Version: parts[1], Language: "go",
				})
			}
			continue
		}

		if inRequire {
			// Strip inline comments.
			if idx := strings.Index(trimmed, "//"); idx > 0 {
				trimmed = strings.TrimSpace(trimmed[:idx])
			}
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				info.Dependencies = append(info.Dependencies, Dependency{
					Name: parts[0], Version: parts[1], Language: "go",
				})
			}
		}
	}
	return info
}
```

**Step 4: Run test to verify it passes**

Run: `cd $REPO_ROOT && go test ./internal/freshness/ -v -run TestParseGoMod`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/freshness/parse_gomod.go internal/freshness/parse_gomod_test.go
git commit -m "feat(freshness): add go.mod parser"
```

---

### Task 3: NPM Manifest Parser (package.json)

**Files:**
- Create: `internal/freshness/parse_npm.go`
- Test: `internal/freshness/parse_npm_test.go`

**Step 1: Write the failing test**

```go
package freshness

import "testing"

func TestParsePackageJSON(t *testing.T) {
	data := []byte(`{
		"name": "my-app",
		"engines": {"node": ">=20.0.0"},
		"dependencies": {
			"react": "^18.2.0",
			"next": "14.1.0"
		},
		"devDependencies": {
			"typescript": "~5.3.0"
		}
	}`)
	info := ParsePackageJSON(data)
	if info.Language != "typescript" {
		t.Fatalf("expected typescript, got %s", info.Language)
	}
	if info.RuntimeVersion != ">=20.0.0" {
		t.Fatalf("expected >=20.0.0, got %s", info.RuntimeVersion)
	}
	if len(info.Dependencies) != 3 {
		t.Fatalf("expected 3 deps, got %d", len(info.Dependencies))
	}
}

func TestParsePackageJSONEmpty(t *testing.T) {
	info := ParsePackageJSON([]byte(`{}`))
	if len(info.Dependencies) != 0 {
		t.Fatal("expected 0 deps")
	}
}
```

**Step 2: Run test, verify fail**

Run: `cd $REPO_ROOT && go test ./internal/freshness/ -v -run TestParsePackageJSON`

**Step 3: Write implementation**

```go
package freshness

import "encoding/json"

type packageJSON struct {
	Name            string            `json:"name"`
	Engines         map[string]string `json:"engines"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

// ParsePackageJSON extracts dependencies from package.json content.
func ParsePackageJSON(data []byte) ManifestInfo {
	info := ManifestInfo{Language: "typescript"}
	var pkg packageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return info
	}

	if v, ok := pkg.Engines["node"]; ok {
		info.RuntimeVersion = v
	}

	for name, ver := range pkg.Dependencies {
		info.Dependencies = append(info.Dependencies, Dependency{
			Name: name, Version: ver, Language: "typescript",
		})
	}
	for name, ver := range pkg.DevDependencies {
		info.Dependencies = append(info.Dependencies, Dependency{
			Name: name, Version: ver, Language: "typescript",
		})
	}
	return info
}
```

**Step 4: Run test, verify pass**

**Step 5: Commit**

```bash
git add internal/freshness/parse_npm.go internal/freshness/parse_npm_test.go
git commit -m "feat(freshness): add package.json parser"
```

---

### Task 4: Python Manifest Parser (pyproject.toml + requirements.txt)

**Files:**
- Create: `internal/freshness/parse_python.go`
- Test: `internal/freshness/parse_python_test.go`

**Step 1: Write tests for both formats**

```go
package freshness

import "testing"

func TestParsePyProjectToml(t *testing.T) {
	data := []byte(`[project]
name = "my-project"
requires-python = ">=3.11"

[project.dependencies]
fastapi = ">=0.100.0"
pydantic = "^2.0"

[project.optional-dependencies]
dev = ["pytest>=7.0", "ruff"]
`)
	info := ParsePyProject(data)
	if info.RuntimeVersion != ">=3.11" {
		t.Fatalf("expected >=3.11, got %s", info.RuntimeVersion)
	}
	if len(info.Dependencies) < 2 {
		t.Fatalf("expected >=2 deps, got %d", len(info.Dependencies))
	}
}

func TestParseRequirementsTxt(t *testing.T) {
	data := []byte(`flask==3.0.0
requests>=2.31.0
# comment
numpy~=1.26
-e git+https://github.com/foo/bar.git
`)
	info := ParseRequirementsTxt(data)
	if len(info.Dependencies) != 3 {
		t.Fatalf("expected 3 deps, got %d", len(info.Dependencies))
	}
}
```

**Step 2: Run test, verify fail**

**Step 3: Write implementation** — line-based parsing for both formats

**Step 4: Run test, verify pass**

**Step 5: Commit**

```bash
git add internal/freshness/parse_python.go internal/freshness/parse_python_test.go
git commit -m "feat(freshness): add Python manifest parsers"
```

---

### Task 5: Extend Cargo.toml Parser with Versions

**Files:**
- Create: `internal/freshness/parse_cargo.go`
- Test: `internal/freshness/parse_cargo_test.go`

Reuse logic from existing `internal/polyglot/cargo.go` but extract versions too. The existing parser only captures dependency names.

**Step 1: Write test**

```go
package freshness

import "testing"

func TestParseCargoTomlFreshness(t *testing.T) {
	data := []byte(`[package]
name = "my-crate"
version = "0.1.0"
edition = "2021"
rust-version = "1.75"

[dependencies]
serde = "1.0"
tokio = { version = "1.36", features = ["full"] }

[dev-dependencies]
criterion = "0.5"
`)
	info := ParseCargoTomlFreshness(data)
	if info.RuntimeVersion != "1.75" {
		t.Fatalf("expected 1.75, got %s", info.RuntimeVersion)
	}
	if len(info.Dependencies) != 3 {
		t.Fatalf("expected 3 deps, got %d", len(info.Dependencies))
	}
}
```

**Step 2-5: Implement, test, commit**

```bash
git commit -m "feat(freshness): add Cargo.toml parser with versions"
```

---

### Task 6: Java (pom.xml) + Ruby (Gemfile) + C# (csproj) Parsers

**Files:**
- Create: `internal/freshness/parse_java.go`, `internal/freshness/parse_ruby.go`, `internal/freshness/parse_csharp.go`
- Test: `internal/freshness/parse_java_test.go`, `internal/freshness/parse_ruby_test.go`, `internal/freshness/parse_csharp_test.go`

**Java (pom.xml):** XML parsing — extract `<dependency>` elements with `groupId:artifactId` as name, `version` as version.

**Ruby (Gemfile):** Line-based — `gem 'name', '~> 1.0'` patterns.

**C# (csproj):** XML parsing — `<PackageReference Include="name" Version="ver"/>`.

Each parser follows the same pattern: test first, implement, verify, commit.

```bash
git commit -m "feat(freshness): add Java, Ruby, C# manifest parsers"
```

---

### Task 7: Manifest Discovery from Ingest Files

**Files:**
- Create: `internal/freshness/discover.go`
- Test: `internal/freshness/discover_test.go`

**Step 1: Write test**

```go
package freshness

import (
	"os"
	"testing"
)

func TestDiscoverManifests(t *testing.T) {
	// Create temp dir with go.mod and package.json
	dir := t.TempDir()
	os.WriteFile(dir+"/go.mod", []byte("module foo\n\ngo 1.24\n"), 0o644)
	os.MkdirAll(dir+"/frontend", 0o755)
	os.WriteFile(dir+"/frontend/package.json", []byte(`{"dependencies":{"react":"18.0.0"}}`), 0o644)

	manifests := DiscoverManifests(dir)
	if len(manifests) != 2 {
		t.Fatalf("expected 2 manifests, got %d", len(manifests))
	}
}
```

**Step 3: Write implementation**

```go
package freshness

import (
	"os"
	"path/filepath"
)

// manifestParsers maps filename to parser function.
var manifestParsers = map[string]func([]byte) ManifestInfo{
	"go.mod":         ParseGoMod,
	"package.json":   ParsePackageJSON,
	"Cargo.toml":     ParseCargoTomlFreshness,
	"pyproject.toml": ParsePyProject,
	"requirements.txt": ParseRequirementsTxt,
	"pom.xml":        ParsePomXML,
	"Gemfile":        ParseGemfile,
}

// DiscoverManifests walks root to find and parse all manifest files.
// Returns parsed ManifestInfo for each found manifest.
func DiscoverManifests(root string) []ManifestInfo {
	var results []ManifestInfo
	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			// Skip vendor, node_modules, .git
			if d != nil && d.IsDir() {
				name := d.Name()
				if name == "vendor" || name == "node_modules" || name == ".git" {
					return filepath.SkipDir
				}
			}
			return nil
		}
		name := d.Name()
		parser, ok := manifestParsers[name]
		if !ok {
			// Check .csproj extension.
			if filepath.Ext(name) == ".csproj" {
				parser = ParseCsproj
			} else {
				return nil
			}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		info := parser(data)
		rel, _ := filepath.Rel(root, path)
		info.ManifestPath = rel
		results = append(results, info)
		return nil
	})
	return results
}
```

**Step 4-5: Test, commit**

```bash
git commit -m "feat(freshness): add manifest discovery"
```

---

### Task 8: Registry Clients (Go, NPM, PyPI, Crates, Maven, RubyGems, NuGet)

**Files:**
- Create: `internal/freshness/registry.go`
- Test: `internal/freshness/registry_test.go`

**Step 1: Write test with mock server**

```go
package freshness

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGoRegistryLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Version":"v1.10.0"}`))
	}))
	defer srv.Close()

	reg := &GoRegistry{BaseURL: srv.URL}
	ver, err := reg.Latest(context.Background(), "github.com/foo/bar")
	if err != nil {
		t.Fatal(err)
	}
	if ver != "v1.10.0" {
		t.Fatalf("expected v1.10.0, got %s", ver)
	}
}
```

**Step 3: Write implementation**

Registry interface + per-language implementations:

```go
package freshness

import "context"

// Registry can look up the latest version of a package.
type Registry interface {
	Latest(ctx context.Context, name string) (string, error)
}
```

Each registry struct (GoRegistry, NpmRegistry, PyPIRegistry, CratesRegistry, MavenRegistry, RubyGemsRegistry, NuGetRegistry) implements `Latest()` by calling the public API and extracting the version from the JSON response.

**API endpoints:**
- Go: `GET https://proxy.golang.org/{module}/@latest` → `{"Version":"v1.2.3"}`
- NPM: `GET https://registry.npmjs.org/{pkg}/latest` → `{"version":"1.2.3"}`
- PyPI: `GET https://pypi.org/pypi/{pkg}/json` → `{"info":{"version":"1.2.3"}}`
- Crates: `GET https://crates.io/api/v1/crates/{name}` → `{"crate":{"max_stable_version":"1.2.3"}}`
- Maven: `GET https://search.maven.org/solrsearch/select?q=g:{g}+AND+a:{a}&rows=1` → `{"response":{"docs":[{"latestVersion":"1.2.3"}]}}`
- RubyGems: `GET https://rubygems.org/api/v1/gems/{name}.json` → `{"version":"1.2.3"}`
- NuGet: `GET https://api.nuget.org/v3-flatcontainer/{name}/index.json` → `{"versions":["1.0","2.0"]}`

**Step 4-5: Test (with mocks), commit**

```bash
git commit -m "feat(freshness): add registry clients for 7 ecosystems"
```

---

### Task 9: Version Comparison and Freshness Calculation

**Files:**
- Create: `internal/freshness/check.go`
- Test: `internal/freshness/check_test.go`

**Step 1: Write test**

```go
package freshness

import (
	"context"
	"testing"
)

type mockRegistry struct {
	versions map[string]string
}

func (m *mockRegistry) Latest(_ context.Context, name string) (string, error) {
	return m.versions[name], nil
}

func TestCheckFreshness(t *testing.T) {
	deps := []Dependency{
		{Name: "a", Version: "v1.2.0", Language: "go"},
		{Name: "b", Version: "v1.0.0", Language: "go"},
		{Name: "c", Version: "v2.0.0", Language: "go"},
	}
	reg := &mockRegistry{versions: map[string]string{
		"a": "v1.2.0", // up to date
		"b": "v1.5.0", // minor outdated
		"c": "v3.0.0", // major outdated
	}}

	result := CheckFreshness(context.Background(), deps, reg)
	if result.Total != 3 {
		t.Fatalf("expected 3 total, got %d", result.Total)
	}
	if result.UpToDate != 1 {
		t.Fatalf("expected 1 up to date, got %d", result.UpToDate)
	}
	if result.MinorOutdated != 1 {
		t.Fatalf("expected 1 minor, got %d", result.MinorOutdated)
	}
	if result.MajorOutdated != 1 {
		t.Fatalf("expected 1 major, got %d", result.MajorOutdated)
	}
}
```

**Step 3: Implement** — semver parsing, compare major/minor, bounded concurrency (10 goroutines), 3s timeout per lookup, skip on error.

**Step 4-5: Test, commit**

```bash
git commit -m "feat(freshness): add version comparison and freshness check"
```

---

### Task 10: Redis Caching for Registry Lookups

**Files:**
- Create: `internal/freshness/cache.go`
- Test: `internal/freshness/cache_test.go`

Wrap any `Registry` with a Redis-backed cache. Key: `dep:fresh:{lang}:{name}`, TTL 24h. Falls back to uncached on Redis error.

```bash
git commit -m "feat(freshness): add Redis cache for registry lookups"
```

---

### Task 11: Integrate into RepoMetrics and Grade

**Files:**
- Modify: `internal/compare/compare.go:159-185` — add `DepFreshnessRatio` field
- Modify: `internal/compare/grade.go` — add 13th weight, rebalance
- Modify: `internal/compare/recommend.go` — add freshness case in `buildMessage`
- Modify: `internal/compare/metrics.go` — call freshness check
- Test: update existing `internal/compare/grade_test.go`

**Step 1: Add field to RepoMetrics**

```go
// In compare.go, add to RepoMetrics struct:
DepFreshnessRatio float64 `json:"depFreshnessRatio,omitempty"` // fraction of up-to-date deps
```

**Step 2: Add weight in grade.go**

Rebalance weights — reduce test_coverage from 0.16→0.14, semantic_dup from 0.05→0.04 to make room for 0.06 freshness:

```go
weightDepFreshness = 0.06
// Target: 80% deps up to date
targetDepFreshness = 0.8
```

Add to `GradeScore()`:
```go
freshnessScore := clamp01(m.DepFreshnessRatio / targetDepFreshness)
// Add to total: freshnessScore*weightDepFreshness
```

Add to `computeSubScores()`:
```go
{"dep_freshness", clamp01(m.DepFreshnessRatio / targetDepFreshness), weightDepFreshness, 0},
```

**Step 3: Add recommendation in recommend.go**

```go
case "dep_freshness":
    return fmt.Sprintf("Update outdated dependencies (%.0f%% current, target: %.0f%%)",
        m.DepFreshnessRatio*percentScale, targetDepFreshness*percentScale)
```

**Step 4: Update existing grade_test.go** — verify weight sum = 1.0

**Step 5: Commit**

```bash
git commit -m "feat(freshness): integrate dep freshness into health grade"
```

---

### Task 12: Wire Freshness into code_health Tool Handler

**Files:**
- Modify: `cmd/go-code/tool_code_health.go` — add freshness check after snapshot
- Modify: `cmd/go-code/tool_code_health_reports.go` — add XML section

**Step 1: Add freshness check in handler**

After `metrics := compare.ComputeMetrics(snap)`, add:

```go
// Dependency freshness (optional, non-fatal).
manifests := freshness.DiscoverManifests(root)
if len(manifests) > 0 {
    allDeps := freshness.CollectDeps(manifests)
    reg := freshness.NewMultiRegistry(cfg.RedisURL)
    fr := freshness.CheckFreshness(ctx, allDeps, reg)
    metrics.DepFreshnessRatio = fr.Ratio
    score = compare.GradeScore(metrics)
    metrics.Score = score
    metrics.Grade = compare.ComputeGrade(metrics)
}
```

**Step 2: Add XML section**

```go
type xmlDepFreshness struct {
    Runtime  *xmlRuntime       `xml:"runtime,omitempty"`
    Summary  xmlFreshSummary   `xml:"summary"`
    Outdated []xmlOutdatedDep  `xml:"outdated>dep,omitempty"`
}
```

**Step 3: Run full test suite**

Run: `cd $REPO_ROOT && make test`
Expected: PASS

**Step 4: Commit**

```bash
git commit -m "feat(freshness): wire into code_health MCP tool with XML output"
```

---

### Task 13: Go Runtime Version Check

**Files:**
- Create: `internal/freshness/runtime.go`
- Test: `internal/freshness/runtime_test.go`

Check `go` directive against latest stable from `https://go.dev/dl/?mode=json`. Parse first entry's `version` field (e.g. `"go1.24.1"` → `"1.24"`).

```bash
git commit -m "feat(freshness): add Go runtime version freshness check"
```

---

### Task 14: Integration Test

**Files:**
- Create: `internal/freshness/integration_test.go`

Test with a real local repo (go-code itself via `go.mod`). Verify:
1. Manifests discovered
2. Dependencies parsed
3. Freshness ratio computed (0.0–1.0)
4. No panics on network errors

```bash
git commit -m "test(freshness): add integration test"
```

---

### Task 15: Update tool description + build + deploy

**Files:**
- Modify: `cmd/go-code/tool_code_health.go:57` — update description to mention dependency freshness
- Modify: `CLAUDE.md` — mention freshness in code_health description

**Steps:**
1. Update description string
2. `make lint && make test`
3. `make deploy` (docker compose build + up)
4. Verify via MCP: `code_health repo=$REPO_ROOT`

```bash
git commit -m "docs: update code_health description with dep freshness"
```
