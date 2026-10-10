package callgraph

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pyTierGraph(t *testing.T) *CallGraph {
	t.Helper()
	res, err := BuildAndEnrich(context.Background(), PipelineOpts{
		Root:         writePyFixture(t),
		MaxFileBytes: 1 << 20,
		TypedEnrich:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return res.CG
}

// A Python repo answered at the basic tier must carry a note saying the typed
// pass did not run; before, only the bare string tier:"basic" distinguished a
// short caller list from "no callers".
func TestTierNote_UntrustedPythonSaysTypedPassSkipped(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")
	bin := t.TempDir()
	script := "#!/bin/sh\n: > " + marker + "\n"
	if err := os.WriteFile(filepath.Join(bin, "scip-python"), []byte(script), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	cg := pyTierGraph(t)

	if cg.Tier != "basic" {
		t.Fatalf("tier = %q, want basic", cg.Tier)
	}
	note := TierNote(cg)
	for _, want := range []string{"tier basic", "python: scip-python was not run", "not an operator-trusted checkout", "not proof"} {
		if !strings.Contains(note, want) {
			t.Errorf("note %q lacks %q", note, want)
		}
	}
	if exists(marker) {
		t.Fatal("scip-python executed on an untrusted repo")
	}
}

func TestTierNote_MissingIndexerSaysNotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no scip-python anywhere
	note := TierNote(pyTierGraph(t))
	if !strings.Contains(note, "python: scip-python is not installed") {
		t.Fatalf("note = %q, want it to say scip-python is not installed", note)
	}
}

func TestTierNote_EmptyWhenNothingSkipped(t *testing.T) {
	for name, cg := range map[string]*CallGraph{
		"nil":                 nil,
		"enhanced":            {Tier: "enhanced", TypedSkipped: []string{"python: x"}},
		"basic, none skipped": {Tier: "basic"},
	} {
		if got := TierNote(cg); got != "" {
			t.Errorf("%s: note = %q, want empty", name, got)
		}
	}
}

// TraceRepo is the in-memory path call_trace takes for a "Class.method" query
// (the AGE fast path matches bare names only): it must find the Python method
// and carry the tier note.
func TestTraceRepo_PythonQualifiedMethodCarriesTierNote(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	res, err := TraceRepo(context.Background(), TraceRepoInput{
		Root:   writePyFixture(t),
		Symbol: "OpenAi.transcribe",
		Opts:   TraceOpts{Direction: "callers"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Root == nil || res.Root.Receiver != "OpenAi" {
		t.Fatalf("root = %+v, want OpenAi.transcribe", res.Root)
	}
	if got := len(res.Tree[0].Children); got != 8 {
		t.Fatalf("callers = %d, want 8", got)
	}
	if !strings.Contains(res.TierNote, "python: scip-python is not installed") {
		t.Fatalf("TierNote = %q", res.TierNote)
	}
}
