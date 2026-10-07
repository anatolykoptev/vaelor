package forge

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	kitcache "github.com/anatolykoptev/go-kit/cache"
)

// TestBuildBlackbirdQuery verifies qualifier translation: raw query (incl.
// blackbird-only syntax) passes through; structured filters map to
// repo:/lang:/-repo:/-path: and extension → path regex.
func TestBuildBlackbirdQuery(t *testing.T) {
	q := buildBlackbirdQuery(
		"symbol:ServeHTTP is:vendored",
		[]string{"gorilla/mux"},
		SearchCodeOptions{
			Language:       "go",
			FileExtensions: []string{"go"},
			ExcludeRepos:   []string{"bad/repo"},
			ExcludePaths:   []string{"vendor"},
		},
	)
	for _, want := range []string{
		"symbol:ServeHTTP is:vendored",
		"repo:gorilla/mux",
		"lang:go",
		`path:/\.go$/`,
		"-repo:bad/repo",
		"-path:vendor",
	} {
		if !strings.Contains(q, want) {
			t.Errorf("query %q missing %q", q, want)
		}
	}
}

// TestConvertBBResult verifies a blackbird result maps into CodeResult using
// the real captured shape: snippets[].lines hold HTML-marked code, repo_nwo is
// flat, commit_sha pins the URL, matched_symbols feed Matched.
func TestConvertBBResult(t *testing.T) {
	var item bbCodeResult
	raw := `{
		"repo_nwo": "gorilla/mux",
		"path": "mux.go",
		"language_name": "Go",
		"ref_name": "refs/heads/main",
		"commit_sha": "db9d1d0073d27a0a2d9a8c1bc52aa0af4374d265",
		"line_number": 184,
		"matched_symbols": [{"fully_qualified_name": "Router.ServeHTTP", "kind": "SYMBOL_KIND_METHOD_DEF"}],
		"snippets": [{
			"format": "SNIPPET_FORMAT_HTML",
			"starting_line_number": 185,
			"ending_line_number": 187,
			"lines": [
				"<span class=pl-k>func</span> (<span class=pl-s1>r</span> *<span class=pl-smi>Router</span>) <mark><span class=pl-c1>ServeHTTP</span></mark>(w http.<span class=pl-smi>ResponseWriter</span>) {",
				"<span class=pl-c>// plain comment, no mark</span>",
				"\thandler.<mark><span class=pl-smi>ServeHTTP</span></mark>(w, req)"
			]
		}]
	}`
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	r, ok := convertBBResult(item)
	if !ok {
		t.Fatal("convertBBResult rejected valid item")
	}
	if r.Repo != "gorilla/mux" || r.Path != "mux.go" || r.Engine != "blackbird" {
		t.Errorf("bad identity: %+v", r)
	}
	if !strings.Contains(r.Content, "func (r *Router) ServeHTTP(w http.ResponseWriter) {") {
		t.Errorf("content missing verbatim code: %q", r.Content)
	}
	if strings.Contains(r.Content, "<") && strings.Contains(r.Content, "mark") {
		t.Errorf("content leaked mark tags: %q", r.Content)
	}
	// Matched = symbol FQN + the marked term.
	if len(r.Matched) != 2 || r.Matched[0] != "Router.ServeHTTP" || r.Matched[1] != "ServeHTTP" {
		t.Errorf("Matched = %v", r.Matched)
	}
	// Lines = primary line_number + mark-bearing snippet lines (185, 187).
	if len(r.Lines) != 3 || r.Lines[0] != 184 || r.Lines[1] != 185 || r.Lines[2] != 187 {
		t.Errorf("Lines = %v, want [184 185 187]", r.Lines)
	}
	wantURL := "https://github.com/gorilla/mux/blob/db9d1d0073d27a0a2d9a8c1bc52aa0af4374d265/mux.go#L184"
	if r.URL != wantURL {
		t.Errorf("URL = %q, want %q", r.URL, wantURL)
	}
	if r.Commit != "db9d1d0073d27a0a2d9a8c1bc52aa0af4374d265" {
		t.Errorf("Commit = %q", r.Commit)
	}
	if r.rawFrag == "" {
		t.Error("rawFrag empty — context expansion has nothing to locate")
	}
}

