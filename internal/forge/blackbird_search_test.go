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

// newBlackbirdTestServer emulates the go-wowa interact endpoint. Requests
// carrying a get_cookies action get the profile jar + UA; evaluate actions get
// fr (the embeddedData extraction result).
func newBlackbirdTestServer(t *testing.T, fr bbFetchResult, reqs *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reqs != nil {
			reqs.Add(1)
		}
		var body struct {
			Mode    string           `json:"mode"`
			Session string           `json:"session"`
			URL     string           `json:"url"`
			Actions []map[string]any `json:"actions"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		// Assert the load-bearing transport fields.
		if body.Mode != "default" {
			t.Errorf("mode = %v, want default (incognito context = empty cookie jar)", body.Mode)
		}
		if body.Session == "" {
			t.Error("session missing")
		}
		for _, a := range body.Actions {
			if a["type"] != "get_cookies" {
				continue
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "ok",
				"actions": []map[string]any{
					{"ok": true, "data": []map[string]any{
						{"name": "user_session", "value": "testsession", "domain": ".github.com", "http_only": true},
						{"name": "logged_in", "value": "yes", "domain": ".github.com"},
						{"name": "other", "value": "x", "domain": ".example.com"},
					}},
					{"ok": true, "data": "Mozilla/5.0 Test"},
				},
			})
			return
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

// newBBClient builds a client pointed at the wowa mock with ghBase set to the
// github.com mock.
func newBBClient(wowaURL, ghURL string, opts ...BlackbirdOption) *BlackbirdClient {
	b := NewBlackbirdClient(wowaURL, "", "", opts...)
	b.ghBase = ghURL
	return b
}

// rejectGithub is a github.com mock that refuses the bare-cookie request —
// driving the wowa-evaluate fallback in tests that exercise the browser path.
func rejectGithub(t *testing.T, status int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}))
}

func testRouteJSON() string {
	return `{"results":[{"repo_nwo":"a/b","path":"x.go","language_name":"Go","ref_name":"refs/heads/main","commit_sha":"deadbeef","line_number":5,"snippets":[{"format":"SNIPPET_FORMAT_HTML","starting_line_number":5,"ending_line_number":5,"lines":["<mark>hit</mark>"]}]}],"result_count":1,"page_count":1,"logged_in":true}`
}

// TestBlackbirdHTTPFastPath: the bare-cookie GET is the primary transport —
// a good JSON answer must resolve the search with zero evaluate navigations
// (only the one get_cookies pull).
func TestBlackbirdHTTPFastPath(t *testing.T) {
	var wowaReqs atomic.Int32
	var evalNavs atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wowaReqs.Add(1)
		var body struct {
			URL     string           `json:"url"`
			Actions []map[string]any `json:"actions"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.Contains(body.URL, "type=code") {
			evalNavs.Add(1)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "ok",
			"actions": []map[string]any{
				{"ok": true, "data": []map[string]any{
					{"name": "user_session", "value": "testsession", "domain": ".github.com"},
				}},
				{"ok": true, "data": "Mozilla/5.0 Test"},
			},
		})
	}))
	defer srv.Close()

	var ghReqs atomic.Int32
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ghReqs.Add(1)
		if r.Header.Get("Accept") != "application/json" {
			t.Error("missing Accept: application/json")
		}
		if !strings.Contains(r.Header.Get("Cookie"), "user_session=testsession") {
			t.Error("missing user_session cookie")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"payload": map[string]any{
				"blackbirdSearchRoute": json.RawMessage(testRouteJSON()),
			},
		})
	}))
	defer gh.Close()

	bb := newBBClient(srv.URL, gh.URL, WithBlackbirdCache(kitcache.New(kitcache.Config{L1MaxItems: 64})))
	g := newGitHubForgeWithBase("", AppConfig{}, "http://127.0.0.1:1", WithBlackbird(bb))

	res, err := g.searchCodeBlackbird(context.Background(), "ServeHTTP", nil, SearchCodeOptions{}, 0)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("got %d results, want 1", len(res.Results))
	}
	if evalNavs.Load() != 0 {
		t.Errorf("fast path used the browser %d times — must be plain HTTP only", evalNavs.Load())
	}
	if ghReqs.Load() != 1 {
		t.Errorf("github mock hits = %d, want 1", ghReqs.Load())
	}
}

