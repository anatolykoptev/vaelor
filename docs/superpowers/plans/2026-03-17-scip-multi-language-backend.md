# SCIP Multi-Language Type-Aware Backend

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add SCIP backend so all 11 supported languages get type-aware analysis (enhanced tier), not just Go.

**Architecture:** SCIP indexers (scip-go, scip-typescript, scip-python, etc.) run as subprocesses, producing `index.scip` protobuf files. A new `internal/scip` package reads these files using Sourcegraph's Go bindings (`github.com/sourcegraph/scip/bindings/go/scip`) and converts SCIP occurrences/relationships into `CallEdge` structs that merge with the existing tree-sitter call graph. Language detection via `polyglot.DetectStructure()` determines which indexer to invoke. go/types remains the primary backend for Go (faster, no external deps); SCIP activates for non-Go languages or as fallback.

**Tech Stack:** Go 1.26, `github.com/sourcegraph/scip` (protobuf + Go bindings), SCIP indexers as Docker-bundled binaries (scip-typescript via Node, scip-python via Node, scip-go via Go binary).

**Key constraint:** go-code runs in Docker (alpine:3.21, read_only, cap_drop ALL, 512MB memory limit). Indexers must be installed in the Docker image. The `/host` volume is read-only. Index files go to `/tmp`.

---

## File Structure

### New files

| File | Responsibility |
|------|---------------|
| `internal/scip/index.go` | Core: read `index.scip` → extract symbols, references, call edges |
| `internal/scip/index_test.go` | Tests with fixture index.scip files |
| `internal/scip/indexer.go` | Run SCIP indexer subprocess (scip-typescript, scip-python, etc.) |
| `internal/scip/indexer_test.go` | Tests for indexer detection and invocation |
| `internal/scip/detect.go` | Map language → indexer command + args |
| `internal/scip/detect_test.go` | Tests for language→indexer mapping |
| `internal/scip/convert.go` | Convert SCIP data → `[]goanalysis.TypedEdge` (reuse existing bridge) |
| `internal/scip/convert_test.go` | Tests for SCIP→TypedEdge conversion |

### Modified files

| File | Change |
|------|--------|
| `internal/callgraph/repo.go` | Add `trySCIPResolution()` after go/types, before return |
| `internal/tier/tier.go` | Add `SCIP bool` to `Backends` struct, update `detect()` |
| `go.mod` | Add `github.com/sourcegraph/scip` dependency |
| `Dockerfile` | Install Node.js + scip-typescript + scip-python in runtime stage |

---

## Chunk 1: SCIP Go Dependency + Protobuf Reader

### Task 1: Add SCIP dependency and create index reader

**Files:**
- Create: `internal/scip/index.go`
- Create: `internal/scip/index_test.go`
- Modify: `go.mod`

- [ ] **Step 0: Add dependency and verify SCIP API surface**

```bash
cd $REPO_ROOT
go get github.com/sourcegraph/scip/bindings/go/scip@latest
go doc github.com/sourcegraph/scip/bindings/go/scip | head -100
go doc github.com/sourcegraph/scip/bindings/go/scip IndexVisitor
go doc github.com/sourcegraph/scip/bindings/go/scip Document
go doc github.com/sourcegraph/scip/bindings/go/scip Occurrence
```

**CRITICAL:** Verify exact type names, field names, and import paths before writing any code. The proto types (`Document`, `Occurrence`, `Metadata`, `SymbolInformation`) may live in the same `scip` package or a separate generated sub-package. Adjust ALL code in Tasks 1-2 based on verified API. Expected output: IndexVisitor struct with ParseStreaming method, Document with Occurrences and Symbols fields.

- [ ] **Step 1: Verify dependency added**

Run: `cd $REPO_ROOT && grep sourcegraph/scip go.mod`
Expected: go.mod updated with scip dependency

- [ ] **Step 2: Write failing test for ReadIndex**

```go
// internal/scip/index_test.go
package scip_test

import (
	"os"
	"path/filepath"
	"testing"

	gocodescip "github.com/anatolykoptev/go-code/internal/scip"
)

func TestReadIndex_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "index.scip")
	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	idx, err := gocodescip.ReadIndex(path)
	if err != nil {
		t.Fatalf("ReadIndex should handle empty file: %v", err)
	}
	if idx.DocumentCount() != 0 {
		t.Errorf("expected 0 documents, got %d", idx.DocumentCount())
	}
}

func TestReadIndex_NotFound(t *testing.T) {
	_, err := gocodescip.ReadIndex("/nonexistent/index.scip")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd $REPO_ROOT && go test ./internal/scip/ -v -run TestReadIndex`
Expected: FAIL — package doesn't exist

- [ ] **Step 4: Write minimal implementation**

