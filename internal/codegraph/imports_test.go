package codegraph

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// TestBuildGraphCreatesImportsEdges verifies that buildGraph generates IMPORTS
// edges from fileImports and creates external Package vertices for non-local
// import paths.
func TestBuildGraphCreatesImportsEdges(t *testing.T) {
	t.Parallel()

	root := "/repo"
	files := []*ingest.File{
		{Path: "/repo/cmd/main.go", RelPath: "cmd/main.go", Language: "go", Size: 100},
		{Path: "/repo/internal/util.go", RelPath: "internal/util.go", Language: "go", Size: 200},
	}

	// No symbols or calls — focus on imports.
	var symbols []*parser.Symbol
	cg := &callgraph.CallGraph{}

	fileImports := map[string][]string{
		"cmd/main.go":      {"fmt", "github.com/pkg/errors", "internal"},
		"internal/util.go": {"fmt", "strings"},
	}

	_, edges := buildGraph(root, files, symbols, cg, fileImports)

	// Count IMPORTS edges.
	importsEdgeCount := 0
	for _, e := range edges {
		if e.EdgeLabel == "IMPORTS" {
			importsEdgeCount++
		}
	}

	// cmd/main.go imports 3 packages, internal/util.go imports 2 → 5 total.
	wantImportsEdges := 5
	if importsEdgeCount != wantImportsEdges {
		t.Errorf("IMPORTS edge count = %d, want %d", importsEdgeCount, wantImportsEdges)
	}

	// Verify a specific IMPORTS edge exists.
	found := false
	for _, e := range edges {
		if e.EdgeLabel == "IMPORTS" && e.FromKey == "cmd/main.go" && e.ToKey == "github.com/pkg/errors" {
			found = true
			if e.FromLabel != "File" {
				t.Errorf("IMPORTS FromLabel = %q, want File", e.FromLabel)
			}
			if e.ToLabel != "Package" {
				t.Errorf("IMPORTS ToLabel = %q, want Package", e.ToLabel)
			}
		}
	}
	if !found {
		t.Error("missing IMPORTS edge: cmd/main.go -> github.com/pkg/errors")
	}
}

// TestBuildGraphCreatesExternalPackageVertices verifies that buildGraph creates
// Package vertices with repo="external" for import paths not matching local
// package directories.
func TestBuildGraphCreatesExternalPackageVertices(t *testing.T) {
	t.Parallel()

	root := "/repo"
	files := []*ingest.File{
		{Path: "/repo/main.go", RelPath: "main.go", Language: "go", Size: 100},
	}

	var symbols []*parser.Symbol
	cg := &callgraph.CallGraph{}

	fileImports := map[string][]string{
		"main.go": {"fmt", "github.com/pkg/errors"},
	}

	vertices, _ := buildGraph(root, files, symbols, cg, fileImports)

	// Collect external Package vertices.
	externalPkgs := make(map[string]string) // path -> repo
	for _, v := range vertices {
		if v.Label == "Package" && v.Props["repo"] == "external" {
			externalPkgs[v.Props["path"]] = v.Props["name"]
		}
	}

	// "fmt" and "github.com/pkg/errors" are not local dirs, so both should be external.
	if _, ok := externalPkgs["fmt"]; !ok {
		t.Error("expected external Package vertex for 'fmt'")
	}
	if name := externalPkgs["fmt"]; name != "fmt" {
		t.Errorf("external Package 'fmt' name = %q, want 'fmt'", name)
	}

	if _, ok := externalPkgs["github.com/pkg/errors"]; !ok {
		t.Error("expected external Package vertex for 'github.com/pkg/errors'")
	}
	if name := externalPkgs["github.com/pkg/errors"]; name != "errors" {
		t.Errorf("external Package 'github.com/pkg/errors' name = %q, want 'errors'", name)
	}
}

