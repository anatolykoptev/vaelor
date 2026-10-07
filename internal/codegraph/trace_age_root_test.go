package codegraph

import (
	"testing"
)

func cypherRow(name, file string) []string {
	return []string{`"` + name + `"`, `"function"`, `"` + file + `"`, "10", "30", `""`}
}

// Issue #867: >1 AGE vertex for a bare name must surface candidates, not
// silently collapse to the pagerank-first row (the old LIMIT 1 behavior
// merged every `main`/`Close` call tree into one wrong answer).
func TestResolveTraceRoot_AmbiguousReturnsCandidates(t *testing.T) {
	rows := [][]string{
		cypherRow("main", "cmd/api/main.go"),
		cypherRow("main", "cmd/worker/main.go"),
	}
	root, ambiguous, err := resolveTraceRoot(rows, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if root != nil {
		t.Fatalf("ambiguous input must not pick a root, got %v", root)
	}
	if len(ambiguous) != 2 || ambiguous[0].Name != "main" {
		t.Fatalf("expected 2 candidates, got %v", ambiguous)
	}
}

func TestResolveTraceRoot_UniqueReturnsRoot(t *testing.T) {
	root, ambiguous, err := resolveTraceRoot([][]string{cypherRow("Serve", "api/server.go")}, "Serve")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ambiguous) != 0 {
		t.Fatalf("unique match must not be ambiguous, got %v", ambiguous)
	}
	if root == nil || root.File != "api/server.go" {
		t.Fatalf("expected root at api/server.go, got %v", root)
	}
}

func TestResolveTraceRoot_NotFound(t *testing.T) {
	if _, _, err := resolveTraceRoot(nil, "nope"); err == nil {
		t.Fatal("empty rows must error")
	}
}
