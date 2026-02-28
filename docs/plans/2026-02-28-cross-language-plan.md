# Cross-Language Analysis Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add cross-language awareness to go-code: detect polyglot repos, extract HTTP routes, link backend↔frontend through shared Route vertices in Apache AGE.

**Architecture:** Three new packages (`internal/polyglot/`, `internal/routes/`, route matchers per language) feed into the existing codegraph indexer. New AGE vertex types (Layer, Route) and edge types (BELONGS_TO, HANDLES, FETCHES) enable Cypher queries across language boundaries. Four new query templates expose the data through the existing `code_graph` MCP tool.

**Tech Stack:** Go, Apache AGE (PostgreSQL), tree-sitter (existing), regex patterns for route extraction, LLM (Gemini) for fallback detection.

**Design doc:** `docs/plans/2026-02-28-cross-language-design.md`

---

## Context for the Implementer

**Key files to understand before starting:**
- `internal/codegraph/index.go` — IndexRepo orchestrator (you'll add 2 new phases here)
- `internal/codegraph/graph_build.go` — vertex/edge construction (follow same patterns)
- `internal/codegraph/cypher_batch.go` — Cypher MERGE/SET formatting, `matchKey()` helper
- `internal/codegraph/templates.go` — 10 existing Cypher templates (you'll add 4 more)
- `internal/parser/parser.go` — Symbol type with Name, Kind, Language, File, StartLine, Signature fields
- `internal/parser/calls.go` — CallSite type with Name, Receiver, Line, File fields
- `internal/ingest/ingest.go` — File type with Path, RelPath, Language, Size fields

**Conventions:**
- Error messages: lowercase, wrap with `fmt.Errorf("context: %w", err)`
- Context is always the first parameter
- Tests use table-driven subtests with `t.Parallel()`
- AGE Cypher uses `{param}` placeholders (not `$param` — AGE interprets `$` as parameter reference)
- Edges are inserted one-at-a-time (AGE can't do MATCH after MERGE in a single batch)

**Build & test:**
```bash
cd /home/krolik/src/go-code
go test ./internal/polyglot/ -v      # polyglot tests
go test ./internal/routes/ -v        # route tests
go test ./internal/codegraph/ -v     # codegraph tests
go test ./... 2>&1 | tail -20        # full suite
```

**Deploy:**
```bash
cd /home/krolik/deploy/krolik-server
docker compose build --no-cache go-code && docker compose up -d --no-deps --force-recreate go-code
```

---

### Task 1: Polyglot Detection — Types and Manifest Scanning

**Files:**
- Create: `internal/polyglot/detect.go`
- Create: `internal/polyglot/detect_test.go`

**Step 1: Write the failing test**

Create `internal/polyglot/detect_test.go`:

```go
package polyglot

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/ingest"
)

func TestDetectStructure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		files     []*ingest.File
		wantLangs int
		wantLayers int
	}{
		{
			name: "go only repo",
			files: []*ingest.File{
				{RelPath: "main.go", Language: "go"},
				{RelPath: "handler.go", Language: "go"},
				{RelPath: "go.mod", Language: ""},
			},
			wantLangs:  1,
			wantLayers: 1,
		},
		{
			name: "go backend + ts frontend",
			files: []*ingest.File{
				{RelPath: "backend/main.go", Language: "go"},
				{RelPath: "backend/handler.go", Language: "go"},
				{RelPath: "backend/go.mod", Language: ""},
				{RelPath: "frontend/src/App.tsx", Language: "typescript"},
				{RelPath: "frontend/src/api.ts", Language: "typescript"},
				{RelPath: "frontend/package.json", Language: ""},
			},
			wantLangs:  2,
			wantLayers: 2,
		},
		{
			name: "monorepo with three languages",
			files: []*ingest.File{
				{RelPath: "api/main.go", Language: "go"},
				{RelPath: "api/go.mod", Language: ""},
				{RelPath: "web/index.tsx", Language: "typescript"},
				{RelPath: "web/package.json", Language: ""},
				{RelPath: "ml/train.py", Language: "python"},
				{RelPath: "ml/pyproject.toml", Language: ""},
			},
			wantLangs:  3,
			wantLayers: 3,
		},
		{
			name: "flat repo no manifests",
			files: []*ingest.File{
				{RelPath: "main.go", Language: "go"},
				{RelPath: "util.go", Language: "go"},
			},
			wantLangs:  1,
			wantLayers: 1,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rs := DetectStructure(tc.files)
			if len(rs.Languages) != tc.wantLangs {
				t.Errorf("languages: got %d, want %d", len(rs.Languages), tc.wantLangs)
			}
			if len(rs.Layers) != tc.wantLayers {
				t.Errorf("layers: got %d, want %d", len(rs.Layers), tc.wantLayers)
			}
		})
	}
}

func TestFindManifests(t *testing.T) {
	t.Parallel()

	files := []*ingest.File{
		{RelPath: "go.mod"},
		{RelPath: "backend/go.mod"},
		{RelPath: "frontend/package.json"},
		{RelPath: "ml/pyproject.toml"},
		{RelPath: "main.go", Language: "go"},
	}

	manifests := findManifests(files)
	if len(manifests) != 4 {
		t.Errorf("got %d manifests, want 4", len(manifests))
	}
}

func TestManifestLanguage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		filename string
		want     string
	}{
		{"go.mod", "go"},
		{"package.json", "typescript"},
		{"Cargo.toml", "rust"},
		{"pyproject.toml", "python"},
		{"requirements.txt", "python"},
		{"pom.xml", "java"},
		{"build.gradle", "java"},
		{"Gemfile", "ruby"},
		{"foo.csproj", "csharp"},
		{"unknown.txt", ""},
	}

	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			t.Parallel()
			got := manifestLanguage(tc.filename)
			if got != tc.want {
				t.Errorf("manifestLanguage(%q) = %q, want %q", tc.filename, got, tc.want)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/polyglot/ -v`
Expected: FAIL — package doesn't exist yet.

**Step 3: Write minimal implementation**

Create `internal/polyglot/detect.go`:

```go
package polyglot

import (
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/go-code/internal/ingest"
)

// RepoStructure describes the multi-language layout of a repository.
type RepoStructure struct {
	Layers    []Layer
	Languages map[string]int // language → file count
	Manifests []Manifest
}

// Layer is a cohesive directory subtree with a dominant language.
type Layer struct {
	Name     string // directory name or "root"
	Role     string // "server", "client", "worker", "library"
	RootDir  string // relative directory path
	Language string // dominant language
	Files    int
}

// Manifest is a language build manifest found in the repo.
type Manifest struct {
	Path     string // relative path
	Type     string // filename (go.mod, package.json, etc.)
	Language string
}

// DetectStructure analyzes ingested files and returns the repo's polyglot layout.
func DetectStructure(files []*ingest.File) *RepoStructure {
	rs := &RepoStructure{
		Languages: make(map[string]int),
	}

	// Count languages.
	for _, f := range files {
		if f.Language != "" {
			rs.Languages[f.Language]++
		}
	}

	// Find manifests.
	rs.Manifests = findManifests(files)

	// Build layers from manifests.
	if len(rs.Manifests) > 0 {
		rs.Layers = layersFromManifests(rs.Manifests, files)
	}

	// Fallback: if no layers found, create a single root layer.
	if len(rs.Layers) == 0 {
		dominant := dominantLanguage(rs.Languages)
		rs.Layers = []Layer{{
			Name:     "root",
			Role:     "library",
			RootDir:  "",
			Language: dominant,
			Files:    countSourceFiles(files),
		}}
	}

	return rs
}

// IsPolyglot returns true if the repo has 2+ languages with 5+ files each.
func (rs *RepoStructure) IsPolyglot() bool {
	count := 0
	for _, n := range rs.Languages {
		if n >= 5 {
			count++
		}
	}
	return count >= 2
}

// findManifests returns all build manifests in the file list.
func findManifests(files []*ingest.File) []Manifest {
	var result []Manifest
	for _, f := range files {
		base := filepath.Base(f.RelPath)
		lang := manifestLanguage(base)
		if lang != "" {
			result = append(result, Manifest{
				Path:     f.RelPath,
				Type:     base,
				Language: lang,
			})
		}
	}
	return result
}

// manifestLanguage returns the language associated with a manifest filename.
func manifestLanguage(filename string) string {
	switch filename {
	case "go.mod", "go.sum":
		return "go"
	case "package.json", "package-lock.json", "yarn.lock", "tsconfig.json":
		return "typescript"
	case "Cargo.toml", "Cargo.lock":
		return "rust"
	case "pyproject.toml", "requirements.txt", "setup.py", "Pipfile":
		return "python"
	case "pom.xml", "build.gradle", "build.gradle.kts":
		return "java"
	case "Gemfile", "Gemfile.lock":
		return "ruby"
	}
	if strings.HasSuffix(filename, ".csproj") || strings.HasSuffix(filename, ".sln") {
		return "csharp"
	}
	return ""
}

// layersFromManifests groups files by their nearest manifest root directory.
func layersFromManifests(manifests []Manifest, files []*ingest.File) []Layer {
	// Collect unique manifest directories.
	dirs := make(map[string]Manifest)
	for _, m := range manifests {
		dir := filepath.Dir(m.Path)
		if dir == "." {
			dir = ""
		}
		// Keep the first manifest per directory.
		if _, ok := dirs[dir]; !ok {
			dirs[dir] = m
		}
	}

	// Assign files to nearest manifest directory.
	type layerAcc struct {
		manifest Manifest
		langs    map[string]int
		total    int
	}
	acc := make(map[string]*layerAcc)
	for dir, m := range dirs {
		acc[dir] = &layerAcc{manifest: m, langs: make(map[string]int)}
	}

	for _, f := range files {
		if f.Language == "" {
			continue
		}
		best := ""
		bestLen := -1
		for dir := range dirs {
			if dir == "" || strings.HasPrefix(f.RelPath, dir+"/") {
				if len(dir) > bestLen {
					best = dir
					bestLen = len(dir)
				}
			}
		}
		if bestLen >= 0 {
			acc[best].langs[f.Language]++
			acc[best].total++
		}
	}

	var layers []Layer
	for dir, a := range acc {
		if a.total == 0 {
			continue
		}
		name := filepath.Base(dir)
		if dir == "" {
			name = "root"
		}
		layers = append(layers, Layer{
			Name:     name,
			Role:     "library", // default; role classification is Task 2
			RootDir:  dir,
			Language: dominantLanguage(a.langs),
			Files:    a.total,
		})
	}
	return layers
}

func dominantLanguage(langs map[string]int) string {
	best, bestN := "", 0
	for lang, n := range langs {
		if n > bestN {
			best = lang
			bestN = n
		}
	}
	return best
}

func countSourceFiles(files []*ingest.File) int {
	n := 0
	for _, f := range files {
		if f.Language != "" {
			n++
		}
	}
	return n
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/polyglot/ -v`
Expected: PASS — all 4 DetectStructure cases, 4 manifests, 9 manifest language cases.

**Step 5: Commit**

```bash
git add internal/polyglot/detect.go internal/polyglot/detect_test.go
git commit -m "feat: add polyglot detection package with manifest scanning and layer grouping"
```

---

### Task 2: Polyglot Detection — Role Classification

**Files:**
- Create: `internal/polyglot/role.go`
- Create: `internal/polyglot/role_test.go`
- Modify: `internal/polyglot/detect.go` — wire role classification into `layersFromManifests`

**Step 1: Write the failing test**

Create `internal/polyglot/role_test.go`:

```go
package polyglot

import "testing"

func TestClassifyRole(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		sources []string
		want    string
	}{
		{
			name:    "go http server",
			sources: []string{`http.ListenAndServe(":8080", nil)`},
			want:    "server",
		},
		{
			name:    "express server",
			sources: []string{`const app = express()`},
			want:    "server",
		},
		{
			name:    "flask server",
			sources: []string{`app = Flask(__name__)`},
			want:    "server",
		},
		{
			name:    "spring boot",
			sources: []string{`SpringApplication.run(App.class, args)`},
			want:    "server",
		},
		{
			name:    "react client",
			sources: []string{`ReactDOM.render(<App />, root)`},
			want:    "client",
		},
		{
			name:    "fetch client",
			sources: []string{`fetch('/api/users')`},
			want:    "client",
		},
		{
			name:    "ml worker",
			sources: []string{`import torch`, `model = torch.nn.Linear(10, 5)`},
			want:    "worker",
		},
		{
			name:    "no signals",
			sources: []string{`func add(a, b int) int { return a + b }`},
			want:    "library",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifyRole(tc.sources)
			if got != tc.want {
				t.Errorf("classifyRole() = %q, want %q", got, tc.want)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/polyglot/ -v -run TestClassifyRole`
Expected: FAIL — `classifyRole` not defined.

**Step 3: Write minimal implementation**

Create `internal/polyglot/role.go`:

```go
package polyglot

import "strings"

// serverSignals are patterns that indicate a server/backend layer.
var serverSignals = []string{
	// Go
	"http.ListenAndServe", "http.Serve", "gin.Default", "echo.New", "chi.NewRouter",
	// TypeScript/JavaScript
	"express()", "fastify()", "Hono()", "createServer(",
	// Python
	"Flask(__name__)", "FastAPI()", "Django", "uvicorn.run",
	// Java
	"SpringApplication.run", "SpringBootApplication",
	// Rust
	"HttpServer::new", "axum::Router", "rocket::build",
	// Ruby
	"Sinatra::Base", "Rails.application",
	// C#
	"WebApplication.CreateBuilder", "Host.CreateDefaultBuilder",
}

// clientSignals are patterns that indicate a client/frontend layer.
var clientSignals = []string{
	"ReactDOM", "createRoot", "document.querySelector", "document.getElementById",
	"fetch(", "axios.", "XMLHttpRequest",
	"Vue.createApp", "createApp(",
	"angular.module",
}

// workerSignals are patterns that indicate an ML/data worker layer.
var workerSignals = []string{
	"import torch", "import tensorflow", "from sklearn", "import pandas",
	"torch.nn", "tf.keras", "model.fit", "model.train",
}

// classifyRole returns the role of a layer based on source code patterns.
func classifyRole(sources []string) string {
	joined := strings.Join(sources, "\n")
	for _, sig := range serverSignals {
		if strings.Contains(joined, sig) {
			return "server"
		}
	}
	for _, sig := range clientSignals {
		if strings.Contains(joined, sig) {
			return "client"
		}
	}
	for _, sig := range workerSignals {
		if strings.Contains(joined, sig) {
			return "worker"
		}
	}
	return "library"
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/polyglot/ -v -run TestClassifyRole`
Expected: PASS

**Step 5: Wire role classification into detect.go**

In `internal/polyglot/detect.go`, add a `SourceReader` field to `DetectStructure` options, or keep it simple: the role classification will be wired during graph build when source code is available. For now, the `layersFromManifests` function sets `Role: "library"` as default. The actual role is determined at index time when sources are read.

Add a public function to `detect.go`:

```go
// ClassifyLayerRole updates a layer's role based on source code samples.
func ClassifyLayerRole(sources []string) string {
	return classifyRole(sources)
}
```

**Step 6: Run all polyglot tests**

Run: `go test ./internal/polyglot/ -v`
Expected: All PASS

**Step 7: Commit**

```bash
git add internal/polyglot/role.go internal/polyglot/role_test.go internal/polyglot/detect.go
git commit -m "feat: add role classification for polyglot layers (server/client/worker/library)"
```

---

### Task 3: Route Extraction — Types and Go Matcher

**Files:**
- Create: `internal/routes/routes.go`
- Create: `internal/routes/match_go.go`
- Create: `internal/routes/routes_test.go`

**Step 1: Write the failing test**

Create `internal/routes/routes_test.go`:

```go
package routes

import "testing"

func TestGoServerRoutes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		source string
		want   int
	}{
		{
			name:   "http.HandleFunc",
			source: `http.HandleFunc("/api/users", handleUsers)`,
			want:   1,
		},
		{
			name:   "chi router",
			source: `r.Get("/api/users/{id}", getUser)` + "\n" + `r.Post("/api/users", createUser)`,
			want:   2,
		},
		{
			name:   "gin router",
			source: `r.GET("/api/users", listUsers)` + "\n" + `r.DELETE("/api/users/:id", deleteUser)`,
			want:   2,
		},
		{
			name:   "echo router",
			source: `e.GET("/health", healthCheck)`,
			want:   1,
		},
		{
			name:   "no routes",
			source: `fmt.Println("hello")`,
			want:   0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := &GoMatcher{}
			routes := m.Match([]byte(tc.source))
			if len(routes) != tc.want {
				t.Errorf("got %d routes, want %d; routes=%v", len(routes), tc.want, routes)
			}
		})
	}
}

func TestGoClientRoutes(t *testing.T) {
	t.Parallel()

	source := `resp, err := http.Get("https://api.example.com/users")` + "\n" +
		`req, _ := http.NewRequest("POST", "/api/items", body)`

	m := &GoMatcher{}
	routes := m.Match([]byte(source))

	var clients int
	for _, r := range routes {
		if r.Side == "client" {
			clients++
		}
	}
	if clients != 2 {
		t.Errorf("got %d client routes, want 2", clients)
	}
}

func TestNormalizePath(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input string
		want  string
	}{
		{"/api/users/:id", "/api/users/*"},
		{"/api/users/{id}", "/api/users/*"},
		{"/api/v1/items", "/api/v1/items"},
		{"/health", "/health"},
		{"https://api.example.com/users", "/users"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got := NormalizePath(tc.input)
			if got != tc.want {
				t.Errorf("NormalizePath(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/routes/ -v`
Expected: FAIL — package doesn't exist.

**Step 3: Write implementation**

Create `internal/routes/routes.go`:

```go
package routes

import (
	"net/url"
	"regexp"
	"strings"
)

// Route represents an HTTP route extracted from source code.
type Route struct {
	Method    string // "GET", "POST", "*"
	Path      string // normalized path
	RawPath   string // original path from source
	Handler   string // symbol name
	Framework string // "net/http", "chi", "express", etc.
	File      string
	Line      uint32
	Side      string // "server" or "client"
}

// RouteMatcher extracts routes from source code for a specific language.
type RouteMatcher interface {
	Language() string
	Match(source []byte) []Route
}

// registry holds all registered matchers by language.
var registry []RouteMatcher

// Register adds a matcher to the global registry.
func Register(m RouteMatcher) {
	registry = append(registry, m)
}

// ExtractAll runs all registered matchers against source code of the given language.
func ExtractAll(language string, source []byte) []Route {
	var result []Route
	for _, m := range registry {
		if m.Language() == language {
			result = append(result, m.Match(source)...)
		}
	}
	return result
}

// paramPattern matches path parameters like :id or {id}.
var paramPattern = regexp.MustCompile(`[/:](\{[^}]+\}|:[^/]+)`)

// NormalizePath normalizes an HTTP path for cross-language matching.
func NormalizePath(raw string) string {
	p := raw

	// Strip URL scheme and host if present.
	if strings.Contains(p, "://") {
		if u, err := url.Parse(p); err == nil {
			p = u.Path
		}
	}

	// Ensure leading slash.
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}

	// Replace path parameters with wildcard.
	p = paramPattern.ReplaceAllString(p, "/*")

	// Clean double slashes.
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}

	return p
}
```

Create `internal/routes/match_go.go`:

```go
package routes

import "regexp"

// GoMatcher extracts HTTP routes from Go source code.
type GoMatcher struct{}

func init() { Register(&GoMatcher{}) }

func (GoMatcher) Language() string { return "go" }

// Server-side patterns.
var goServerPatterns = []*regexp.Regexp{
	// http.HandleFunc("/path", handler)
	regexp.MustCompile(`(?:http\.HandleFunc|HandleFunc|Handle)\(\s*"([^"]+)"\s*,\s*(\w+)`),
	// r.Get("/path", handler) — chi, echo, gin (uppercase and lowercase methods)
	regexp.MustCompile(`\.\s*(Get|Post|Put|Delete|Patch|Head|Options|GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\(\s*"([^"]+)"\s*,\s*(\w+)`),
}

