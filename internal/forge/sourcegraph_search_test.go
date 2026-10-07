package forge

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newTestSourcegraph returns a SourcegraphClient backed by the test server.
func newTestSourcegraph(srv *httptest.Server) *SourcegraphClient {
	return &SourcegraphClient{
		base:  srv.URL,
		http:  &http.Client{Timeout: 5 * time.Second},
		token: "",
	}
}

func sgStreamServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/.api/search/stream") {
			http.Error(w, "bad path "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestTranslateCodeQueryToSG(t *testing.T) {
	t.Parallel()
	tests := []struct{ in, want string }{
		{"func resize", "func resize"},
		{"ServeHTTP language:go", "ServeHTTP lang:go"},
		{"x path:src/*.js", `x file:src/.*\.js`},
		{"x -path:vendor", `x -file:vendor`},
		{"x extension:go", `x file:\.go$`},
		{"x filename:mux.go", `x file:(^|/)mux\.go$`},
		{"x repo:a/b", `x repo:^github\.com/a/b$`},
		{"x -repo:a/b", `x -repo:^github\.com/a/b$`},
		{`x repo:^github\.com/a/b$`, `x repo:^github\.com/a/b$`},
		{"x org:golang", `x repo:^github\.com/golang/`},
		{"x symbol:ServeHTTP", "x type:symbol ServeHTTP"},
		// GitHub-only qualifiers are dropped, not passed through to error out.
		{"x is:vendored", "x"},
		{"x sort:indexed", "x"},
		{"x license:MIT", "x"},
		{"x in:file", "x"},
		// Unknown qualifiers are dropped too (SG errors on them).
		{"x bogusqual:y", "x"},
		// SG-native qualifiers pass through.
		{"x file:foo", "x file:foo"},
		{"x lang:rust count:20", "x lang:rust count:20"},
		{"a OR b NOT c", "a OR b NOT c"},
		{`"exact phrase"`, `"exact phrase"`},
		{`/regex.+/`, `/regex.+/`},
	}
	for _, tc := range tests {
		if got := translateCodeQueryToSG(tc.in); got != tc.want {
			t.Errorf("translate(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildSourcegraphQuery(t *testing.T) {
	t.Parallel()
	opt := SearchCodeOptions{
		Language:       "go",
		FileExtensions: []string{".ts"},
		ExcludeRepos:   []string{"bad/repo"},
		ExcludePaths:   []string{"vendor"},
	}
	q, err := buildSourcegraphQuery("ServeHTTP", []string{"o/r"}, opt, 25)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"context:global",
		`repo:^github\.com/o/r$`,
		`-repo:^github\.com/bad/repo$`,
		"lang:go",
		`file:\.ts$`,
		`-file:vendor`,
		"count:25",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query missing %q: %s", want, q)
		}
	}
}

func TestParseSourcegraphStream(t *testing.T) {
	t.Parallel()
	body := `event: filters
data: [{"value":"lang:go"}]

event: matches
data: [{"type":"content","path":"a.go","repository":"github.com/o/r","repoStars":42,"commit":"abc123","language":"Go","lineMatches":[{"line":"func ServeHTTP()","lineNumber":10,"offsetAndLengths":[[5,9]]}]}]

event: matches
data: [{"type":"repo","repository":"github.com/x/y"}]

event: progress
data: {"done":true,"matchCount":1}

event: done
data: {}
`
	matches, err := parseSourcegraphStream(strings.NewReader(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 2 { // repo-type match also decodes; filtered downstream
		t.Fatalf("len(matches) = %d, want 2", len(matches))
	}
	m := matches[0]
	if m.Repository != "github.com/o/r" || m.RepoStars != 42 || m.Commit != "abc123" {
		t.Errorf("bad match: %+v", m)
	}
	if m.LineMatches[0].LineNumber != 10 || m.LineMatches[0].OffsetAndLengths[0][0] != 5 {
		t.Errorf("bad lineMatch: %+v", m.LineMatches[0])
	}
}

func TestConvertSGMatch_LineMatches(t *testing.T) {
	t.Parallel()
	m := sgContentMatch{
		Type:       "content",
		Path:       "src/mux.go",
		Repository: "github.com/gorilla/mux",
		RepoStars:  21000,
		Commit:     "deadbeef",
		Language:   "Go",
		LineMatches: []sgLineMatch{
			{Line: "func ServeHTTP(w http.ResponseWriter) {", LineNumber: 9, OffsetAndLengths: [][]int{{5, 9}}},
			{Line: "\treturn ServeHTTP(x)", LineNumber: 20, OffsetAndLengths: [][]int{{8, 9}}},
		},
	}
	r, ok := convertSGMatch(m)
	if !ok {
		t.Fatal("conversion failed")
	}
	if r.Repo != "gorilla/mux" || r.Engine != "sourcegraph" {
		t.Errorf("Repo=%q Engine=%q", r.Repo, r.Engine)
	}
	if r.URL != "https://github.com/gorilla/mux/blob/deadbeef/src/mux.go#L10" {
		t.Errorf("URL = %q", r.URL)
	}
	if len(r.Lines) != 2 || r.Lines[0] != 10 || r.Lines[1] != 21 {
		t.Errorf("Lines = %v, want [10 21] (1-based)", r.Lines)
	}
	if len(r.Matched) != 1 || r.Matched[0] != "ServeHTTP" {
		t.Errorf("Matched = %v", r.Matched)
	}
	if r.rawFrag == "" || r.Commit != "deadbeef" || r.Stars != 21000 {
		t.Errorf("rawFrag=%q Commit=%q Stars=%d", r.rawFrag, r.Commit, r.Stars)
	}
}

func TestConvertSGMatch_ChunkMatches(t *testing.T) {
	t.Parallel()
	content := "line before\nfunc Target() {}\nline after"
	// range covers "Target" — offset 16, line 1 (chunk-relative)
	m := sgContentMatch{
		Type:       "content",
		Path:       "f.go",
		Repository: "github.com/o/r",
		ChunkMatches: []sgChunkMatch{
			{
				Content:      content,
				ContentStart: sgPosition{Offset: 100, Line: 39, Column: 0},
				Ranges: []struct {
					Start sgPosition `json:"start"`
					End   sgPosition `json:"end"`
				}{
					{Start: sgPosition{Offset: 17, Line: 1, Column: 5}, End: sgPosition{Offset: 23, Line: 1, Column: 11}},
				},
			},
		},
	}
	r, ok := convertSGMatch(m)
	if !ok {
		t.Fatal("conversion failed")
	}
	if len(r.Lines) != 1 || r.Lines[0] != 41 { // contentStart.Line(39,0-based) + rel(1) + 1-based
		t.Errorf("Lines = %v, want [41]", r.Lines)
	}
	if len(r.Matched) != 1 || r.Matched[0] != "Target" {
		t.Errorf("Matched = %v, want [Target]", r.Matched)
	}
	if !strings.Contains(r.Content, "func Target()") {
		t.Errorf("Content missing chunk:\n%s", r.Content)
	}
}

func TestSearchCode_SourcegraphEngine(t *testing.T) {
	t.Parallel()
	sse := `event: matches
data: [{"type":"content","path":"mux.go","repository":"github.com/gorilla/mux","repoStars":21000,"commit":"abc","lineMatches":[{"line":"func ServeHTTP() {","lineNumber":4,"offsetAndLengths":[[5,9]]}]}]

event: done
data: {}
`
	sgSrv := sgStreamServer(t, sse)

	ghMux := http.NewServeMux()
	gh := newTestGitHubForge(t, ghMux) // GitHub server has no /search/code -> would 404
	gh.sg = newTestSourcegraph(sgSrv)

	res, err := gh.SearchCode(context.Background(), "ServeHTTP language:go", nil,
		SearchCodeOptions{Engine: "sourcegraph", MaxResults: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("len(results) = %d", len(res.Results))
	}
	r := res.Results[0]
	if r.Engine != "sourcegraph" || r.Repo != "gorilla/mux" {
		t.Errorf("engine=%q repo=%q", r.Engine, r.Repo)
	}
	if len(r.Lines) != 1 || r.Lines[0] != 5 {
		t.Errorf("Lines = %v, want [5]", r.Lines)
	}
	// The GitHub-syntax query must have been translated before hitting SG —
	// check what the mock received.
}

func TestSearchCode_AutoSupplementsOnEmpty(t *testing.T) {
	t.Parallel()
	sse := `event: matches
data: [{"type":"content","path":"zoekt.go","repository":"github.com/sourcegraph/zoekt","repoStars":900,"commit":"c0ffee","lineMatches":[{"line":"type searcher struct {","lineNumber":2,"offsetAndLengths":[[5,8]]}]}]

event: done
data: {}
`
	sgSrv := sgStreamServer(t, sse)

	ghMux := http.NewServeMux()
	ghMux.HandleFunc("GET /search/code", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"total_count":0,"incomplete_results":true,"items":[]}`))
	})
	gh := newTestGitHubForge(t, ghMux)
	gh.sg = newTestSourcegraph(sgSrv)

	res, err := gh.SearchCode(context.Background(), "searcher", nil,
		SearchCodeOptions{Engine: "auto", MaxResults: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("len(results) = %d, want 1 from sourcegraph fallback", len(res.Results))
	}
	if res.Results[0].Engine != "sourcegraph" {
		t.Errorf("Engine = %q, want sourcegraph", res.Results[0].Engine)
	}
	if res.Results[0].Repo != "sourcegraph/zoekt" {
		t.Errorf("Repo = %q", res.Results[0].Repo)
	}
}

func TestSearchCode_AutoSkipsSourcegraphWhenFull(t *testing.T) {
	t.Parallel()
	var sgCalled bool
	sgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		sgCalled = true
		w.Write([]byte("event: done\ndata: {}\n\n"))
	}))
	t.Cleanup(sgSrv.Close)

	ghMux := http.NewServeMux()
	ghMux.HandleFunc("GET /search/code", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"total_count":1,"items":[{"name":"a.go","path":"a.go","html_url":"https://github.com/o/r/blob/m/a.go","repository":{"full_name":"o/r"},"text_matches":[{"fragment":"needle x","matches":[{"text":"needle","indices":[0,6]}]}]}]}`)
	})
	gh := newTestGitHubForge(t, ghMux)
	gh.sg = newTestSourcegraph(sgSrv)

	res, err := gh.SearchCode(context.Background(), "needle", nil,
		SearchCodeOptions{Engine: "auto", MaxResults: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("len(results) = %d", len(res.Results))
	}
	if sgCalled {
		t.Error("sourcegraph called despite complete github result")
	}
}

func TestSearchCode_AutoMergesDuplicatePaths(t *testing.T) {
	t.Parallel()
	sse := `event: matches
data: [{"type":"content","path":"a.go","repository":"github.com/o/r","repoStars":5,"commit":"sha1","lineMatches":[{"line":"needle x","lineNumber":7,"offsetAndLengths":[[0,6]]}]},{"type":"content","path":"b.go","repository":"github.com/p/q","repoStars":3,"commit":"sha2","lineMatches":[{"line":"needle y","lineNumber":1,"offsetAndLengths":[[0,6]]}]}]

event: done
data: {}
`
	sgSrv := sgStreamServer(t, sse)

	ghMux := http.NewServeMux()
	ghMux.HandleFunc("GET /search/code", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"total_count":2,"incomplete_results":true,"items":[{"name":"a.go","path":"a.go","html_url":"https://github.com/o/r/blob/m/a.go","repository":{"full_name":"o/r"},"text_matches":[{"fragment":"needle x","matches":[{"text":"needle","indices":[0,6]}]}]}]}`)
	})
	gh := newTestGitHubForge(t, ghMux)
	gh.sg = newTestSourcegraph(sgSrv)

	res, err := gh.SearchCode(context.Background(), "needle", nil,
		SearchCodeOptions{Engine: "auto", MaxResults: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Results) != 2 {
		t.Fatalf("len(results) = %d, want 2 (merged, not duplicated)", len(res.Results))
	}
	first := res.Results[0]
	if first.Engine != "github" {
		t.Errorf("first engine = %q, want github (github result wins for shared path)", first.Engine)
	}
	// SG-only fields merged into the github result for the shared path.
	if len(first.Lines) != 1 || first.Lines[0] != 8 || first.Commit != "sha1" || first.Stars != 5 {
		t.Errorf("merged fields missing: Lines=%v Commit=%q Stars=%d", first.Lines, first.Commit, first.Stars)
	}
	if res.Results[1].Engine != "sourcegraph" || res.Results[1].Repo != "p/q" {
		t.Errorf("second = %+v", res.Results[1])
	}
}

func TestSearchCode_SourcegraphFailureKeepsGithub(t *testing.T) {
	t.Parallel()
	sgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(sgSrv.Close)

	ghMux := http.NewServeMux()
	ghMux.HandleFunc("GET /search/code", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"total_count":1,"items":[{"name":"a.go","path":"a.go","html_url":"https://github.com/o/r/blob/m/a.go","repository":{"full_name":"o/r"},"text_matches":[{"fragment":"needle x","matches":[{"text":"needle","indices":[0,6]}]}]}]}`)
	})
	gh := newTestGitHubForge(t, ghMux)
	gh.sg = newTestSourcegraph(sgSrv)

	res, err := gh.SearchCode(context.Background(), "needle", nil,
		SearchCodeOptions{Engine: "auto", MaxResults: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Results) != 1 || res.Results[0].Engine != "github" {
		t.Fatalf("github result lost on sg failure: %+v", res.Results)
	}
}

func TestSearchCode_SourcegraphEngineNotConfigured(t *testing.T) {
	t.Parallel()
	ghMux := http.NewServeMux()
	gh := newTestGitHubForge(t, ghMux)
	_, err := gh.SearchCode(context.Background(), "x", nil, SearchCodeOptions{Engine: "sourcegraph"})
	if err == nil {
		t.Error("expected error when sourcegraph not configured")
	}
}