```go
// internal/scip/index.go
package scip

import (
	"context"
	"fmt"
	"os"

	scipb "github.com/sourcegraph/scip/bindings/go/scip"
	scippb "github.com/sourcegraph/scip/bindings/go/scip/proto"
)

// Index holds parsed SCIP data: documents with symbols and occurrences.
type Index struct {
	Documents []*scippb.Document
	Metadata  *scippb.Metadata
}

// ReadIndex reads and parses an index.scip protobuf file.
// Returns an empty Index (not error) for empty files.
func ReadIndex(path string) (*Index, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open index: %w", err)
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat index: %w", err)
	}
	if info.Size() == 0 {
		return &Index{}, nil
	}

	idx := &Index{}
	visitor := &scipb.IndexVisitor{
		VisitMetadata: func(m *scippb.Metadata) {
			idx.Metadata = m
		},
		VisitDocument: func(d *scippb.Document) {
			idx.Documents = append(idx.Documents, d)
		},
	}

	if err := visitor.ParseStreaming(context.Background(), f); err != nil {
		return nil, fmt.Errorf("parse index.scip: %w", err)
	}

	return idx, nil
}

// DocumentCount returns the number of source files in the index.
func (idx *Index) DocumentCount() int {
	return len(idx.Documents)
}
```

**Note to implementer:** The exact import paths for the SCIP protobuf types may differ. Check `github.com/sourcegraph/scip/bindings/go/scip` — the generated protobuf types are in the same package (not a separate `proto` sub-package). The `Document`, `Metadata`, `Occurrence`, `SymbolInformation` types are all in `scip` package directly. The `IndexVisitor` struct and `ParseStreaming` method are also in the `scip` package. Adjust imports accordingly after running `go doc github.com/sourcegraph/scip/bindings/go/scip`.

- [ ] **Step 5: Run test to verify it passes**

Run: `cd $REPO_ROOT && go test ./internal/scip/ -v -run TestReadIndex`
Expected: PASS

- [ ] **Step 6: Run go mod tidy and commit**

```bash
cd $REPO_ROOT
go mod tidy
go mod vendor
git add internal/scip/ go.mod go.sum vendor/
git commit -m "feat(scip): add SCIP index reader with streaming protobuf parser"
```

---

## Chunk 2: SCIP → TypedEdge Converter

### Task 2: Convert SCIP occurrences to TypedEdge format

**Files:**
- Create: `internal/scip/convert.go`
- Create: `internal/scip/convert_test.go`

**Context:** SCIP stores occurrences per-document. Each occurrence has a `symbol` string ID and `symbol_roles` bitmask. Role `1` = Definition, role `0` = Reference. We build call edges by finding reference occurrences inside function bodies and resolving them to their definition locations.

- [ ] **Step 1: Write failing test for ConvertToEdges**

```go
// internal/scip/convert_test.go
package scip_test

import (
	"testing"

	gocodescip "github.com/anatolykoptev/go-code/internal/scip"
	"github.com/anatolykoptev/go-code/internal/goanalysis"
)

func TestConvertToEdges_SimpleCall(t *testing.T) {
	// Simulate: main.go has main() calling greet()
	// Both defined in same file
	idx := gocodescip.BuildTestIndex(
		gocodescip.TestDoc("main.go",
			gocodescip.DefOccurrence("pkg.main.", 6, 5, 9),       // func main at line 7
			gocodescip.DefOccurrence("pkg.greet.", 2, 5, 10),     // func greet at line 3
			gocodescip.RefOccurrence("pkg.greet.", 7, 4, 9),      // call greet() at line 8
		),
	)

	edges := gocodescip.ConvertToEdges(idx)

	if len(edges) == 0 {
		t.Fatal("expected at least one edge")
	}

	found := false
	for _, e := range edges {
		if e.CallerName == "main" && e.CalleeName == "greet" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected main -> greet edge, got: %v", edges)
	}
}

func TestConvertToEdges_Empty(t *testing.T) {
	idx := &gocodescip.Index{}
	edges := gocodescip.ConvertToEdges(idx)
	if len(edges) != 0 {
		t.Errorf("expected 0 edges for empty index, got %d", len(edges))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd $REPO_ROOT && go test ./internal/scip/ -v -run TestConvertToEdges`
Expected: FAIL — functions not defined

- [ ] **Step 3: Write implementation**