// TestBuildGraphLocalImportNoExternalVertex verifies that when an import path
// matches a local package directory, no duplicate external Package vertex is
// created.
func TestBuildGraphLocalImportNoExternalVertex(t *testing.T) {
	t.Parallel()

	root := "/repo"
	// The local package directory "internal" matches the import path "internal".
	files := []*ingest.File{
		{Path: "/repo/main.go", RelPath: "main.go", Language: "go", Size: 100},
		{Path: "/repo/internal/util.go", RelPath: "internal/util.go", Language: "go", Size: 200},
	}

	var symbols []*parser.Symbol
	cg := &callgraph.CallGraph{}

	fileImports := map[string][]string{
		"main.go": {"internal"},
	}

	vertices, edges := buildGraph(root, files, symbols, cg, fileImports)

	// Count Package vertices with path="internal".
	internalPkgCount := 0
	for _, v := range vertices {
		if v.Label == "Package" && v.Props["path"] == "internal" {
			internalPkgCount++
		}
	}

	// Should only have 1 (the local one), not 2 (no external duplicate).
	if internalPkgCount != 1 {
		t.Errorf("Package vertices with path='internal' = %d, want 1 (no external duplicate)", internalPkgCount)
	}

	// The IMPORTS edge should still exist.
	importsEdge := false
	for _, e := range edges {
		if e.EdgeLabel == "IMPORTS" && e.FromKey == "main.go" && e.ToKey == "internal" {
			importsEdge = true
		}
	}
	if !importsEdge {
		t.Error("missing IMPORTS edge: main.go -> internal")
	}
}

// TestBuildGraphDeduplicatesExternalPackages verifies that the same external
// import path imported by multiple files creates only one Package vertex.
func TestBuildGraphDeduplicatesExternalPackages(t *testing.T) {
	t.Parallel()

	root := "/repo"
	files := []*ingest.File{
		{Path: "/repo/a.go", RelPath: "a.go", Language: "go", Size: 100},
		{Path: "/repo/b.go", RelPath: "b.go", Language: "go", Size: 100},
	}

	var symbols []*parser.Symbol
	cg := &callgraph.CallGraph{}

	fileImports := map[string][]string{
		"a.go": {"fmt"},
		"b.go": {"fmt"},
	}

	vertices, edges := buildGraph(root, files, symbols, cg, fileImports)

	// Count external Package vertices for "fmt".
	fmtPkgCount := 0
	for _, v := range vertices {
		if v.Label == "Package" && v.Props["path"] == "fmt" && v.Props["repo"] == "external" {
			fmtPkgCount++
		}
	}
	if fmtPkgCount != 1 {
		t.Errorf("external Package vertices for 'fmt' = %d, want 1", fmtPkgCount)
	}

	// Both files should have an IMPORTS edge to "fmt".
	importsCount := 0
	for _, e := range edges {
		if e.EdgeLabel == "IMPORTS" && e.ToKey == "fmt" {
			importsCount++
		}
	}
	if importsCount != 2 {
		t.Errorf("IMPORTS edges to 'fmt' = %d, want 2", importsCount)
	}

	_ = edges
}

// TestBuildGraphEmptyImports verifies that buildGraph handles nil/empty
// fileImports without panicking and produces no IMPORTS edges.
func TestBuildGraphEmptyImports(t *testing.T) {
	t.Parallel()

	root := "/repo"
	files := []*ingest.File{
		{Path: "/repo/main.go", RelPath: "main.go", Language: "go", Size: 100},
	}

	var symbols []*parser.Symbol
	cg := &callgraph.CallGraph{}

	// Test with nil map.
	vertices, edges := buildGraph(root, files, symbols, cg, nil)
	for _, e := range edges {
		if e.EdgeLabel == "IMPORTS" {
			t.Error("unexpected IMPORTS edge with nil fileImports")
		}
	}

	// Test with empty map.
	vertices, edges = buildGraph(root, files, symbols, cg, map[string][]string{})
	for _, e := range edges {
		if e.EdgeLabel == "IMPORTS" {
			t.Error("unexpected IMPORTS edge with empty fileImports")
		}
	}

	_ = vertices
}
