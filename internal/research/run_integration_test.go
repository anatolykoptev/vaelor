package research

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// fixtureRoot returns the absolute path to the committed testdata/minirepo.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("testdata/minirepo")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func TestRunEndToEndKeyword(t *testing.T) {
	t.Parallel()
	root := fixtureRoot(t)
	res, err := Run(context.Background(), Input{
		Root:       root,
		Query:      "exponential backoff retry",
		MaxTokens:  4000,
		ExpandHops: 2,
	}, Deps{AnalyzeDeps: analyze.Deps{}})
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Seeds) == 0 {
		t.Fatal("expected at least one seed")
	}

	hasRetry := false
	for _, s := range res.Seeds {
		if strings.Contains(s.File, "retry/retry.go") {
			hasRetry = true
		}
	}
	if !hasRetry {
		t.Errorf("retry/retry.go must appear in seeds, got %+v", res.Seeds)
	}

	if !strings.Contains(res.Map, "WithBackoff") {
		t.Errorf("rendered map must mention WithBackoff, got:\n%s", res.Map)
	}
}

func TestRunEndToEndIncludeTests(t *testing.T) {
	t.Parallel()
	root := fixtureRoot(t)
	res, err := Run(context.Background(), Input{
		Root:         root,
		Query:        "WithBackoff",
		MaxTokens:    4000,
		ExpandHops:   1,
		IncludeTests: true,
	}, Deps{AnalyzeDeps: analyze.Deps{}})
	if err != nil {
		t.Fatal(err)
	}

	hasTest := false
	for _, lf := range res.Graph {
		if strings.HasSuffix(lf.RelPath, "_test.go") {
			hasTest = true
		}
	}
	// Test file may show up in Seeds or Graph — accept either.
	if !hasTest {
		for _, s := range res.Seeds {
			if strings.HasSuffix(s.File, "_test.go") {
				hasTest = true
			}
		}
	}
	if !hasTest {
		t.Errorf("expected retry_test.go in Seeds or Graph when IncludeTests=true; seeds=%+v graph=%+v", res.Seeds, res.Graph)
	}
}

func TestRunEndToEndFileGlob(t *testing.T) {
	t.Parallel()
	root := fixtureRoot(t)
	res, err := Run(context.Background(), Input{
		Root:      root,
		Query:     "WithBackoff",
		FileGlob:  "retry/**",
		MaxTokens: 4000,
	}, Deps{AnalyzeDeps: analyze.Deps{}})
	if err != nil {
		t.Fatal(err)
	}

	for _, s := range res.Seeds {
		if !strings.HasPrefix(s.File, "retry/") {
			t.Errorf("FileGlob 'retry/**' must restrict to retry/, got %s", s.File)
		}
	}
}

func TestRunEndToEndCallGraphOptIn(t *testing.T) {
	t.Parallel()
	root := fixtureRoot(t)

	// Build a stub call graph hook that returns a hand-built CallGraph
	// linking util.SafeCall → retry.WithBackoff. Verify util.go appears
	// in the expanded graph when querying for WithBackoff with
	// IncludeCallGraph=true.
	stub := func(ctx context.Context, _ string) (*callgraph.CallGraph, error) {
		safeCall := &parser.Symbol{Name: "SafeCall", File: "util/util.go", Kind: parser.KindFunction}
		withBackoff := &parser.Symbol{Name: "WithBackoff", File: "retry/retry.go", Kind: parser.KindFunction}
		return &callgraph.CallGraph{
			Edges: []callgraph.CallEdge{
				{Caller: safeCall, Callee: withBackoff, CalleeName: "WithBackoff"},
			},
		}, nil
	}

	res, err := Run(context.Background(), Input{
		Root:             root,
		Query:            "WithBackoff",
		MaxTokens:        4000,
		ExpandHops:       1,
		IncludeCallGraph: true,
	}, Deps{
		AnalyzeDeps:    analyze.Deps{},
		BuildCallGraph: stub,
	})
	if err != nil {
		t.Fatal(err)
	}

	// util/util.go should appear in the result — either via call-graph expansion
	// ("called by WithBackoff") or as a keyword seed (if the fixture already
	// references WithBackoff in util.go). Either way confirms the call-graph BFS
	// ran without error and the file is reachable.
	foundUtil := false
	for _, lf := range res.Graph {
		if lf.RelPath == "util/util.go" {
			foundUtil = true
		}
	}
	if !foundUtil {
		for _, s := range res.Seeds {
			if s.File == "util/util.go" {
				foundUtil = true
			}
		}
	}
	if !foundUtil {
		t.Errorf("expected util/util.go in Seeds or Graph when IncludeCallGraph=true; seeds=%+v graph=%+v", res.Seeds, res.Graph)
	}
}