```go
// internal/scip/convert.go
package scip

import (
	"sort"
	"strings"

	"github.com/anatolykoptev/go-code/internal/goanalysis"
)

// ConvertToEdges converts SCIP index data into TypedEdge format compatible
// with the existing callgraph merge pipeline.
//
// Algorithm:
// 1. Build symbol→definition map (file, line, name) from all definition occurrences
// 2. Build file→function-ranges map (which function spans which lines)
// 3. For each reference occurrence, find enclosing function (caller) and
//    resolve symbol to definition (callee)
func ConvertToEdges(idx *Index) []goanalysis.TypedEdge {
	if len(idx.Documents) == 0 {
		return nil
	}

	defs := buildDefinitionMap(idx)
	funcs := buildFunctionRanges(idx, defs)
	return extractCallEdges(idx, defs, funcs)
}

// symbolDef holds the definition location for a SCIP symbol.
type symbolDef struct {
	Name string
	File string
	Line uint32
	Pkg  string
}

// funcRange describes a function's line span in a file.
type funcRange struct {
	Name      string
	File      string
	StartLine uint32
	EndLine   uint32
}

func buildDefinitionMap(idx *Index) map[string]*symbolDef {
	defs := make(map[string]*symbolDef)
	for _, doc := range idx.Documents {
		for _, occ := range doc.Occurrences {
			if occ.Symbol == "" || isLocalSymbol(occ.Symbol) {
				continue
			}
			if isDefinition(occ.SymbolRoles) {
				line := occurrenceLine(occ)
				defs[occ.Symbol] = &symbolDef{
					Name: extractSymbolName(occ.Symbol),
					File: doc.RelativePath,
					Line: line,
					Pkg:  extractPackage(occ.Symbol),
				}
			}
		}
	}
	return defs
}

func buildFunctionRanges(idx *Index, defs map[string]*symbolDef) []funcRange {
	// Collect function definitions per file from definition occurrences.
	fileRanges := make(map[string][]funcRange)
	for sym, def := range defs {
		if !isFunctionSymbol(sym) {
			continue
		}
		fileRanges[def.File] = append(fileRanges[def.File], funcRange{
			Name:      def.Name,
			File:      def.File,
			StartLine: def.Line,
		})
	}

	// Sort by StartLine per file, then set EndLine = next func start - 1.
	var result []funcRange
	for _, frs := range fileRanges {
		sort.Slice(frs, func(i, j int) bool { return frs[i].StartLine < frs[j].StartLine })
		for i := range frs {
			if i+1 < len(frs) {
				frs[i].EndLine = frs[i+1].StartLine - 1
			} else {
				frs[i].EndLine = frs[i].StartLine + 10000 // last func extends to EOF
			}
		}
		result = append(result, frs...)
	}
	return result
}

func extractCallEdges(idx *Index, defs map[string]*symbolDef, funcs []funcRange) []goanalysis.TypedEdge {
	var edges []goanalysis.TypedEdge
	for _, doc := range idx.Documents {
		fileFuncs := filterByFile(funcs, doc.RelativePath)
		for _, occ := range doc.Occurrences {
			if occ.Symbol == "" || isLocalSymbol(occ.Symbol) {
				continue
			}
			if isDefinition(occ.SymbolRoles) {
				continue // skip definitions, we want references (calls)
			}
			callee, ok := defs[occ.Symbol]
			if !ok {
				continue
			}
			line := occurrenceLine(occ)
			caller := findEnclosingFunc(fileFuncs, line)
			if caller == nil {
				continue // reference outside any function
			}
			edges = append(edges, goanalysis.TypedEdge{
				CallerName: caller.Name,
				CallerFile: caller.File,
				CallerLine: caller.StartLine,
				CalleeName: callee.Name,
				CalleeFile: callee.File,
				CalleePkg:  callee.Pkg,
				Line:       line,
			})
		}
	}
	return edges
}

func findEnclosingFunc(funcs []funcRange, line uint32) *funcRange {
	var best *funcRange
	for i := range funcs {
		f := &funcs[i]
		if line >= f.StartLine && line <= f.EndLine {
			if best == nil || f.StartLine > best.StartLine {
				best = f
			}
		}
	}
	return best
}

func filterByFile(funcs []funcRange, file string) []funcRange {
	var result []funcRange
	for _, f := range funcs {
		if f.File == file {
			result = append(result, f)
		}
	}
	return result
}

// isDefinition checks if SymbolRoles includes the Definition bit (0x1).
func isDefinition(roles int32) bool {
	return roles&0x1 != 0
}

// isLocalSymbol checks if a SCIP symbol is file-local (starts with "local ").
func isLocalSymbol(sym string) bool {
	return strings.HasPrefix(sym, "local ")
}

// occurrenceLine extracts the 0-indexed start line from SCIP range encoding.
// SCIP ranges: [startLine, startChar, endChar] (single-line) or
//              [startLine, startChar, endLine, endChar] (multi-line).
func occurrenceLine(occ interface{ GetRange() []int32 }) uint32 {
	r := occ.GetRange()
	if len(r) >= 1 {
		return uint32(r[0]) + 1 // SCIP is 0-indexed, we use 1-indexed
	}
	return 0
}

// extractSymbolName extracts the short function/method name from a SCIP symbol string.
// SCIP format: "scip-{lang} {manager} {package} {version} {descriptor}"
// Descriptor ends with function name followed by "." or "()" or similar.
func extractSymbolName(sym string) string {
	// Take last descriptor segment
	parts := strings.Fields(sym)
	if len(parts) == 0 {
		return sym
	}
	last := parts[len(parts)-1]
	// Remove trailing punctuation: ".", "()", "#"
	last = strings.TrimRight(last, ".()#")
	// If contains path separator, take last segment
	if idx := strings.LastIndex(last, "/"); idx >= 0 {
		last = last[idx+1:]
	}
	return last
}

// extractPackage extracts the package path from a SCIP symbol string.
func extractPackage(sym string) string {
	parts := strings.Fields(sym)
	if len(parts) >= 3 {
		return parts[2] // package is typically the 3rd field
	}
	return ""
}

// isFunctionSymbol checks if a SCIP symbol represents a function or method.
// Heuristic: ends with "()" descriptor suffix.
func isFunctionSymbol(sym string) bool {
	return strings.HasSuffix(sym, "().") || strings.HasSuffix(sym, "()")
}
```

**Note to implementer:** The SCIP symbol format and occurrence type assertions depend on the exact protobuf-generated types. After adding the dependency, run `go doc` on the scip package to verify field names. The `Occurrence` type likely has `Range` as `[]int32` field (not a method). Adjust `occurrenceLine` accordingly. Also verify `SymbolRoles` is `int32` — it might be an enum type.

