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

	_, edges := buildGraph(buildGraphInput{Root: root, Files: files, Symbols: symbols, CallGraph: cg, FileImports: fileImports})

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

	vertices, _ := buildGraph(buildGraphInput{Root: root, Files: files, Symbols: symbols, CallGraph: cg, FileImports: fileImports})

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

	vertices, edges := buildGraph(buildGraphInput{Root: root, Files: files, Symbols: symbols, CallGraph: cg, FileImports: fileImports})

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

	vertices, edges := buildGraph(buildGraphInput{Root: root, Files: files, Symbols: symbols, CallGraph: cg, FileImports: fileImports})

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
}

// TestBuildGraphFullImportPathMapsToLocalContainer is the regression guard for
// the duplicate-package-node bug. Real Go imports are full module paths
// (github.com/x/y/internal/fleet/docker), but pkgDirs is keyed by repo-relative
// dir (internal/fleet/docker). The IMPORTS edge for a local import must point at
// the CONTAINER vertex (path = relative dir), NOT create a second "external" node
// keyed by the full import path — otherwise the package graph splits into two
// disconnected halves (CONTAINS on one node, IMPORTS on the other).
//
// Falsification (red-on-revert): restore the bare `pkgDirs[imp]` lookup and this
// test fails — the edge ToKey becomes the full import path and a duplicate
// external vertex appears.
func TestBuildGraphFullImportPathMapsToLocalContainer(t *testing.T) {
	t.Parallel()

	const localImport = "github.com/anatolykoptev/go-code/internal/fleet/docker"
	root := "/repo"
	files := []*ingest.File{
		{Path: "/repo/internal/fleet/fleet.go", RelPath: "internal/fleet/fleet.go", Language: "go", Size: 100},
		{Path: "/repo/internal/fleet/docker/driver.go", RelPath: "internal/fleet/docker/driver.go", Language: "go", Size: 200},
	}
	cg := &callgraph.CallGraph{}
	fileImports := map[string][]string{
		// fleet.go imports the docker subpackage by its FULL module path.
		"internal/fleet/fleet.go": {localImport},
		// docker/driver.go imports an unrelated EXTERNAL package that shares the
		// base name "docker" — must NOT be conflated with the local docker dir.
		"internal/fleet/docker/driver.go": {"github.com/docker/docker/client"},
	}

	vertices, edges := buildGraph(buildGraphInput{Root: root, Files: files, CallGraph: cg, FileImports: fileImports})

	// The local IMPORTS edge must target the container dir, not the full path.
	var localEdgeToKey string
	for _, e := range edges {
		if e.EdgeLabel == "IMPORTS" && e.FromKey == "internal/fleet/fleet.go" {
			localEdgeToKey = e.ToKey
		}
	}
	if localEdgeToKey != "internal/fleet/docker" {
		t.Errorf("local IMPORTS ToKey = %q, want %q (container dir, not full import path)",
			localEdgeToKey, "internal/fleet/docker")
	}

	// No Package vertex should exist for the full local import path (no duplicate).
	for _, v := range vertices {
		if v.Label == "Package" && v.Props["path"] == localImport {
			t.Errorf("duplicate Package vertex created for local import %q (should reuse container)", localImport)
		}
	}

	// The same-base EXTERNAL import must still get its own external vertex and a
	// full-path edge (not collapsed into the local docker dir).
	extVertex := false
	for _, v := range vertices {
		if v.Label == "Package" && v.Props["path"] == "github.com/docker/docker/client" && v.Props["repo"] == "external" {
			extVertex = true
		}
	}
	if !extVertex {
		t.Error("external same-base import github.com/docker/docker/client should be its own external vertex")
	}
	extEdge := false
	for _, e := range edges {
		if e.EdgeLabel == "IMPORTS" && e.ToKey == "github.com/docker/docker/client" {
			extEdge = true
		}
	}
	if !extEdge {
		t.Error("external import edge ToKey should remain the full path, not the local docker dir")
	}
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
	_, edges := buildGraph(buildGraphInput{Root: root, Files: files, Symbols: symbols, CallGraph: cg})
	for _, e := range edges {
		if e.EdgeLabel == "IMPORTS" {
			t.Error("unexpected IMPORTS edge with nil fileImports")
		}
	}

	// Test with empty map.
	_, edges = buildGraph(buildGraphInput{Root: root, Files: files, Symbols: symbols, CallGraph: cg, FileImports: map[string][]string{}})
	for _, e := range edges {
		if e.EdgeLabel == "IMPORTS" {
			t.Error("unexpected IMPORTS edge with empty fileImports")
		}
	}
}

// TestBuildGraphRelativeTSImportResolvesToContainer verifies that TS/JS-style
// relative imports ("./x", "../x", index dirs) resolve to the target file's
// package (container) dir instead of becoming orphan external nodes.
//
// Falsification (red-on-revert): drop the resolveRelativeImport dispatch and the
// edges target the raw "./chat" string + duplicate external vertices appear.
func TestBuildGraphRelativeTSImportResolvesToContainer(t *testing.T) {
	t.Parallel()

	root := "/repo"
	files := []*ingest.File{
		{Path: "/repo/web/src/lib/app.ts", RelPath: "web/src/lib/app.ts", Language: "typescript", Size: 100},
		{Path: "/repo/web/src/lib/chat.ts", RelPath: "web/src/lib/chat.ts", Language: "typescript", Size: 100},
		{Path: "/repo/web/src/lib/video/index.ts", RelPath: "web/src/lib/video/index.ts", Language: "typescript", Size: 100},
		{Path: "/repo/web/src/util/fmt.ts", RelPath: "web/src/util/fmt.ts", Language: "typescript", Size: 100},
	}
	cg := &callgraph.CallGraph{}
	fileImports := map[string][]string{
		"web/src/lib/app.ts": {
			"./chat",         // extensionless → chat.ts, container web/src/lib
			"./video",        // dir index → video/index.ts, container web/src/lib/video
			"../util/fmt.ts", // explicit ext, parent dir → container web/src/util
			"react",          // external — stays external
		},
	}

	vertices, edges := buildGraph(buildGraphInput{Root: root, Files: files, CallGraph: cg, FileImports: fileImports})

	wantEdge := map[string]string{
		"./chat":         "web/src/lib",
		"./video":        "web/src/lib/video",
		"../util/fmt.ts": "web/src/util",
	}
	got := make(map[string]string)
	for _, e := range edges {
		if e.EdgeLabel == "IMPORTS" && e.FromKey == "web/src/lib/app.ts" {
			got[e.ToKey] = e.ToKey
		}
	}
	for imp, container := range wantEdge {
		if _, hasRaw := got[imp]; hasRaw {
			t.Errorf("relative import %q left unresolved as raw ToKey (want container %q)", imp, container)
		}
		if _, hasContainer := got[container]; !hasContainer {
			t.Errorf("relative import %q did not resolve to container %q; edges=%v", imp, container, got)
		}
	}

	// No external vertex for a resolved relative import.
	for _, v := range vertices {
		if v.Label == "Package" && (v.Props["path"] == "./chat" || v.Props["path"] == "./video" || v.Props["path"] == "../util/fmt.ts") {
			t.Errorf("duplicate external vertex created for resolved relative import %q", v.Props["path"])
		}
	}
	// External import still gets its own vertex.
	ext := false
	for _, v := range vertices {
		if v.Label == "Package" && v.Props["path"] == "react" && v.Props["repo"] == "external" {
			ext = true
		}
	}
	if !ext {
		t.Error("external import 'react' should remain its own external vertex")
	}
}
