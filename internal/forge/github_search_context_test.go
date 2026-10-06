package forge

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestSearchCode_DedupIdenticalFragments(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search/code", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"name": "README.md", "path": "README.md",
					"html_url":   "https://github.com/a/one/blob/main/README.md",
					"repository": map[string]any{"full_name": "a/one"},
					"text_matches": []map[string]any{
						{"fragment": "use the thing\ninstall it", "matches": []map[string]any{{"text": "thing", "indices": []int{8, 13}}}},
					},
				},
				{
					// vendored copy — same fragment text, different whitespace
					"name": "README.md", "path": "vendor/x/README.md",
					"html_url":   "https://github.com/b/two/blob/main/vendor/x/README.md",
					"repository": map[string]any{"full_name": "b/two"},
					"text_matches": []map[string]any{
						{"fragment": "use the  thing\ninstall it", "matches": []map[string]any{{"text": "thing", "indices": []int{8, 13}}}},
					},
				},
				{
					"name": "main.go", "path": "main.go",
					"html_url":   "https://github.com/c/three/blob/main/main.go",
					"repository": map[string]any{"full_name": "c/three"},
					"text_matches": []map[string]any{
						{"fragment": "thing := setup()", "matches": []map[string]any{{"text": "thing", "indices": []int{0, 5}}}},
					},
				},
			},
		})
	})

	g := newTestGitHubForge(t, mux)
	res, err := g.SearchCode(context.Background(), "thing", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Results) != 2 {
		t.Fatalf("len(results) = %d, want 2 (dup dropped)", len(res.Results))
	}
	if res.Results[0].Repo != "a/one" || res.Results[1].Repo != "c/three" {
		t.Errorf("wrong survivors: %v, %v", res.Results[0].Repo, res.Results[1].Repo)
	}
	if len(res.Results[0].Matched) != 1 || res.Results[0].Matched[0] != "thing" {
		t.Errorf("Matched = %v, want [thing]", res.Results[0].Matched)
	}
}

func TestSearchCode_ContextExpansion(t *testing.T) {
	t.Parallel()
	file := strings.Join([]string{
		"line 1 header", "line 2", "line 3",
		"func target() {", "  doWork()", "}", "line 7 footer",
	}, "\n")
	frag := "func target() {\n  doWork()"

	mux := http.NewServeMux()
	mux.HandleFunc("GET /search/code", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"name": "f.go", "path": "src/f.go",
					"html_url":   "https://github.com/o/r/blob/main/src/f.go",
					"repository": map[string]any{"full_name": "o/r"},
					"text_matches": []map[string]any{
						{"fragment": frag, "matches": []map[string]any{{"text": "target", "indices": []int{5, 11}}}},
					},
				},
			},
		})
	})
	mux.HandleFunc("GET /repos/o/r/contents/src/f.go", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(ghHeaderAccept) != ghMediaTypeRaw {
			http.Error(w, "want raw accept", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(file))
	})

	g := newTestGitHubForge(t, mux)
	res, err := g.SearchCode(context.Background(), "target", nil,
		SearchCodeOptions{ContextLines: 1, ContextResults: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("len(results) = %d", len(res.Results))
	}
	got := res.Results[0].Context
	if !strings.Contains(got, "line 3") || !strings.Contains(got, "func target() {") || !strings.Contains(got, "}") {
		t.Errorf("context missing window around match:\n%s", got)
	}
	if strings.Contains(got, "line 1 header") || strings.Contains(got, "line 7 footer") {
		t.Errorf("context window too wide:\n%s", got)
	}
	if res.Results[0].ContextStart != 3 {
		t.Errorf("ContextStart = %d, want 3", res.Results[0].ContextStart)
	}
}

func TestSearchCode_ContextLocateByMatchedTerm(t *testing.T) {
	t.Parallel()
	// Fragment text mangled (e.g. upstream re-render) — locate falls back to
	// the longest matched term.
	file := "aaa\nbbb needle-matched\nccc\n"
	mux := http.NewServeMux()
	mux.HandleFunc("GET /search/code", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"name": "f.go", "path": "f.go",
					"html_url":   "https://github.com/o/r/blob/main/f.go",
					"repository": map[string]any{"full_name": "o/r"},
					"text_matches": []map[string]any{
						{"fragment": "different rendering", "matches": []map[string]any{{"text": "needle-matched", "indices": []int{0, 14}}}},
					},
				},
			},
		})
	})
	mux.HandleFunc("GET /repos/o/r/contents/f.go", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(file))
	})

	g := newTestGitHubForge(t, mux)
	res, err := g.SearchCode(context.Background(), "needle", nil,
		SearchCodeOptions{ContextLines: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := res.Results[0].Context
	if !strings.Contains(got, "needle-matched") || !strings.Contains(got, "aaa") || !strings.Contains(got, "ccc") {
		t.Errorf("fallback locate failed:\n%s", got)
	}
}

func TestBuildGitHubCodeSearchQuery_ExcludePaths(t *testing.T) {
	t.Parallel()
	q, err := buildGitHubCodeSearchQuery("func", nil, nil, nil, "", []string{"vendor", "*.generated.go"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(q, "-path:vendor") || !strings.Contains(q, "-path:*.generated.go") {
		t.Errorf("missing -path qualifiers: %s", q)
	}
	// Duplicate qualifier in user query is not re-added.
	q2, err := buildGitHubCodeSearchQuery("func -path:vendor", nil, nil, nil, "", []string{"vendor"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Count(q2, "-path:vendor") != 1 {
		t.Errorf("duplicate -path qualifier: %s", q2)
	}
	// Invalid char rejected.
	if _, err := buildGitHubCodeSearchQuery("func", nil, nil, nil, "", []string{"foo;rm -rf"}); err == nil {
		t.Error("expected error for invalid exclude path")
	}
}