// TestBlackbirdCookieRefresh: a logged_in:false answer triggers one cookie
// re-pull from the profile + one retry — a silently-skipped refresh would
// surface a stale-cookie error instead.
func TestBlackbirdCookieRefresh(t *testing.T) {
	var wowaReqs atomic.Int32
	srv := newBlackbirdTestServer(t, bbFetchResult{Status: 200, Payload: testRouteJSON()}, &wowaReqs)
	defer srv.Close()

	var ghReqs atomic.Int32
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := ghReqs.Add(1)
		if n == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"payload": map[string]any{"blackbirdSearchRoute": map[string]any{
					"results": []any{}, "logged_in": false,
				}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"payload": map[string]any{
				"blackbirdSearchRoute": json.RawMessage(testRouteJSON()),
			},
		})
	}))
	defer gh.Close()

	bb := newBBClient(srv.URL, gh.URL, WithBlackbirdCache(kitcache.New(kitcache.Config{L1MaxItems: 64})))
	g := newGitHubForgeWithBase("", AppConfig{}, "http://127.0.0.1:1", WithBlackbird(bb))

	res, err := g.searchCodeBlackbird(context.Background(), "x", nil, SearchCodeOptions{}, 0)
	if err != nil {
		t.Fatalf("search after refresh: %v", err)
	}
	if len(res.Results) != 1 {
		t.Fatalf("got %d results, want 1", len(res.Results))
	}
	if ghReqs.Load() != 2 {
		t.Errorf("github hits = %d, want 2 (logged_out → retry)", ghReqs.Load())
	}
	if wowaReqs.Load() != 2 {
		t.Errorf("wowa calls = %d, want 2 (initial + refresh cookie pulls)", wowaReqs.Load())
	}
}

// TestBlackbirdHTTPLoggedOutStays: refresh didn't help — the error must
// surface, not an empty result.
func TestBlackbirdHTTPLoggedOutStays(t *testing.T) {
	var ghReqs atomic.Int32
	srv := newBlackbirdTestServer(t, bbFetchResult{Status: 200}, nil)
	defer srv.Close()
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ghReqs.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"payload": map[string]any{"blackbirdSearchRoute": map[string]any{
				"results": []any{}, "logged_in": false,
			}},
		})
	}))
	defer gh.Close()

	bb := newBBClient(srv.URL, gh.URL, WithBlackbirdCache(kitcache.New(kitcache.Config{L1MaxItems: 64})))
	g := newGitHubForgeWithBase("", AppConfig{}, "http://127.0.0.1:1", WithBlackbird(bb))

	_, err := g.searchCodeBlackbird(context.Background(), "x", nil, SearchCodeOptions{}, 0)
	if !errors.Is(err, errBlackbirdLoggedOut) {
		t.Fatalf("err = %v, want errBlackbirdLoggedOut", err)
	}
	if ghReqs.Load() != 2 {
		t.Errorf("github hits = %d, want 2 (one retry after refresh)", ghReqs.Load())
	}
}

// TestBlackbirdSearchHappyPath: the browser-evaluate fallback still works
// when the bare-cookie request is rejected (transport-level fingerprint).
func TestBlackbirdSearchHappyPath(t *testing.T) {
	var reqs atomic.Int32
	srv := newBlackbirdTestServer(t, bbFetchResult{
		Status:  200,
		Payload: testRouteJSON(),
	}, &reqs)
	defer srv.Close()
	gh := rejectGithub(t, http.StatusForbidden)
	defer gh.Close()

	bb := newBBClient(srv.URL, gh.URL, WithBlackbirdCache(kitcache.New(kitcache.Config{L1MaxItems: 64})))
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
	gh := rejectGithub(t, http.StatusForbidden)
	defer gh.Close()

	bb := newBBClient(srv.URL, gh.URL, WithBlackbirdCache(kitcache.New(kitcache.Config{L1MaxItems: 64})))
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
	gh := rejectGithub(t, http.StatusForbidden)
	defer gh.Close()

	bb := newBBClient(srv.URL, gh.URL, WithBlackbirdCache(kitcache.New(kitcache.Config{L1MaxItems: 64})))
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

	gh := rejectGithub(t, http.StatusForbidden)
	defer gh.Close()

	bb := newBBClient(bbSrv.URL, gh.URL, WithBlackbirdCache(kitcache.New(kitcache.Config{L1MaxItems: 64})))
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

	gh := rejectGithub(t, http.StatusForbidden)
	defer gh.Close()

	bb := newBBClient(bbSrv.URL, gh.URL, WithBlackbirdCache(kitcache.New(kitcache.Config{L1MaxItems: 64})))
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

	gh := rejectGithub(t, http.StatusForbidden)
	defer gh.Close()

	bb := newBBClient(bbSrv.URL, gh.URL, WithBlackbirdCache(kitcache.New(kitcache.Config{L1MaxItems: 64})))
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

	gh := rejectGithub(t, http.StatusForbidden)
	defer gh.Close()

	bb := newBBClient(bbSrv.URL, gh.URL, WithBlackbirdCache(kitcache.New(kitcache.Config{L1MaxItems: 64})))
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
