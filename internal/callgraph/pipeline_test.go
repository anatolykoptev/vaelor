package callgraph

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestBuildAndEnrich_BasicGoModule verifies that the unified pipeline
// produces a non-empty call graph with symbols, files, and type rels
// for a minimal Go module.
func TestBuildAndEnrich_BasicGoModule(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/test\n\ngo 1.21\n",
		"main.go": `package main

func main() {
	greet("world")
}

func greet(name string) {
	println("hello", name)
}
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	result, err := BuildAndEnrich(context.Background(), PipelineOpts{
		Root:         root,
		TypedEnrich:  false, // avoid go/types load in test
		MaxFileBytes: 512 * 1024,
	})
	if err != nil {
		t.Fatalf("BuildAndEnrich: %v", err)
	}

	if result.CG == nil {
		t.Fatal("expected non-nil CG")
	}
	if len(result.Symbols) == 0 {
		t.Error("expected non-empty Symbols")
	}
	if len(result.Files) == 0 {
		t.Error("expected non-empty Files")
	}
	if result.FileImports == nil {
		t.Error("expected non-nil FileImports")
	}
	// main.go should have no imports, but the map should be initialized.
	if len(result.CG.Edges) == 0 {
		t.Error("expected non-empty CG.Edges (main calls greet)")
	}
}

// TestBuildAndEnrich_FileImportsPopulated verifies that the pipeline
// populates FileImports from parsed files.
func TestBuildAndEnrich_FileImportsPopulated(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/test\n\ngo 1.21\n",
		"main.go": `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	result, err := BuildAndEnrich(context.Background(), PipelineOpts{
		Root:         root,
		TypedEnrich:  false,
		MaxFileBytes: 512 * 1024,
	})
	if err != nil {
		t.Fatalf("BuildAndEnrich: %v", err)
	}

	imports, ok := result.FileImports["main.go"]
	if !ok {
		t.Fatal("expected main.go in FileImports")
	}
	found := false
	for _, imp := range imports {
		if imp == "fmt" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'fmt' in main.go imports, got %v", imports)
	}
}

// TestBuildAndEnrich_TypedEnrichDisabled verifies that when TypedEnrich
// is false, the pipeline returns a basic tree-sitter-only graph.
func TestBuildAndEnrich_TypedEnrichDisabled(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/test\n\ngo 1.21\n",
		"main.go": `package main

func main() {
	helper()
}

func helper() {}
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	result, err := BuildAndEnrich(context.Background(), PipelineOpts{
		Root:         root,
		TypedEnrich:  false,
		MaxFileBytes: 512 * 1024,
	})
	if err != nil {
		t.Fatalf("BuildAndEnrich: %v", err)
	}

	if result.CG.Tier != "basic" {
		t.Errorf("expected Tier=basic with TypedEnrich=false, got %q", result.CG.Tier)
	}
	if result.CG.Backend != BackendTreeSitter {
		t.Errorf("expected Backend=tree-sitter with TypedEnrich=false, got %q", result.CG.Backend)
	}
}