// newBlackbirdTestServer emulates the go-wowa interact endpoint.
func newBlackbirdTestServer(t *testing.T, fr bbFetchResult, reqs *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reqs != nil {
			reqs.Add(1)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		// Assert the load-bearing transport fields.
		if body["mode"] != "default" {
			t.Errorf("mode = %v, want default (incognito context = empty cookie jar)", body["mode"])
		}
		if body["session"] == "" {
			t.Error("session missing")
		}
		resp := map[string]any{
			"status": "ok",
			"actions": []map[string]any{
				{"ok": true, "data": fr},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

func testRouteJSON() string {
	return `{"results":[{"repo_nwo":"a/b","path":"x.go","language_name":"Go","ref_name":"refs/heads/main","commit_sha":"deadbeef","line_number":5,"snippets":[{"format":"SNIPPET_FORMAT_HTML","starting_line_number":5,"ending_line_number":5,"lines":["<mark>hit</mark>"]}]}],"result_count":1,"page_count":1,"logged_in":true}`
}

// TestBlackbirdSearchHappyPath: one interact call → parsed route → CodeResult.
func TestBlackbirdSearchHappyPath(t *testing.T) {
	var reqs atomic.Int32
	srv := newBlackbirdTestServer(t, bbFetchResult{
		Status:  200,
		Payload: testRouteJSON(),
	}, &reqs)
	defer srv.Close()

	bb := NewBlackbirdClient(srv.URL, "test-session", "", WithBlackbirdCache(kitcache.New(kitcache.Config{L1MaxItems: 64})))
	g := newGitHubForgeWithBase("", AppConfig{}, "http://127.0.0.1:1", WithBlackbird(bb))

	res, err := g.searchCodeBlackbird(context.Background(), "ServeHTTP", nil, SearchCodeOptions{}, 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("got %d results, want 1", len(res.Results))
	}
	r := res.Results[0]
	if r.Repo != "a/b" || r.Engine != "blackbird" {
		t.Errorf("bad result: %+v", r)
	}
	if res.Total != 1 {
		t.Errorf("Total = %d, want 1", res.Total)
	}
}

// TestBlackbirdLoggedOut: logged_in:false inside the route must surface
// errBlackbirdLoggedOut — silently treating it as an empty result would hide
// session expiry.
func TestBlackbirdLoggedOut(t *testing.T) {
	srv := newBlackbirdTestServer(t, bbFetchResult{
		Status:  200,
		Payload: `{"results":[],"logged_in":false}`,
	}, nil)
	defer srv.Close()

	bb := NewBlackbirdClient(srv.URL, "", "", WithBlackbirdCache(kitcache.New(kitcache.Config{L1MaxItems: 64})))
	g := newGitHubForgeWithBase("", AppConfig{}, "http://127.0.0.1:1", WithBlackbird(bb))

	_, err := g.searchCodeBlackbird(context.Background(), "x", nil, SearchCodeOptions{}, 0)
	if !errors.Is(err, errBlackbirdLoggedOut) {
		t.Fatalf("err = %v, want errBlackbirdLoggedOut", err)
	}
}

// TestBlackbirdLoginRedirect: a landed /login URL means the session died.
func TestBlackbirdLoginRedirect(t *testing.T) {
	srv := newBlackbirdTestServer(t, bbFetchResult{
		Status:    200,
		LandedURL: "https://github.com/login?return_to=...",
		Payload:   `{"results":[]}`,
	}, nil)
	defer srv.Close()

	bb := NewBlackbirdClient(srv.URL, "", "", WithBlackbirdCache(kitcache.New(kitcache.Config{L1MaxItems: 64})))
	g := newGitHubForgeWithBase("", AppConfig{}, "http://127.0.0.1:1", WithBlackbird(bb))

	_, err := g.searchCodeBlackbird(context.Background(), "x", nil, SearchCodeOptions{}, 0)
	if !errors.Is(err, errBlackbirdLoggedOut) {
		t.Fatalf("err = %v, want errBlackbirdLoggedOut", err)
	}
}

// TestAutoBlackbirdLastResort: engine=auto with GitHub returning nothing must
// escalate to blackbird; a full GitHub result must NOT touch the browser.
func TestAutoBlackbirdLastResort(t *testing.T) {
	var bbCalls atomic.Int32
	bbSrv := newBlackbirdTestServer(t, bbFetchResult{
		Status:  200,
		Payload: `{"results":[{"repo_nwo":"only/bb","path":"only.go","language_name":"Go","ref_name":"refs/heads/main","commit_sha":"cafe","line_number":1,"snippets":[{"format":"SNIPPET_FORMAT_HTML","starting_line_number":1,"ending_line_number":1,"lines":["<mark>hit</mark>"]}]}],"result_count":1,"page_count":1,"logged_in":true}`,
	}, &bbCalls)
	defer bbSrv.Close()

	ghSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count": 0, "incomplete_results": false, "items": []any{},
		})
	}))
	defer ghSrv.Close()

	bb := NewBlackbirdClient(bbSrv.URL, "", "", WithBlackbirdCache(kitcache.New(kitcache.Config{L1MaxItems: 64})))
	g := newGitHubForgeWithBase("", AppConfig{}, ghSrv.URL, WithBlackbird(bb), WithCache(kitcache.New(kitcache.Config{L1MaxItems: 64})))

	res, err := g.SearchCode(context.Background(), "uniqueterm", nil, SearchCodeOptions{Engine: "auto"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Results) != 1 || res.Results[0].Engine != "blackbird" {
		t.Fatalf("expected blackbird last-resort result, got %+v", res.Results)
	}
	if bbCalls.Load() == 0 {
		t.Error("blackbird never called on empty GitHub result")
	}
}

