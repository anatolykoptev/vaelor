package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/anatolykoptev/go-kit/llm"
	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/jackc/pgx/v5/pgxpool"
)

// countingLLM counts Complete calls and fails every one of them, standing in
// for the flaky free model chain.
type countingLLM struct{ calls atomic.Int32 }

func (c *countingLLM) Complete(context.Context, string, string, ...llm.ChatOption) (string, error) {
	c.calls.Add(1)
	return "", llm.ErrUnavailable
}

// TestCodeGraph_ExplicitTemplateE2E drives handleCodeGraph against a real AGE
// graph. An explicit template must answer from the graph with ZERO LLM calls
// (no classify, no narrative) even though an LLM is configured and failing;
// a natural-language query on the same broken LLM must fail with the hint
// pointing at template + params.
//
// Skipped unless DATABASE_URL is set (CI preflight provides postgres+age).
func TestCodeGraph_ExplicitTemplateE2E(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping code_graph explicit-template e2e test")
	}
	ctx := context.Background()

	root := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":  "module example.com/cgexplicit\n\ngo 1.22\n",
		"main.go": "package main\n\nfunc main() { Caller() }\n\nfunc Caller() { Target() }\n\nfunc Target() {}\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() }) // registered first -> runs last
	store := codegraph.NewStore(pool)
	repoKey := codegraph.GraphNameFor(root)
	t.Cleanup(func() { _ = store.DropGraph(ctx, repoKey, repoKey) })

	cfg := Config{GraphTTLLocal: 3600, GraphTTLRemote: 3600}
	indexCfg := codegraph.IndexConfig{TTLLocal: cfg.GraphTTLLocal, TTLRemote: cfg.GraphTTLRemote}
	if _, err := codegraph.IndexRepo(ctx, store, root, false, indexCfg); err != nil {
		t.Fatalf("IndexRepo: %v", err)
	}

	broken := &countingLLM{}
	deps := analyze.Deps{LLM: broken, LLMHasKey: true}

	res, err := handleCodeGraph(ctx, CodeGraphInput{
		Repo: root, Template: "who_calls", Params: map[string]string{"name": "Target"},
	}, cfg, deps, store)
	if err != nil {
		t.Fatalf("handleCodeGraph: %v", err)
	}
	text := resultText(res)
	if res.IsError || !strings.Contains(text, "Caller") {
		t.Fatalf("explicit who_calls Target: want a row naming Caller, got (isError=%v) %q", res.IsError, text)
	}
	if strings.Contains(text, "<status>") {
		t.Fatalf("graph not treated as fresh after IndexRepo: %q", text)
	}
	if n := broken.calls.Load(); n != 0 {
		t.Errorf("explicit template made %d LLM calls, want 0", n)
	}

	res, err = handleCodeGraph(ctx, CodeGraphInput{Repo: root, Query: "who calls Target?"}, cfg, deps, store)
	if err != nil {
		t.Fatalf("handleCodeGraph NL: %v", err)
	}
	if text := resultText(res); !res.IsError || !strings.Contains(text, "retry with template") || !strings.Contains(text, "who_calls(name)") {
		t.Errorf("NL query on a failing LLM: want error with template hint, got (isError=%v) %q", res.IsError, text)
	}
}