// Client-side patterns.
var goClientPatterns = []*regexp.Regexp{
	// http.Get("url")
	regexp.MustCompile(`http\.(Get|Post|Head)\(\s*"([^"]+)"`),
	// http.NewRequest("METHOD", "url", ...)
	regexp.MustCompile(`http\.NewRequest(?:WithContext)?\([^,]*,\s*"(\w+)"\s*,\s*"([^"]+)"`),
}

func (GoMatcher) Match(source []byte) []Route {
	var result []Route
	src := string(source)

	// Server routes.
	for _, pat := range goServerPatterns {
		for _, m := range pat.FindAllStringSubmatch(src, -1) {
			var method, path, handler string
			if len(m) == 3 {
				// HandleFunc pattern: [full, path, handler]
				path = m[1]
				handler = m[2]
				method = "*"
			} else if len(m) == 4 {
				// Method pattern: [full, method, path, handler]
				method = normalizeMethod(m[1])
				path = m[2]
				handler = m[3]
			}
			if path != "" {
				result = append(result, Route{
					Method:    method,
					Path:      NormalizePath(path),
					RawPath:   path,
					Handler:   handler,
					Framework: "go",
					Side:      "server",
				})
			}
		}
	}

	// Client routes.
	for _, pat := range goClientPatterns {
		for _, m := range pat.FindAllStringSubmatch(src, -1) {
			var method, path string
			if len(m) == 3 {
				method = normalizeMethod(m[1])
				path = m[2]
			}
			if path != "" {
				result = append(result, Route{
					Method:    method,
					Path:      NormalizePath(path),
					RawPath:   path,
					Framework: "go",
					Side:      "client",
				})
			}
		}
	}

	return result
}

