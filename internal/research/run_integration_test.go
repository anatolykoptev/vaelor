package research

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/analyze"
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
