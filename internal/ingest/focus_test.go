package ingest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestContentFilter_MatchesBySymbolName(t *testing.T) {
	t.Parallel()
	symbols := []*parser.Symbol{
		{Name: "NewLLMClient", File: "/repo/llm/client.go"},
		{Name: "HandleHTTP", File: "/repo/http/handler.go"},
	}

	matched := ContentFilter("llm", symbols, nil, nil)

	if !matched["/repo/llm/client.go"] {
		t.Error("expected llm/client.go to match keyword 'llm'")
	}
	if matched["/repo/http/handler.go"] {
		t.Error("http/handler.go should not match keyword 'llm'")
	}
}

func TestContentFilter_MatchesByImport(t *testing.T) {
	t.Parallel()
	imports := map[string][]string{
		"/repo/main.go": {"github.com/prometheus/client_golang/prometheus"},
		"/repo/util.go": {"fmt", "strings"},
	}

	matched := ContentFilter("prometheus", nil, imports, nil)

	if !matched["/repo/main.go"] {
		t.Error("expected main.go to match via prometheus import")
	}
	if matched["/repo/util.go"] {
		t.Error("util.go should not match keyword 'prometheus'")
	}
}

func TestContentFilter_MatchesByCallSite(t *testing.T) {
	t.Parallel()
	calls := []parser.CallSite{
		{Name: "Retry", Receiver: "backoff", File: "/repo/retry.go"},
		{Name: "Println", Receiver: "fmt", File: "/repo/main.go"},
	}

	matched := ContentFilter("retry", nil, nil, calls)

	if !matched["/repo/retry.go"] {
		t.Error("expected retry.go to match via call site name 'Retry'")
	}
	if matched["/repo/main.go"] {
		t.Error("main.go should not match keyword 'retry'")
	}
}

func TestContentFilter_ORLogic(t *testing.T) {
	t.Parallel()
	symbols := []*parser.Symbol{
		{Name: "NewLLMClient", File: "/repo/llm.go"},
		{Name: "RetryWithBackoff", File: "/repo/retry.go"},
		{Name: "HandleHTTP", File: "/repo/http.go"},
	}

	matched := ContentFilter("llm retry", symbols, nil, nil)

	if !matched["/repo/llm.go"] {
		t.Error("llm.go should match keyword 'llm'")
	}
	if !matched["/repo/retry.go"] {
		t.Error("retry.go should match keyword 'retry'")
	}
	if matched["/repo/http.go"] {
		t.Error("http.go should not match either keyword")
	}
}

func TestContentFilter_EmptyFocus(t *testing.T) {
	t.Parallel()
	symbols := []*parser.Symbol{
		{Name: "Foo", File: "/repo/foo.go"},
	}
	matched := ContentFilter("", symbols, nil, nil)
	if matched != nil {
		t.Error("empty focus should return nil")
	}
}

func TestContentFilter_CaseInsensitive(t *testing.T) {
	t.Parallel()
	symbols := []*parser.Symbol{
		{Name: "NewMetricsCollector", File: "/repo/metrics.go"},
	}

	matched := ContentFilter("METRICS", symbols, nil, nil)
	if !matched["/repo/metrics.go"] {
		t.Error("should match case-insensitively")
	}
}

func TestContentFilter_CommaSeparated(t *testing.T) {
	t.Parallel()
	symbols := []*parser.Symbol{
		{Name: "NewLLMClient", File: "/repo/llm/client.go"},
		{Name: "RetryWithBackoff", File: "/repo/retry.go"},
		{Name: "HandleHTTP", File: "/repo/http.go"},
	}

	matched := ContentFilter("llm,retry", symbols, nil, nil)

	if !matched["/repo/llm/client.go"] {
		t.Error("expected llm/client.go to match keyword 'llm'")
	}
	if !matched["/repo/retry.go"] {
		t.Error("expected retry.go to match keyword 'retry'")
	}
	if matched["/repo/http.go"] {
		t.Error("http.go should not match either keyword")
	}
}

func TestFilterFiles_ByMatchSet(t *testing.T) {
	t.Parallel()
	files := []*File{
		{Path: "/repo/a.go", RelPath: "a.go"},
		{Path: "/repo/b.go", RelPath: "b.go"},
		{Path: "/repo/c.go", RelPath: "c.go"},
	}
	matched := map[string]bool{"/repo/a.go": true, "/repo/c.go": true}

	result := FilterFiles(files, matched)
	if len(result) != 2 {
		t.Fatalf("expected 2 files, got %d", len(result))
	}
	paths := map[string]bool{}
	for _, f := range result {
		paths[f.Path] = true
	}
	if !paths["/repo/a.go"] || !paths["/repo/c.go"] {
		t.Errorf("unexpected files: %v", result)
	}
}

func TestParseLightweight_ExtractsSymbolsAndCalls(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "main.go"), `package main

import "fmt"

func hello() {
	fmt.Println("hi")
}

func add(a, b int) int {
	return a + b
}
`)

	files := []*File{
		{Path: filepath.Join(dir, "main.go"), RelPath: "main.go", Language: "go"},
	}

	symbols, imports, calls := ParseLightweight(context.Background(), files)

	if len(symbols) < 2 {
		t.Errorf("expected >= 2 symbols (hello, add), got %d", len(symbols))
	}
	if len(imports) == 0 {
		t.Error("expected imports map to contain main.go")
	}
	if len(calls) == 0 {
		t.Error("expected at least one call site (fmt.Println)")
	}
}

func TestContentFallback_FiltersCorrectly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "llm.go"), `package main

func NewLLMClient() {}
`)
	writeFile(t, filepath.Join(dir, "http.go"), `package main

func HandleHTTP() {}
`)

	ir, err := ContentFallback(context.Background(), IngestOpts{Root: dir, MaxFileBytes: 512 * 1024}, "llm")
	if err != nil {
		t.Fatalf("ContentFallback: %v", err)
	}

	if len(ir.Files) != 1 {
		t.Fatalf("expected 1 file matching 'llm', got %d", len(ir.Files))
	}
	if ir.Files[0].RelPath != "llm.go" {
		t.Errorf("expected llm.go, got %s", ir.Files[0].RelPath)
	}
}