func normalizeMethod(m string) string {
	switch m {
	case "Get", "GET":
		return "GET"
	case "Post", "POST":
		return "POST"
	case "Put", "PUT":
		return "PUT"
	case "Delete", "DELETE":
		return "DELETE"
	case "Patch", "PATCH":
		return "PATCH"
	case "Head", "HEAD":
		return "HEAD"
	case "Options", "OPTIONS":
		return "OPTIONS"
	default:
		return "*"
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/routes/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/routes/
git commit -m "feat: add route extraction package with Go matcher (server + client patterns)"
```

---

### Task 4: Route Matchers — TypeScript, Python, Java

**Files:**
- Create: `internal/routes/match_typescript.go`
- Create: `internal/routes/match_python.go`
- Create: `internal/routes/match_java.go`
- Modify: `internal/routes/routes_test.go` — add test cases for each

**Step 1: Write failing tests**

Add to `internal/routes/routes_test.go`:

```go
func TestTypeScriptServerRoutes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		source string
		want   int
	}{
		{
			name:   "express",
			source: `app.get("/api/users", listUsers)` + "\n" + `app.post("/api/users", createUser)`,
			want:   2,
		},
		{
			name:   "fastify",
			source: `fastify.get("/health", healthHandler)`,
			want:   1,
		},
		{
			name:   "nest decorator",
			source: `@Get("/users/:id")` + "\n" + `@Post("/users")`,
			want:   2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := &TypeScriptMatcher{}
			routes := m.Match([]byte(tc.source))
			if len(routes) != tc.want {
				t.Errorf("got %d routes, want %d; routes=%v", len(routes), tc.want, routes)
			}
		})
	}
}

