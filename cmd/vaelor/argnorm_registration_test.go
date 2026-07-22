package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoDirectMCPServerAddTool guards the argnorm middleware invariant: the
// normalization registry fail-closes on ITS OWN membership, so a tool
// registered via mcpserver.AddTool directly (bypassing argnorm.AddTool) would
// be silently uncallable — the middleware would answer `unknown tool` for it.
// Every tool registration in this package must go through argnorm.AddTool.
func TestNoDirectMCPServerAddTool(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(src), "mcpserver.AddTool(") {
			t.Errorf("%s: direct mcpserver.AddTool call — use addTool, "+
				"otherwise the argnorm middleware rejects the tool as unknown", name)
		}
		// argnorm.AddTool directly (bypassing addTool) registers fine but
		// skips budget shaping + took_ms — the wrapper chain must be
		// addTool → argnorm.AddTool → mcpserver.AddTool, with the argnorm
		// call living only inside addtool.go.
		if name != "addtool.go" && strings.Contains(string(src), "argnorm.AddTool(") {
			t.Errorf("%s: direct argnorm.AddTool call — use addTool so the "+
				"response budget and took_ms wrapper applies", name)
		}
	}
}

// TestRepoSchemaOptionalOnInferenceTools guards the #569 live path: the
// framework derives JSON-schema `required` from non-omitempty json tags, so a
// bare `json:"repo"` makes validation reject a missing repo BEFORE the
// handler runs — turning resolveOrInferRepo / shortMissingRepoMsg into dead
// code (caught live on v1.50.0: the generic framework error shipped instead
// of inference). The tag must stay `repo,omitempty` on these tools.
func TestRepoSchemaOptionalOnInferenceTools(t *testing.T) {
	for _, name := range []string{
		"tool_code_search.go",
		"tool_code_research.go",
		"tool_semantic_search.go",
	} {
		src, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(src), "`json:\"repo\"") {
			t.Errorf("%s: repo tag lacks omitempty — framework marks it "+
				"required and the missing-repo inference/short-error path "+
				"never runs", name)
		}
	}
}