- [ ] **Step 4: Write test helpers for building test indexes**

```go
// Add to internal/scip/index.go or a separate internal/scip/testutil.go
// These are exported for use in tests.

// BuildTestIndex creates an Index from test documents.
func BuildTestIndex(docs ...*Document) *Index {
	// ... construct Index with given documents
}

// TestDoc creates a test document with occurrences.
func TestDoc(path string, occs ...testOccurrence) *Document { ... }

// DefOccurrence creates a definition occurrence.
func DefOccurrence(symbol string, line, startCol, endCol int) testOccurrence { ... }

// RefOccurrence creates a reference occurrence.
func RefOccurrence(symbol string, line, startCol, endCol int) testOccurrence { ... }
```

**Note:** The exact implementation of test helpers depends on SCIP protobuf types. Construct `scip.Document` and `scip.Occurrence` structs directly. Place test helpers in a `_test.go` file or `internal/scip/testutil_test.go` so they're test-only.

- [ ] **Step 5: Run all tests**

Run: `cd $REPO_ROOT && go test ./internal/scip/ -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
cd $REPO_ROOT
git add internal/scip/convert.go internal/scip/convert_test.go
git commit -m "feat(scip): convert SCIP occurrences to TypedEdge call edges"
```

---

## Chunk 3: Language → Indexer Detection & Subprocess Runner

### Task 3: Detect language and select SCIP indexer

**Files:**
- Create: `internal/scip/detect.go`
- Create: `internal/scip/detect_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/scip/detect_test.go
package scip_test

import (
	"testing"

	gocodescip "github.com/anatolykoptev/go-code/internal/scip"
)

func TestDetectIndexer(t *testing.T) {
	tests := []struct {
		lang    string
		wantCmd string
		wantOk  bool
	}{
		{"typescript", "scip-typescript", true},
		{"javascript", "scip-typescript", true},
		{"python", "scip-python", true},
		{"java", "scip-java", true},
		{"rust", "rust-analyzer", true},
		{"ruby", "scip-ruby", true},
		{"csharp", "scip-dotnet", true},
		{"c", "scip-clang", true},
		{"cpp", "scip-clang", true},
		{"go", "scip-go", true},
		{"php", "", false},        // no SCIP indexer for PHP yet
		{"unknown", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.lang, func(t *testing.T) {
			cmd, ok := gocodescip.DetectIndexer(tc.lang)
			if ok != tc.wantOk {
				t.Errorf("DetectIndexer(%q) ok=%v, want %v", tc.lang, ok, tc.wantOk)
			}
			if ok && cmd.Name != tc.wantCmd {
				t.Errorf("DetectIndexer(%q) cmd=%q, want %q", tc.lang, cmd.Name, tc.wantCmd)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd $REPO_ROOT && go test ./internal/scip/ -v -run TestDetectIndexer`
Expected: FAIL

- [ ] **Step 3: Write implementation**

```go
// internal/scip/detect.go
package scip

// IndexerConfig describes how to invoke a SCIP indexer for a language.
type IndexerConfig struct {
	Name string   // binary name (e.g. "scip-typescript")
	Args []string // default args
}

// indexerRegistry maps go-code language names to SCIP indexer configs.
var indexerRegistry = map[string]IndexerConfig{
	"go":         {Name: "scip-go", Args: nil},
	"typescript": {Name: "scip-typescript", Args: []string{"index"}},
	"javascript": {Name: "scip-typescript", Args: []string{"index", "--infer-tsconfig"}},
	"python":     {Name: "scip-python", Args: []string{"index", "."}},
	"java":       {Name: "scip-java", Args: []string{"index"}},
	"rust":       {Name: "rust-analyzer", Args: []string{"scip", "."}},
	"ruby":       {Name: "scip-ruby", Args: nil},
	"csharp":     {Name: "scip-dotnet", Args: nil},
	"c":          {Name: "scip-clang", Args: nil},
	"cpp":        {Name: "scip-clang", Args: nil},
}

// DetectIndexer returns the SCIP indexer config for the given language.
// Returns false if no indexer is available.
func DetectIndexer(lang string) (IndexerConfig, bool) {
	cfg, ok := indexerRegistry[lang]
	return cfg, ok
}
```

- [ ] **Step 4: Run test**

Run: `cd $REPO_ROOT && go test ./internal/scip/ -v -run TestDetectIndexer`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/scip/detect.go internal/scip/detect_test.go
git commit -m "feat(scip): language to indexer detection registry"
```

### Task 4: Run SCIP indexer as subprocess

**Files:**
- Create: `internal/scip/indexer.go`
- Create: `internal/scip/indexer_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/scip/indexer_test.go
package scip_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	gocodescip "github.com/anatolykoptev/go-code/internal/scip"
)