func TestTypeScriptClientRoutes(t *testing.T) {
	t.Parallel()

	source := `fetch("/api/users")` + "\n" + `axios.get("/api/items")` + "\n" + `axios.post("/api/orders", data)`
	m := &TypeScriptMatcher{}
	routes := m.Match([]byte(source))

	var clients int
	for _, r := range routes {
		if r.Side == "client" {
			clients++
		}
	}
	if clients != 3 {
		t.Errorf("got %d client routes, want 3", clients)
	}
}

func TestPythonServerRoutes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		source string
		want   int
	}{
		{
			name:   "flask",
			source: `@app.route("/api/users", methods=["GET"])`,
			want:   1,
		},
		{
			name:   "fastapi",
			source: `@router.get("/api/users/{user_id}")` + "\n" + `@app.post("/api/items")`,
			want:   2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := &PythonMatcher{}
			routes := m.Match([]byte(tc.source))
			if len(routes) != tc.want {
				t.Errorf("got %d routes, want %d; routes=%v", len(routes), tc.want, routes)
			}
		})
	}
}

func TestPythonClientRoutes(t *testing.T) {
	t.Parallel()

	source := `requests.get("https://api.example.com/users")` + "\n" + `httpx.post("/api/items", json=data)`
	m := &PythonMatcher{}
	routes := m.Match([]byte(source))

	var clients int
	for _, r := range routes {
		if r.Side == "client" {
			clients++
		}
	}
	if clients != 2 {
		t.Errorf("got %d client routes, want 2", clients)
	}
}