// TestAutoGitHubErrorEscalates: a GitHub failure (e.g. 422 on blackbird-only
// syntax) in auto mode escalates to the fallback engine instead of failing.
func TestAutoGitHubErrorEscalates(t *testing.T) {
	bbSrv := newBlackbirdTestServer(t, bbFetchResult{
		Status:  200,
		Payload: `{"results":[{"repo_nwo":"only/bb","path":"only.go","commit_sha":"cafe","line_number":1,"snippets":[{"starting_line_number":1,"lines":["<mark>hit</mark>"]}]}],"result_count":1,"page_count":1,"logged_in":true}`,
	}, nil)
	defer bbSrv.Close()

	ghSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer ghSrv.Close()

	bb := NewBlackbirdClient(bbSrv.URL, "", "", WithBlackbirdCache(kitcache.New(kitcache.Config{L1MaxItems: 64})))
	g := newGitHubForgeWithBase("", AppConfig{}, ghSrv.URL, WithBlackbird(bb), WithCache(kitcache.New(kitcache.Config{L1MaxItems: 64})))

	res, err := g.SearchCode(context.Background(), "ServeHTTP NOT is:archived", nil, SearchCodeOptions{Engine: "auto"})
	if err != nil {
		t.Fatalf("auto search should survive a GitHub 422: %v", err)
	}
	if len(res.Results) != 1 || res.Results[0].Engine != "blackbird" {
		t.Fatalf("expected blackbird result after GitHub error, got %+v", res.Results)
	}
}

// TestAutoGitHubErrorAllEmpty: GitHub failed AND fallbacks produced nothing —
// the original error surfaces rather than an ambiguous empty result.
func TestAutoGitHubErrorAllEmpty(t *testing.T) {
	bbSrv := newBlackbirdTestServer(t, bbFetchResult{
		Status: 200, Payload: `{"results":[],"logged_in":true}`,
	}, nil)
	defer bbSrv.Close()

	ghSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer ghSrv.Close()

	bb := NewBlackbirdClient(bbSrv.URL, "", "", WithBlackbirdCache(kitcache.New(kitcache.Config{L1MaxItems: 64})))
	g := newGitHubForgeWithBase("", AppConfig{}, ghSrv.URL, WithBlackbird(bb), WithCache(kitcache.New(kitcache.Config{L1MaxItems: 64})))

	_, err := g.SearchCode(context.Background(), "x NOT y", nil, SearchCodeOptions{Engine: "auto"})
	if err == nil {
		t.Fatal("expected the GitHub error to surface when all engines return empty")
	}
}

// TestAutoBlackbirdSkippedWhenFull: full GitHub page ⇒ zero browser calls.
func TestAutoBlackbirdSkippedWhenFull(t *testing.T) {
	var bbCalls atomic.Int32
	bbSrv := newBlackbirdTestServer(t, bbFetchResult{Status: 200, Payload: `{"results":[]}`}, &bbCalls)
	defer bbSrv.Close()

	ghSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count":        3,
			"incomplete_results": false,
			"items": []map[string]any{
				{"name": "a.go", "path": "a.go", "html_url": "https://github.com/x/y/blob/a.go", "repository": map[string]string{"full_name": "x/y"}, "text_matches": []map[string]any{{"fragment": "one", "matches": []any{}}}},
				{"name": "b.go", "path": "b.go", "html_url": "https://github.com/x/y/blob/b.go", "repository": map[string]string{"full_name": "x/y"}, "text_matches": []map[string]any{{"fragment": "two", "matches": []any{}}}},
				{"name": "c.go", "path": "c.go", "html_url": "https://github.com/x/y/blob/c.go", "repository": map[string]string{"full_name": "x/y"}, "text_matches": []map[string]any{{"fragment": "three", "matches": []any{}}}},
			},
		})
	}))
	defer ghSrv.Close()

	bb := NewBlackbirdClient(bbSrv.URL, "", "", WithBlackbirdCache(kitcache.New(kitcache.Config{L1MaxItems: 64})))
	g := newGitHubForgeWithBase("", AppConfig{}, ghSrv.URL, WithBlackbird(bb), WithCache(kitcache.New(kitcache.Config{L1MaxItems: 64})))

	res, err := g.SearchCode(context.Background(), "q", nil, SearchCodeOptions{Engine: "auto", MaxResults: 3})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Results) != 3 {
		t.Fatalf("got %d results, want 3", len(res.Results))
	}
	if bbCalls.Load() != 0 {
		t.Errorf("blackbird called %d times despite full GitHub result", bbCalls.Load())
	}
}
