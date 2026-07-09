package explore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExplore_BasicRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	writeFile(t, dir, "main.go", `package main

func main() {
	hello()
}

func hello() {
	println("hello")
}
`)

	writeFile(t, dir, "util.go", `package main

func add(a, b int) int {
	return a + b
}

func sub(a, b int) int {
	return a - b
}
`)

	result, err := Run(context.Background(), Input{Root: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.FileCount != 2 {
		t.Errorf("FileCount = %d, want 2", result.FileCount)
	}

	if result.SymbolCount < 4 {
		t.Errorf("SymbolCount = %d, want >= 4 (main, hello, add, sub)", result.SymbolCount)
	}

	if len(result.Languages) == 0 {
		t.Fatal("Languages is empty")
	}

	foundGo := false
	for _, lang := range result.Languages {
		if lang.Name == "go" {
			foundGo = true
			if lang.Files != 2 {
				t.Errorf("go files = %d, want 2", lang.Files)
			}
		}
	}
	if !foundGo {
		t.Error("go language not found in Languages")
	}

	if result.TotalLines == 0 {
		t.Error("TotalLines = 0, want > 0")
	}
}

func TestExplore_DeadCodeSection(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	writeFile(t, dir, "main.go", `package main

func main() {
	used()
}

func used() {
	println("used")
}

func deadOne() {
	println("dead")
}

func deadTwo() {
	println("also dead")
}
`)

	result, err := Run(context.Background(), Input{Root: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.DeadCode == nil {
		t.Fatal("DeadCode is nil, expected dead functions")
	}

	if result.DeadCode.Count < 2 {
		t.Errorf("DeadCode.Count = %d, want >= 2", result.DeadCode.Count)
	}

	foundDeadOne := false
	foundDeadTwo := false
	for _, s := range result.DeadCode.Samples {
		if s == "deadOne" {
			foundDeadOne = true
		}
		if s == "deadTwo" {
			foundDeadTwo = true
		}
	}
	if !foundDeadOne {
		t.Error("deadOne not in DeadCode.Samples")
	}
	if !foundDeadTwo {
		t.Error("deadTwo not in DeadCode.Samples")
	}
}

func TestExplore_TopSymbols(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	writeFile(t, dir, "main.go", `package main

func main() {
	c()
}

func a() {
	c()
}

func b() {
	c()
}

func c() {
	println("c")
}
`)

	result, err := Run(context.Background(), Input{Root: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.TopSymbols) == 0 {
		t.Fatal("TopSymbols is empty")
	}

	// c should be the most-called symbol (3 calls from main, a, b).
	top := result.TopSymbols[0]
	if top.Name != "c" {
		t.Errorf("top symbol = %q, want %q", top.Name, "c")
	}
	if top.CallCount < 3 {
		t.Errorf("top symbol call count = %d, want >= 3", top.CallCount)
	}
}

func TestExplore_LanguageFilter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	writeFile(t, dir, "main.go", `package main

func main() {}
`)

	writeFile(t, dir, "script.py", `def hello():
    print("hello")
`)

	// Without filter: both languages.
	result, err := Run(context.Background(), Input{Root: dir})
	if err != nil {
		t.Fatalf("Run (no filter): %v", err)
	}
	if result.FileCount != 2 {
		t.Errorf("unfiltered FileCount = %d, want 2", result.FileCount)
	}

	// With go filter: only go.
	result, err = Run(context.Background(), Input{Root: dir, Language: "go"})
	if err != nil {
		t.Fatalf("Run (go filter): %v", err)
	}
	if result.FileCount != 1 {
		t.Errorf("go-filtered FileCount = %d, want 1", result.FileCount)
	}
	if len(result.Languages) != 1 || result.Languages[0].Name != "go" {
		t.Errorf("Languages = %v, want [go]", result.Languages)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