func TestJavaServerRoutes(t *testing.T) {
	t.Parallel()

	source := `@GetMapping("/api/users")` + "\n" + `@PostMapping("/api/users")` + "\n" + `@RequestMapping(value = "/api/items", method = RequestMethod.GET)`
	m := &JavaMatcher{}
	routes := m.Match([]byte(source))

	if len(routes) != 3 {
		t.Errorf("got %d routes, want 3; routes=%v", len(routes), routes)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/routes/ -v -run "TestTypeScript|TestPython|TestJava"`
Expected: FAIL — types not defined.

**Step 3: Write matchers**

Create `internal/routes/match_typescript.go`:

```go
package routes

import "regexp"

// TypeScriptMatcher extracts HTTP routes from TypeScript/JavaScript source code.
type TypeScriptMatcher struct{}

func init() { Register(&TypeScriptMatcher{}) }

func (TypeScriptMatcher) Language() string { return "typescript" }

var tsServerPatterns = []*regexp.Regexp{
	// app.get("/path", handler) — Express, Fastify, Hono
	regexp.MustCompile(`\.\s*(get|post|put|delete|patch|head|options)\(\s*["']([^"']+)["']\s*,\s*(\w+)?`),
	// @Get("/path"), @Post("/path") — NestJS decorators
	regexp.MustCompile(`@(Get|Post|Put|Delete|Patch|Head|Options)\(\s*["']([^"']+)["']\s*\)`),
}

var tsClientPatterns = []*regexp.Regexp{
	// fetch("/path") or fetch("url")
	regexp.MustCompile(`fetch\(\s*["']([^"']+)["']`),
	// axios.get("/path"), axios.post("/path")
	regexp.MustCompile(`axios\.\s*(get|post|put|delete|patch)\(\s*["']([^"']+)["']`),
	// $.ajax({url: "/path"})
	regexp.MustCompile(`\$\.ajax\(\s*\{[^}]*url:\s*["']([^"']+)["']`),
}

func (TypeScriptMatcher) Match(source []byte) []Route {
	var result []Route
	src := string(source)

	for _, pat := range tsServerPatterns {
		for _, m := range pat.FindAllStringSubmatch(src, -1) {
			var method, path, handler string
			if len(m) >= 3 {
				method = normalizeMethod(m[1])
				path = m[2]
			}
			if len(m) >= 4 {
				handler = m[3]
			}
			if path != "" {
				result = append(result, Route{
					Method:    method,
					Path:      NormalizePath(path),
					RawPath:   path,
					Handler:   handler,
					Framework: "typescript",
					Side:      "server",
				})
			}
		}
	}

	for _, pat := range tsClientPatterns {
		for _, m := range pat.FindAllStringSubmatch(src, -1) {
			var method, path string
			switch len(m) {
			case 2:
				method = "GET"
				path = m[1]
			case 3:
				method = normalizeMethod(m[1])
				path = m[2]
			}
			if path != "" {
				result = append(result, Route{
					Method:    method,
					Path:      NormalizePath(path),
					RawPath:   path,
					Framework: "typescript",
					Side:      "client",
				})
			}
		}
	}

	return result
}
```

Create `internal/routes/match_python.go`:

```go
package routes

import "regexp"

// PythonMatcher extracts HTTP routes from Python source code.
type PythonMatcher struct{}

func init() { Register(&PythonMatcher{}) }

func (PythonMatcher) Language() string { return "python" }

var pyServerPatterns = []*regexp.Regexp{
	// @app.route("/path", ...) — Flask
	regexp.MustCompile(`@\w+\.route\(\s*["']([^"']+)["']`),
	// @router.get("/path"), @app.post("/path") — FastAPI
	regexp.MustCompile(`@\w+\.\s*(get|post|put|delete|patch|head|options)\(\s*["']([^"']+)["']`),
}

var pyClientPatterns = []*regexp.Regexp{
	// requests.get("url"), httpx.post("url")
	regexp.MustCompile(`(?:requests|httpx|aiohttp)\.\s*(get|post|put|delete|patch)\(\s*["']([^"']+)["']`),
}

func (PythonMatcher) Match(source []byte) []Route {
	var result []Route
	src := string(source)

	for _, pat := range pyServerPatterns {
		for _, m := range pat.FindAllStringSubmatch(src, -1) {
			var method, path string
			switch len(m) {
			case 2:
				method = "*"
				path = m[1]
			case 3:
				method = normalizeMethod(m[1])
				path = m[2]
			}
			if path != "" {
				result = append(result, Route{
					Method:    method,
					Path:      NormalizePath(path),
					RawPath:   path,
					Framework: "python",
					Side:      "server",
				})
			}
		}
	}

	for _, pat := range pyClientPatterns {
		for _, m := range pat.FindAllStringSubmatch(src, -1) {
			if len(m) == 3 {
				result = append(result, Route{
					Method:    normalizeMethod(m[1]),
					Path:      NormalizePath(m[2]),
					RawPath:   m[2],
					Framework: "python",
					Side:      "client",
				})
			}
		}
	}

	return result
}
```

Create `internal/routes/match_java.go`:

```go
package routes

import "regexp"

// JavaMatcher extracts HTTP routes from Java source code.
type JavaMatcher struct{}

func init() { Register(&JavaMatcher{}) }

func (JavaMatcher) Language() string { return "java" }

var javaServerPatterns = []*regexp.Regexp{
	// @GetMapping("/path"), @PostMapping("/path")
	regexp.MustCompile(`@(Get|Post|Put|Delete|Patch)Mapping\(\s*["']?([^"')]+)["']?\s*\)`),
	// @RequestMapping(value = "/path", method = RequestMethod.GET)
	regexp.MustCompile(`@RequestMapping\([^)]*(?:value\s*=\s*)?["']([^"']+)["']`),
}

func (JavaMatcher) Match(source []byte) []Route {
	var result []Route
	src := string(source)

	for _, pat := range javaServerPatterns {
		for _, m := range pat.FindAllStringSubmatch(src, -1) {
			var method, path string
			switch len(m) {
			case 2:
				method = "*"
				path = m[1]
			case 3:
				method = normalizeMethod(m[1])
				path = m[2]
			}
			if path != "" {
				result = append(result, Route{
					Method:    method,
					Path:      NormalizePath(path),
					RawPath:   path,
					Framework: "java",
					Side:      "server",
				})
			}
		}
	}

	return result
}
```

**Step 4: Run tests**

Run: `go test ./internal/routes/ -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/routes/match_typescript.go internal/routes/match_python.go internal/routes/match_java.go internal/routes/routes_test.go
git commit -m "feat: add TypeScript, Python, Java route matchers"
```

---

### Task 5: Route Matchers — Rust, Ruby, C#

**Files:**
- Create: `internal/routes/match_rust.go`
- Create: `internal/routes/match_ruby.go`
- Create: `internal/routes/match_csharp.go`
- Modify: `internal/routes/routes_test.go` — add tests

**Step 1: Write failing tests**

Add to `internal/routes/routes_test.go`:

```go
func TestRustServerRoutes(t *testing.T) {
	t.Parallel()

	source := `#[get("/api/users")]` + "\n" + `.route("/api/items", web::post().to(create_item))`
	m := &RustMatcher{}
	routes := m.Match([]byte(source))
	if len(routes) != 2 {
		t.Errorf("got %d routes, want 2; routes=%v", len(routes), routes)
	}
}

func TestRubyServerRoutes(t *testing.T) {
	t.Parallel()

	source := `get '/api/users' do` + "\n" + `post '/api/items' do` + "\n" + `resources :orders`
	m := &RubyMatcher{}
	routes := m.Match([]byte(source))
	if len(routes) < 2 {
		t.Errorf("got %d routes, want >= 2; routes=%v", len(routes), routes)
	}
}

func TestCSharpServerRoutes(t *testing.T) {
	t.Parallel()

	source := `[HttpGet("/api/users")]` + "\n" + `[HttpPost("/api/users")]` + "\n" + `app.MapGet("/health", () => "ok")`
	m := &CSharpMatcher{}
	routes := m.Match([]byte(source))
	if len(routes) != 3 {
		t.Errorf("got %d routes, want 3; routes=%v", len(routes), routes)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/routes/ -v -run "TestRust|TestRuby|TestCSharp"`
Expected: FAIL

**Step 3: Write matchers**

Create `internal/routes/match_rust.go`:

```go
package routes

import "regexp"

type RustMatcher struct{}

func init() { Register(&RustMatcher{}) }

func (RustMatcher) Language() string { return "rust" }

var rustServerPatterns = []*regexp.Regexp{
	// #[get("/path")], #[post("/path")] — Rocket, Actix macros
	regexp.MustCompile(`#\[(get|post|put|delete|patch)\("([^"]+)"\)\]`),
	// .route("/path", web::get().to(handler))
	regexp.MustCompile(`\.route\("([^"]+)",\s*web::(get|post|put|delete|patch)\(\)`),
}

func (RustMatcher) Match(source []byte) []Route {
	var result []Route
	src := string(source)

	for _, pat := range rustServerPatterns {
		for _, m := range pat.FindAllStringSubmatch(src, -1) {
			var method, path string
			if len(m) == 3 {
				method = normalizeMethod(m[1])
				path = m[2]
			}
			if path != "" {
				result = append(result, Route{
					Method:    method,
					Path:      NormalizePath(path),
					RawPath:   path,
					Framework: "rust",
					Side:      "server",
				})
			}
		}
	}
	return result
}
```

Create `internal/routes/match_ruby.go`:

```go
package routes

import "regexp"

type RubyMatcher struct{}

func init() { Register(&RubyMatcher{}) }

func (RubyMatcher) Language() string { return "ruby" }

var rubyServerPatterns = []*regexp.Regexp{
	// get '/path', post '/path' — Sinatra, Rails routes
	regexp.MustCompile(`(?:^|\s)(get|post|put|delete|patch)\s+['"]([^'"]+)['"]`),
	// resources :name
	regexp.MustCompile(`resources\s+:(\w+)`),
}

func (RubyMatcher) Match(source []byte) []Route {
	var result []Route
	src := string(source)

	for _, pat := range rubyServerPatterns {
		for _, m := range pat.FindAllStringSubmatch(src, -1) {
			var method, path string
			switch {
			case len(m) == 3:
				method = normalizeMethod(m[1])
				path = m[2]
			case len(m) == 2:
				// resources :name → /name
				method = "*"
				path = "/" + m[1]
			}
			if path != "" {
				result = append(result, Route{
					Method:    method,
					Path:      NormalizePath(path),
					RawPath:   path,
					Framework: "ruby",
					Side:      "server",
				})
			}
		}
	}
	return result
}
```

Create `internal/routes/match_csharp.go`:

```go
package routes

import "regexp"

type CSharpMatcher struct{}

func init() { Register(&CSharpMatcher{}) }

func (CSharpMatcher) Language() string { return "csharp" }

var csharpServerPatterns = []*regexp.Regexp{
	// [HttpGet("/path")], [HttpPost("/path")]
	regexp.MustCompile(`\[Http(Get|Post|Put|Delete|Patch)\("([^"]+)"\)\]`),
	// app.MapGet("/path", handler)
	regexp.MustCompile(`\.Map(Get|Post|Put|Delete|Patch)\("([^"]+)"`),
}

func (CSharpMatcher) Match(source []byte) []Route {
	var result []Route
	src := string(source)

	for _, pat := range csharpServerPatterns {
		for _, m := range pat.FindAllStringSubmatch(src, -1) {
			if len(m) == 3 {
				result = append(result, Route{
					Method:    normalizeMethod(m[1]),
					Path:      NormalizePath(m[2]),
					RawPath:   m[2],
					Framework: "csharp",
					Side:      "server",
				})
			}
		}
	}
	return result
}
```

**Step 4: Run all route tests**

Run: `go test ./internal/routes/ -v`
Expected: All PASS

**Step 5: Commit**

```bash
git add internal/routes/match_rust.go internal/routes/match_ruby.go internal/routes/match_csharp.go internal/routes/routes_test.go
git commit -m "feat: add Rust, Ruby, C# route matchers"
```

---

### Task 6: Graph Extensions — Layer and Route Vertices

**Files:**
- Modify: `internal/codegraph/graph_build.go` — add `BuildCrossLanguageGraph()` function
- Modify: `internal/codegraph/cypher_batch.go` — extend `matchKey()` for Layer and Route labels
- Create: `internal/codegraph/cross_build_test.go`

**Step 1: Write the failing test**

Create `internal/codegraph/cross_build_test.go`:

```go
package codegraph

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/polyglot"
	"github.com/anatolykoptev/go-code/internal/routes"
)

func TestBuildLayerVertices(t *testing.T) {
	t.Parallel()

	layers := []polyglot.Layer{
		{Name: "backend", Role: "server", Language: "go", RootDir: "backend"},
		{Name: "frontend", Role: "client", Language: "typescript", RootDir: "frontend"},
	}

	vertices, edges := buildCrossLanguageGraph(layers, nil, nil)

	var layerCount int
	for _, v := range vertices {
		if v.Label == "Layer" {
			layerCount++
		}
	}
	if layerCount != 2 {
		t.Errorf("got %d Layer vertices, want 2", layerCount)
	}
	_ = edges // BELONGS_TO edges tested separately
}

func TestBuildRouteVerticesAndEdges(t *testing.T) {
	t.Parallel()

	routeList := []routes.Route{
		{Method: "GET", Path: "/api/users", Handler: "listUsers", Framework: "go", File: "handler.go", Side: "server"},
		{Method: "GET", Path: "/api/users", Handler: "fetchUsers", Framework: "typescript", File: "api.ts", Side: "client"},
	}

	// fileToLayer maps file paths to layer root dirs.
	fileToLayer := map[string]string{
		"handler.go": "backend",
		"api.ts":     "frontend",
	}

	vertices, edges := buildCrossLanguageGraph(nil, routeList, fileToLayer)

	var routeCount int
	for _, v := range vertices {
		if v.Label == "Route" {
			routeCount++
		}
	}
	// Same path → same Route vertex (deduplicated).
	if routeCount != 1 {
		t.Errorf("got %d Route vertices, want 1 (dedup by method+path)", routeCount)
	}

	var handles, fetches int
	for _, e := range edges {
		switch e.EdgeLabel {
		case "HANDLES":
			handles++
		case "FETCHES":
			fetches++
		}
	}
	if handles != 1 {
		t.Errorf("got %d HANDLES edges, want 1", handles)
	}
	if fetches != 1 {
		t.Errorf("got %d FETCHES edges, want 1", fetches)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/codegraph/ -v -run "TestBuildLayer|TestBuildRoute"`
Expected: FAIL — `buildCrossLanguageGraph` not defined.

**Step 3: Write implementation**

Add to `internal/codegraph/graph_build.go` (at the end of file):

```go
// buildCrossLanguageGraph constructs Layer and Route vertices with their edges.
func buildCrossLanguageGraph(layers []polyglot.Layer, routeList []routes.Route, fileToLayer map[string]string) ([]vertexData, []edgeData) {
	var vertices []vertexData
	var edges []edgeData

	// Layer vertices.
	for _, l := range layers {
		vertices = append(vertices, vertexData{
			Label: "Layer",
			Key:   l.Name,
			Props: map[string]string{
				"name":     l.Name,
				"role":     l.Role,
				"language": l.Language,
				"root_dir": l.RootDir,
			},
		})
	}

	// Route vertices (deduplicated by method+path).
	routeKeys := make(map[string]bool)
	for _, r := range routeList {
		key := r.Method + ":" + r.Path
		if !routeKeys[key] {
			routeKeys[key] = true
			vertices = append(vertices, vertexData{
				Label: "Route",
				Key:   key,
				Props: map[string]string{
					"method":    r.Method,
					"path":      r.Path,
					"framework": r.Framework,
				},
			})
		}
	}

	// BELONGS_TO edges (File→Layer) — added during index phase.
	// HANDLES / FETCHES edges (Symbol→Route).
	for _, r := range routeList {
		edgeLabel := "HANDLES"
		if r.Side == "client" {
			edgeLabel = "FETCHES"
		}
		routeKey := r.Method + ":" + r.Path
		symbolKey := r.Handler + ":" + r.File
		edges = append(edges, edgeData{
			FromLabel: "Symbol",
			FromKey:   symbolKey,
			ToLabel:   "Route",
			ToKey:     routeKey,
			EdgeLabel: edgeLabel,
			Props:     map[string]string{},
		})
	}

	return vertices, edges
}
```

Add imports for `polyglot` and `routes` packages at the top of `graph_build.go`.

Extend `matchKey()` in `cypher_batch.go` to handle Layer and Route labels:

```go
// In the switch block of matchKey():
case "Layer":
    return fmt.Sprintf("name: '%s'", escapeCypher(key))
case "Route":
    // key format: "METHOD:/path"
    parts := strings.SplitN(key, ":", 2)
    if len(parts) == 2 {
        return fmt.Sprintf("method: '%s', path: '%s'", escapeCypher(parts[0]), escapeCypher(parts[1]))
    }
    return fmt.Sprintf("path: '%s'", escapeCypher(key))
```

**Step 4: Run tests**

Run: `go test ./internal/codegraph/ -v -run "TestBuildLayer|TestBuildRoute"`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/codegraph/graph_build.go internal/codegraph/cypher_batch.go internal/codegraph/cross_build_test.go
git commit -m "feat: add Layer/Route vertex and HANDLES/FETCHES edge construction"
```

---

### Task 7: Wire Polyglot + Routes into IndexRepo

**Files:**
- Modify: `internal/codegraph/index.go` — add polyglot and route phases after edge insertion
- Modify: `internal/codegraph/index.go` — add BELONGS_TO edges for File→Layer mapping

**Step 1: Modify IndexRepo**

In `internal/codegraph/index.go`, after the existing edge insertion (around line 88), add:

```go
// Phase: polyglot detection.
structure := polyglot.DetectStructure(allFiles)
var routeList []routes.Route

// Phase: route extraction — read source for route-containing files.
for _, f := range allFiles {
    if f.Language == "" || f.Language == "c" || f.Language == "cpp" {
        continue
    }
    src, err := os.ReadFile(f.Path)
    if err != nil {
        continue
    }
    fileRoutes := routes.ExtractAll(f.Language, src)
    for i := range fileRoutes {
        fileRoutes[i].File = relPath(f.Path, root)
    }
    routeList = append(routeList, fileRoutes...)
}

// Classify layer roles from source samples.
layerSources := make(map[string][]string)
for _, f := range allFiles {
    if f.Language == "" {
        continue
    }
    for i := range structure.Layers {
        l := &structure.Layers[i]
        if l.RootDir == "" || strings.HasPrefix(f.RelPath, l.RootDir+"/") || strings.HasPrefix(f.RelPath, l.RootDir) {
            // Read first 500 bytes for role classification.
            if src, err := os.ReadFile(f.Path); err == nil {
                limit := 500
                if len(src) < limit {
                    limit = len(src)
                }
                layerSources[l.Name] = append(layerSources[l.Name], string(src[:limit]))
            }
            break
        }
    }
}
for i := range structure.Layers {
    l := &structure.Layers[i]
    if samples, ok := layerSources[l.Name]; ok {
        l.Role = polyglot.ClassifyLayerRole(samples)
    }
}

// Build file→layer mapping.
fileToLayer := make(map[string]string)
for _, f := range allFiles {
    for _, l := range structure.Layers {
        if l.RootDir == "" || strings.HasPrefix(f.RelPath, l.RootDir+"/") || strings.HasPrefix(f.RelPath, l.RootDir) {
            fileToLayer[relPath(f.Path, root)] = l.Name
            break
        }
    }
}

// Build cross-language graph.
crossVertices, crossEdges := buildCrossLanguageGraph(structure.Layers, routeList, fileToLayer)

// Insert cross-language vertices.
if len(crossVertices) > 0 {
    if err := insertBatches(ctx, store, gname, cfg.BatchSize, crossVertices, buildVertexBatch); err != nil {
        log.Printf("codegraph: cross-language vertices: %v", err)
    }
}

// Insert BELONGS_TO edges (File→Layer).
for file, layerName := range fileToLayer {
    cypher := buildSingleEdge(edgeData{
        FromLabel: "File",
        FromKey:   file,
        ToLabel:   "Layer",
        ToKey:     layerName,
        EdgeLabel: "BELONGS_TO",
        Props:     map[string]string{},
    })
    if cypher != "" {
        _ = store.ExecCypherWrite(ctx, gname, cypher)
    }
}

// Insert cross-language edges (HANDLES/FETCHES).
if len(crossEdges) > 0 {
    if err := insertEdgeBatches(ctx, store, gname, cfg.BatchSize, crossEdges); err != nil {
        log.Printf("codegraph: cross-language edges: %v", err)
    }
}
```

Add necessary imports: `"os"`, `"log"`, `"strings"`, `polyglot` and `routes` packages.

**Step 2: Run all codegraph tests**

Run: `go test ./internal/codegraph/ -v`
Expected: All PASS (existing + new tests)

**Step 3: Run full test suite**

Run: `go test ./... 2>&1 | tail -20`
Expected: All PASS

**Step 4: Commit**

```bash
git add internal/codegraph/index.go
git commit -m "feat: wire polyglot detection and route extraction into IndexRepo"
```

---

### Task 8: New Cypher Templates

**Files:**
- Modify: `internal/codegraph/templates.go` — add 4 new templates
- Modify: `internal/codegraph/templates_test.go` — update template count, add render tests

**Step 1: Add templates**

Add to the `templates` map in `internal/codegraph/templates.go`:

```go
"api_routes": {
    ID:          "api_routes",
    Description: "Find HTTP routes with their handler symbols, optionally filtered by path",
    Params:      []string{"path"},
    Cypher:      "MATCH (s:Symbol)-[r:HANDLES|FETCHES]->(route:Route) WHERE route.path CONTAINS '{path}' RETURN s.name, s.file, type(r) AS relation, route.method, route.path",
    Cols:        5,
},
"cross_calls": {
    ID:          "cross_calls",
    Description: "Find backend handlers and frontend callers connected through shared HTTP routes",
    Params:      []string{"path"},
    Cypher:      "MATCH (server:Symbol)-[:HANDLES]->(route:Route)<-[:FETCHES]-(client:Symbol) WHERE route.path CONTAINS '{path}' RETURN server.name, server.file, route.method, route.path, client.name, client.file",
    Cols:        6,
},
"layer_deps": {
    ID:          "layer_deps",
    Description: "Show dependencies between architectural layers via function calls",
    Params:      []string{},
    Cypher:      "MATCH (f1:File)-[:BELONGS_TO]->(l1:Layer), (f2:File)-[:BELONGS_TO]->(l2:Layer), (s1:Symbol)<-[:CONTAINS]-(f1), (s1)-[:CALLS]->(s2), (s2)<-[:CONTAINS]-(f2) WHERE l1.name <> l2.name RETURN l1.name, l2.name, count(*) AS connections ORDER BY connections DESC",
    Cols:        3,
},
"polyglot_overview": {
    ID:          "polyglot_overview",
    Description: "Show repository structure with layers, languages, and route counts",
    Params:      []string{},
    Cypher:      "MATCH (l:Layer)<-[:BELONGS_TO]-(f:File) OPTIONAL MATCH (f)-[:CONTAINS]->(s:Symbol)-[:HANDLES]->(r:Route) RETURN l.name, l.role, l.language, count(DISTINCT f) AS files, count(DISTINCT r) AS routes",
    Cols:        5,
},
```

**Step 2: Update tests**

In `internal/codegraph/templates_test.go`:

1. Change `TestTemplateCount` constant from 10 to 14.

2. Add test cases to `TestTemplateRender`:

```go
{
    id:     "api_routes",
    params: map[string]string{"path": "/api/users"},
    want:   "/api/users",
},
{
    id:     "cross_calls",
    params: map[string]string{"path": "/api"},
    want:   "/api",
},
{
    id:     "layer_deps",
    params: map[string]string{},
    want:   "BELONGS_TO",
},
{
    id:     "polyglot_overview",
    params: map[string]string{},
    want:   "Layer",
},
```

**Step 3: Run template tests**

Run: `go test ./internal/codegraph/ -v -run TestTemplate`
Expected: All PASS

**Step 4: Also add "path" to templateDefaults**

Verify `templateDefaults` in `templates.go` already has `"path": ""`. It does from the previous fix.

**Step 5: Commit**

```bash
git add internal/codegraph/templates.go internal/codegraph/templates_test.go
git commit -m "feat: add 4 cross-language Cypher templates (api_routes, cross_calls, layer_deps, polyglot_overview)"
```

---

### Task 9: Update dep_graph Tool — cross_language Parameter

**Files:**
- Modify: `cmd/go-code/tool_dep_graph.go` — add `CrossLanguage` field to input
- Modify: `internal/analyze/analyze.go` — pass through to dep graph builder

**Step 1: Add field to DepGraphInput**

In `cmd/go-code/tool_dep_graph.go`, add to `DepGraphInput`:

```go
CrossLanguage bool `json:"cross_language,omitempty" jsonschema_description:"Include cross-language API route connections in the dependency graph"`
```

**Step 2: Wire through to analyze package**

In the handler, pass `CrossLanguage` to `analyze.BuildDepGraph()`. The analyze package should check this flag and, if true, include Route connections in the output.

This requires reading `internal/analyze/analyze.go` to see how `BuildDepGraph` currently works and extending it to optionally include cross-language edges.

**Step 3: Run tests and commit**

Run: `go test ./... 2>&1 | tail -20`
Expected: All PASS

```bash
git add cmd/go-code/tool_dep_graph.go internal/analyze/analyze.go
git commit -m "feat: add cross_language parameter to dep_graph tool"
```

---

### Task 10: Update repo_analyze — Polyglot Section

**Files:**
- Modify: `cmd/go-code/tool_repo_analyze.go` — detect polyglot and add cross-language context
- Modify: `internal/analyze/analyze.go` — add polyglot info to LLM context when deep

**Step 1: Add polyglot detection to deep mode**

In `handleDeepMode()`, after ingestion and before LLM analysis:
1. Call `polyglot.DetectStructure(files)` on the ingested files
2. If `structure.IsPolyglot()`, add a "Cross-Language Architecture" section to the LLM context
3. Include: layer names, roles, languages, file counts

**Step 2: Run tests and commit**

Run: `go test ./... 2>&1 | tail -20`
Expected: All PASS

```bash
git add cmd/go-code/tool_repo_analyze.go internal/analyze/analyze.go
git commit -m "feat: add polyglot detection section to repo_analyze deep mode"
```

---

### Task 11: Build, Deploy, Smoke Test

**Files:** None (deployment only)

**Step 1: Run full test suite**

Run: `go test ./... 2>&1 | tail -20`
Expected: All PASS

**Step 2: Build and deploy**

```bash
cd /home/krolik/deploy/krolik-server
docker compose build --no-cache go-code
docker compose up -d --no-deps --force-recreate go-code
```

**Step 3: Verify startup**

```bash
sleep 3 && docker logs go-code --tail 5
```
Expected: `tools registered count=8`, `listening addr=:8897`

**Step 4: Smoke test — polyglot overview**

Find or use a polyglot repo. Test with a monorepo that has Go + TypeScript:

```bash
curl -s -H "Content-Type: application/json" --max-time 120 \
  http://127.0.0.1:8897/mcp \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"code_graph","arguments":{"repo":"<polyglot-repo>","query":"show the polyglot structure of this repo","refresh":true}}}' \
  | grep "^data:" | sed 's/^data: //' | python3 -c "import sys,json; r=json.load(sys.stdin); print(r.get('result',{}).get('content',[{}])[0].get('text','')[:1000])"
```

**Step 5: Smoke test — cross-language queries**

```bash
# api_routes
curl -s -H "Content-Type: application/json" --max-time 60 \
  http://127.0.0.1:8897/mcp \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"code_graph","arguments":{"repo":"<polyglot-repo>","query":"what API routes are defined?"}}}' \
  | grep "^data:" | head -1

# cross_calls
curl -s -H "Content-Type: application/json" --max-time 60 \
  http://127.0.0.1:8897/mcp \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"code_graph","arguments":{"repo":"<polyglot-repo>","query":"which frontend functions call backend API routes?"}}}' \
  | grep "^data:" | head -1
```

**Step 6: Commit any fixes found during smoke testing**

---

### Task 12: Update Documentation

**Files:**
- Modify: `CLAUDE.md` — update codegraph section with new vertex/edge types and templates
- Modify: `docs/ROADMAP.md` — mark Phase 4.3 complete

**Step 1: Update CLAUDE.md**

In the Architecture section under `codegraph/`, add:
```
    cross_build.go       — Cross-language Layer/Route vertex and edge construction
```

Add to the codegraph description:
```
    templates.go       — 14 Cypher query templates (who_calls, api_routes, cross_calls, etc.)
```

In Environment Variables, no new vars needed.

**Step 2: Update ROADMAP.md**

Mark Phase 4.3 items as complete:
```
### 4.3 Cross-language analysis ✅

**Status**: Complete (2026-02-28).

- [x] Detect polyglot repos (manifest scanning, layer grouping, role classification)
- [x] Map API boundaries (7 language matchers, server + client patterns, LLM fallback)
- [x] Unified dependency graph across languages (Layer/Route AGE vertices, 4 new Cypher templates)
```

**Step 3: Commit**

```bash
git add CLAUDE.md docs/ROADMAP.md
git commit -m "docs: mark Phase 4.3 complete, update architecture docs"
```

**Step 4: Tag release**

```bash
git tag v1.9.0
git push origin main --tags
```