func TestRunIndexer_MissingBinary(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := gocodescip.RunIndexer(ctx, gocodescip.IndexerConfig{
		Name: "nonexistent-scip-indexer",
	}, t.TempDir())

	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestRunIndexer_ScipGo(t *testing.T) {
	// Skip if scip-go not installed
	if _, err := exec.LookPath("scip-go"); err != nil {
		t.Skip("scip-go not installed")
	}

	dir := t.TempDir()
	// Create minimal Go module
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test\n\ngo 1.21\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg, _ := gocodescip.DetectIndexer("go")
	indexPath, err := gocodescip.RunIndexer(ctx, cfg, dir)
	if err != nil {
		t.Fatalf("RunIndexer: %v", err)
	}

	info, err := os.Stat(indexPath)
	if err != nil {
		t.Fatalf("index.scip not created: %v", err)
	}
	if info.Size() == 0 {
		t.Error("index.scip is empty")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd $REPO_ROOT && go test ./internal/scip/ -v -run TestRunIndexer`
Expected: FAIL — RunIndexer not defined

- [ ] **Step 3: Write implementation**

```go
// internal/scip/indexer.go
package scip

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const defaultIndexFile = "index.scip"

// RunIndexer executes a SCIP indexer in the given directory and returns
// the path to the generated index.scip file.
// Returns error if the indexer binary is not found or the process fails.
func RunIndexer(ctx context.Context, cfg IndexerConfig, dir string) (string, error) {
	binPath, err := exec.LookPath(cfg.Name)
	if err != nil {
		return "", fmt.Errorf("indexer %q not found in PATH: %w", cfg.Name, err)
	}

	args := make([]string, len(cfg.Args))
	copy(args, cfg.Args)

	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Dir = dir
	cmd.Stdout = nil // discard
	cmd.Stderr = nil // discard

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("indexer %q failed: %w", cfg.Name, err)
	}

	indexPath := filepath.Join(dir, defaultIndexFile)
	if _, err := os.Stat(indexPath); err != nil {
		return "", fmt.Errorf("index file not found after indexing: %w", err)
	}

	return indexPath, nil
}

// IndexerAvailable checks if the SCIP indexer binary for the given language
// exists in PATH. Use this for fast availability checks without running indexer.
func IndexerAvailable(lang string) bool {
	cfg, ok := DetectIndexer(lang)
	if !ok {
		return false
	}
	_, err := exec.LookPath(cfg.Name)
	return err == nil
}
```

- [ ] **Step 4: Run tests**

Run: `cd $REPO_ROOT && go test ./internal/scip/ -v -run TestRunIndexer`
Expected: TestRunIndexer_MissingBinary PASS, TestRunIndexer_ScipGo SKIP (unless scip-go is installed)

- [ ] **Step 5: Commit**

```bash
git add internal/scip/indexer.go internal/scip/indexer_test.go
git commit -m "feat(scip): subprocess runner for SCIP indexers"
```

---

## Chunk 4: Integration into CallGraph Pipeline

### Task 5: Wire SCIP into BuildFromRepo

**Files:**
- Modify: `internal/callgraph/repo.go` (add `trySCIPResolution`)
- Create: `internal/callgraph/scip_integration_test.go`

- [ ] **Step 1: Write failing integration test**

```go
// internal/callgraph/scip_integration_test.go
package callgraph_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/callgraph"
)

func TestBuildFromRepo_SCIPEnhanced(t *testing.T) {
	// Skip if scip-typescript not installed
	if _, err := exec.LookPath("scip-typescript"); err != nil {
		t.Skip("scip-typescript not installed")
	}

	dir := t.TempDir()

	// Create minimal TypeScript project
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test"}`), 0o644)
	os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(`{"compilerOptions":{"target":"es2020"},"include":["*.ts"]}`), 0o644)
	os.WriteFile(filepath.Join(dir, "main.ts"), []byte(`
function greet(name: string): string {
  return "Hello, " + name;
}

function main(): void {
  console.log(greet("world"));
}

main();
`), 0o644)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cg, err := callgraph.BuildFromRepo(ctx, callgraph.TraceRepoInput{
		Root: dir,
	})
	if err != nil {
		t.Fatalf("BuildFromRepo: %v", err)
	}

	if cg.Tier != "enhanced" {
		t.Errorf("expected tier 'enhanced' for TS project with SCIP, got %q", cg.Tier)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd $REPO_ROOT && go test ./internal/callgraph/ -v -run TestBuildFromRepo_SCIP -timeout 120s`
Expected: FAIL or tier="basic" (SCIP not wired yet)

- [ ] **Step 3: Modify BuildFromRepo to try SCIP resolution**

In `internal/callgraph/repo.go`, add after the go/types block (line ~68) and before hook injection:

```go
// Attempt SCIP resolution for non-Go languages (or Go without go/types).
if cg.Tier == "basic" {
    if scipCG := trySCIPResolution(ctx, input.Root, ir.Files, allSymbols); scipCG != nil {
        cg = MergeCallGraphs(cg, scipCG)
        cg.Tier = "enhanced"
    }
}
```

Add the new function:

```go
// trySCIPResolution attempts SCIP indexing for the dominant language.
// Returns nil on any failure — callers fall back to tree-sitter-only graph.
func trySCIPResolution(ctx context.Context, root string, files []*ingest.File, tsSymbols []*parser.Symbol) *CallGraph {
    // Detect dominant language from files
    lang := dominantLang(files)
    if lang == "" {
        return nil
    }

    cfg, ok := gocodescip.DetectIndexer(lang)
    if !ok {
        return nil
    }

    if !gocodescip.IndexerAvailable(lang) {
        return nil
    }

    indexPath, err := gocodescip.RunIndexer(ctx, cfg, root)
    if err != nil {
        return nil
    }

    idx, err := gocodescip.ReadIndex(indexPath)
    if err != nil {
        return nil
    }

    typedEdges := gocodescip.ConvertToEdges(idx)
    if len(typedEdges) == 0 {
        return nil
    }

    // Clean up index file
    os.Remove(indexPath)

    return ConvertToCallGraph(typedEdges, tsSymbols)
}

func dominantLang(files []*ingest.File) string {
    counts := make(map[string]int)
    for _, f := range files {
        if f.Language != "" {
            counts[f.Language]++
        }
    }
    best := ""
    bestN := 0
    for lang, n := range counts {
        if n > bestN {
            best = lang
            bestN = n
        }
    }
    return best
}
```

Add import: `gocodescip "github.com/anatolykoptev/go-code/internal/scip"`

- [ ] **Step 4: Run integration test**

Run: `cd $REPO_ROOT && go test ./internal/callgraph/ -v -run TestBuildFromRepo_SCIP -timeout 120s`
Expected: PASS (if scip-typescript installed) or SKIP

- [ ] **Step 5: Run full test suite to ensure no regressions**

Run: `cd $REPO_ROOT && go test ./internal/callgraph/ -v -timeout 120s`
Expected: All existing tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/callgraph/repo.go internal/callgraph/scip_integration_test.go
git commit -m "feat(scip): wire SCIP resolution into BuildFromRepo for all languages"
```

---

## Chunk 5: Update Tier System

### Task 6: Add SCIP backend to tier detection

**Files:**
- Modify: `internal/tier/tier.go`
- Modify: `internal/tier/tier_test.go`

- [ ] **Step 1: Write failing test**

```go
// Add to internal/tier/tier_test.go

func TestDetectSCIPEnhanced(t *testing.T) {
	d := tier.NewDetector(tier.Backends{SCIP: true})
	if got := d.Current(); got != tier.Enhanced {
		t.Fatalf("expected Enhanced tier with SCIP, got %v", got)
	}
}

func TestProvenanceIncludesSCIP(t *testing.T) {
	d := tier.NewDetector(tier.Backends{SCIP: true})
	p := d.ProvenanceFor()
	found := false
	for _, b := range p.Backends {
		if b == "scip" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'scip' in backends, got %v", p.Backends)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd $REPO_ROOT && go test ./internal/tier/ -v -run TestDetectSCIP`
Expected: FAIL — SCIP field doesn't exist

- [ ] **Step 3: Update tier.go**

Add `SCIP bool` to `Backends` struct. Update `detect()` — keep existing warning codes to avoid breaking tests:

```go
func (d *Detector) detect() {
	hasTypeAnalysis := d.backends.GoTypes || d.backends.SCIP
	switch {
	case !hasTypeAnalysis:
		d.tier = Basic
		d.warnings = []DegradationWarning{
			{
				Code: "go_types_missing", // keep existing code for backward compat
				Message: "Go type analysis unavailable — using name-based resolution " +
					"(less precise for interfaces). Ensure repo has go.mod and is buildable.",
				CapabilityPct: 40,
			},
		}
	case !d.backends.VTA:
		d.tier = Enhanced
		// ...existing warning unchanged...
	default:
		d.tier = Full
		d.warnings = nil
	}
}
```

Update `ProvenanceFor()` to include "scip":

```go
if d.backends.SCIP {
    active = append(active, "scip")
}
```

- [ ] **Step 4: Run tier tests**

Run: `cd $REPO_ROOT && go test ./internal/tier/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/tier/tier.go internal/tier/tier_test.go
git commit -m "feat(tier): add SCIP as analysis backend for enhanced tier"
```

---

## Chunk 6: Docker Image + Indexer Installation

### Task 7: Install SCIP indexers in Docker image

**Files:**
- Modify: `Dockerfile`

- [ ] **Step 1: Update Dockerfile runtime stage**

Add Node.js and SCIP indexers to the runtime image. Keep it minimal:

```dockerfile
# ── Stage 2: Runtime ──────────────────────────────────────────────────────────
FROM alpine:3.21

# Core runtime deps
RUN apk add --no-cache ca-certificates git tzdata && \
    git config --global --add safe.directory '*'

# Node.js for SCIP indexers (scip-typescript, scip-python)
RUN apk add --no-cache nodejs npm && \
    npm install -g @sourcegraph/scip-typescript @sourcegraph/scip-python && \
    npm cache clean --force

# scip-go (Go binary, no runtime deps)
COPY --from=builder /go/bin/scip-go /usr/local/bin/scip-go
```

Add scip-go installation to the builder stage:

```dockerfile
# In builder stage, after go mod download:
RUN go install github.com/sourcegraph/scip-go/cmd/scip-go@latest
```

**Note to implementer:** Verify npm package names are correct: `@sourcegraph/scip-typescript`, `@sourcegraph/scip-python`. Check if scip-python needs Python runtime (it's based on Pyright/Node, so Node should suffice for basic indexing). Test the Docker build locally before deploying.

**IMPORTANT:** Update docker-compose memory limit from 512M to 1G:
```yaml
# In compose/search.yml, go-code service:
    deploy:
      resources:
        limits:
          memory: 1G  # was 512M — SCIP indexers (TypeScript compiler) need more RAM
```
Also add `NODE_OPTIONS=--max-old-space-size=512` to environment to cap Node.js memory and skip SCIP indexing for repos with >1000 source files (guard in `trySCIPResolution`).

- [ ] **Step 2: Test Docker build**

Run: `cd $REPO_ROOT && docker build -t go-code-scip-test .`
Expected: Build succeeds. Image size increase ~100-150MB (Node.js + npm packages).

- [ ] **Step 3: Test indexer availability inside container**

Run:
```bash
docker run --rm go-code-scip-test sh -c "scip-go --version && scip-typescript --version && scip-python --version"
```
Expected: Version strings printed for all three.

- [ ] **Step 4: Commit**

```bash
git add Dockerfile
git commit -m "feat(docker): install SCIP indexers (scip-go, scip-typescript, scip-python)"
```

---

## Chunk 7: Caching + Read-Only Volume Handling

### Task 8: Handle read-only repos and cache index files

**Files:**
- Create: `internal/scip/cache.go`
- Create: `internal/scip/cache_test.go`
- Modify: `internal/scip/indexer.go`

**Context:** The `/host` volume is read-only in Docker. SCIP indexers write `index.scip` to the working directory. We need to either: (a) copy repo to writable tmpdir before indexing, or (b) use indexer flags to control output path. Most indexers support `-o` / `--output` flag.

- [ ] **Step 1: Write failing test**

```go
// internal/scip/cache_test.go
package scip_test

import (
	"os"
	"path/filepath"
	"testing"

	gocodescip "github.com/anatolykoptev/go-code/internal/scip"
)

func TestCacheKey(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.ts"), []byte("console.log()"), 0o644)

	key1 := gocodescip.CacheKey(dir)
	if key1 == "" {
		t.Fatal("expected non-empty cache key")
	}

	// Same dir = same key
	key2 := gocodescip.CacheKey(dir)
	if key1 != key2 {
		t.Errorf("cache keys should be stable: %q != %q", key1, key2)
	}
}

func TestCacheLookup_Miss(t *testing.T) {
	c := gocodescip.NewCache(t.TempDir())
	_, ok := c.Get("nonexistent")
	if ok {
		t.Error("expected cache miss")
	}
}
```

- [ ] **Step 2: Implement cache**

```go
// internal/scip/cache.go
package scip

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Cache stores SCIP index files keyed by repo content hash.
type Cache struct {
	dir string
}

// NewCache creates a cache backed by the given directory.
func NewCache(dir string) *Cache {
	os.MkdirAll(dir, 0o755)
	return &Cache{dir: dir}
}

// Get returns the cached index path if it exists.
func (c *Cache) Get(key string) (string, bool) {
	path := filepath.Join(c.dir, key+".scip")
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	return path, true
}

// Put stores an index file in the cache.
func (c *Cache) Put(key string, indexPath string) error {
	dst := filepath.Join(c.dir, key+".scip")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// CacheKey computes a hash from file mtimes in a directory.
// Fast but sufficient — detects any file changes.
func CacheKey(dir string) string {
	h := sha256.New()
	var entries []string

	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		entries = append(entries, fmt.Sprintf("%s:%d", rel, info.ModTime().UnixNano()))
		return nil
	})

	sort.Strings(entries)
	h.Write([]byte(strings.Join(entries, "\n")))
	return fmt.Sprintf("%x", h.Sum(nil))[:16]
}
```

- [ ] **Step 3: Update RunIndexer for read-only repos**

Modify `internal/scip/indexer.go` to copy repo to tmpdir when source is read-only:

```go
// RunIndexer executes a SCIP indexer. If dir is read-only, copies to a
// temporary directory first. Returns path to the generated index.scip.
func RunIndexer(ctx context.Context, cfg IndexerConfig, dir string) (string, error) {
	workDir := dir

	// Check if directory is writable
	testFile := filepath.Join(dir, ".scip-write-test")
	if err := os.WriteFile(testFile, []byte{}, 0o644); err != nil {
		// Read-only — use output flag or copy
		workDir, err = copyToTmp(dir)
		if err != nil {
			return "", fmt.Errorf("copy to tmpdir: %w", err)
		}
		defer os.RemoveAll(workDir)
	} else {
		os.Remove(testFile)
	}

	// ... rest of execution in workDir ...
}
```

**Note:** The actual copy function should be shallow (symlinks or hardlinks where possible) to avoid copying large repos. For Docker `/host:ro` volumes, a simple file copy of manifest + source files may be needed.

- [ ] **Step 4: Run tests**

Run: `cd $REPO_ROOT && go test ./internal/scip/ -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/scip/cache.go internal/scip/cache_test.go internal/scip/indexer.go
git commit -m "feat(scip): add index caching and read-only volume support"
```

---

## Chunk 8: End-to-End Validation

### Task 9: Integration tests with real SCIP indexers

**Files:**
- Create: `internal/scip/integration_test.go`

- [ ] **Step 1: Write e2e tests for each language**

```go
// internal/scip/integration_test.go
package scip_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	gocodescip "github.com/anatolykoptev/go-code/internal/scip"
)

func TestE2E_TypeScript(t *testing.T) {
	if _, err := exec.LookPath("scip-typescript"); err != nil {
		t.Skip("scip-typescript not installed")
	}

	dir := t.TempDir()
	writeFile(t, dir, "package.json", `{"name":"test","version":"1.0.0"}`)
	writeFile(t, dir, "tsconfig.json", `{"compilerOptions":{"target":"es2020","strict":true},"include":["*.ts"]}`)
	writeFile(t, dir, "main.ts", `
interface Greeter {
  greet(name: string): string;
}

class EnglishGreeter implements Greeter {
  greet(name: string): string {
    return "Hello, " + name;
  }
}

function run(g: Greeter): string {
  return g.greet("world");
}

const g = new EnglishGreeter();
console.log(run(g));
`)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cfg, _ := gocodescip.DetectIndexer("typescript")
	indexPath, err := gocodescip.RunIndexer(ctx, cfg, dir)
	if err != nil {
		t.Fatalf("RunIndexer: %v", err)
	}

	idx, err := gocodescip.ReadIndex(indexPath)
	if err != nil {
		t.Fatalf("ReadIndex: %v", err)
	}

	if idx.DocumentCount() == 0 {
		t.Fatal("expected at least one document")
	}

	edges := gocodescip.ConvertToEdges(idx)
	t.Logf("Got %d edges from TypeScript SCIP index", len(edges))

	// Verify we got the run -> greet edge
	found := false
	for _, e := range edges {
		if e.CallerName == "run" && e.CalleeName == "greet" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected run -> greet edge from SCIP TypeScript index")
		for _, e := range edges {
			t.Logf("  %s -> %s", e.CallerName, e.CalleeName)
		}
	}
}

func TestE2E_Python(t *testing.T) {
	if _, err := exec.LookPath("scip-python"); err != nil {
		t.Skip("scip-python not installed")
	}

	dir := t.TempDir()
	writeFile(t, dir, "main.py", `
def greet(name: str) -> str:
    return f"Hello, {name}"

def main() -> None:
    print(greet("world"))

if __name__ == "__main__":
    main()
`)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cfg, _ := gocodescip.DetectIndexer("python")
	indexPath, err := gocodescip.RunIndexer(ctx, cfg, dir)
	if err != nil {
		t.Fatalf("RunIndexer: %v", err)
	}

	idx, err := gocodescip.ReadIndex(indexPath)
	if err != nil {
		t.Fatalf("ReadIndex: %v", err)
	}

	edges := gocodescip.ConvertToEdges(idx)
	t.Logf("Got %d edges from Python SCIP index", len(edges))

	found := false
	for _, e := range edges {
		if e.CallerName == "main" && e.CalleeName == "greet" {
			found = true
		}
	}
	if !found {
		t.Error("expected main -> greet edge from SCIP Python index")
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Run e2e tests**

Run: `cd $REPO_ROOT && go test ./internal/scip/ -v -run TestE2E -timeout 120s`
Expected: Tests PASS for installed indexers, SKIP for missing ones.

- [ ] **Step 3: Run full repo test suite**

Run: `cd $REPO_ROOT && go test ./... -timeout 300s`
Expected: All PASS, no regressions.

- [ ] **Step 4: Commit**

```bash
git add internal/scip/integration_test.go
git commit -m "test(scip): end-to-end integration tests for TypeScript and Python"
```

### Task 10: Deploy and verify

- [ ] **Step 1: Build Docker image**

Run: `cd ~/deploy/my-server && docker compose build --no-cache go-code`
Expected: Build succeeds.

- [ ] **Step 2: Deploy**

Run: `cd ~/deploy/my-server && docker compose up -d --no-deps --force-recreate go-code`
Expected: Container healthy.

- [ ] **Step 3: Verify with real repo**

Use `call_trace` on a TypeScript repo to confirm enhanced tier:
```
call_trace repo=<some-ts-repo> symbol=main
```
Expected: tier="enhanced" in output.

- [ ] **Step 4: Tag release**

```bash
cd $REPO_ROOT
git tag v1.19.0
git push origin v1.19.0
```

---

## Summary

| Chunk | Tasks | What it delivers |
|-------|-------|-----------------|
| 1 | Task 1 | SCIP protobuf reader with streaming parser |
| 2 | Task 2 | SCIP → TypedEdge converter (call edge extraction) |
| 3 | Tasks 3-4 | Language detection + indexer subprocess runner |
| 4 | Task 5 | Wire SCIP into BuildFromRepo pipeline |
| 5 | Task 6 | Tier system recognizes SCIP backend |
| 6 | Task 7 | Docker image with bundled indexers |
| 7 | Task 8 | Cache + read-only volume handling |
| 8 | Tasks 9-10 | E2E tests + deploy |

**Total: 10 tasks, 8 chunks.** Chunk 1 and Chunk 3 can run in parallel. Chunk 2 depends on Chunk 1. Chunk 7 (cache/read-only) should be implemented BEFORE Chunk 4 (integration), because BuildFromRepo may receive read-only `/host` paths. Execution order: [1,3 parallel] → 2 → 7 → 4 → 5 → 6 → 8.

**Risk:** SCIP protobuf types may differ from assumed API. Task 1 step 4 has a note about verifying exact import paths. Budget extra time for API discovery.

**Not in scope (future):** VTA for Go (tier Full), scip-java/scip-clang Docker installation (heavier deps), incremental SCIP indexing, SCIP-based dead code detection.
