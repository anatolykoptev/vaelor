package webanalyze

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestAnalyze(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/analyze" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}
		resp := AnalyzeResponse{
			URL:    "https://example.com",
			Status: 200,
			Technologies: []Technology{
				{Name: "React", Categories: []string{"JavaScript frameworks"}, Confidence: 100},
			},
			Assets: Assets{
				Scripts:     []string{"app.js"},
				Stylesheets: []string{"style.css"},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Analyze(context.Background(), "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 200 {
		t.Errorf("expected status 200, got %d", resp.Status)
	}
	if len(resp.Technologies) != 1 || resp.Technologies[0].Name != "React" {
		t.Errorf("unexpected technologies: %v", resp.Technologies)
	}
}

func TestFetch(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := FetchResponse{Status: 200, Body: "hello"}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.Fetch(context.Background(), "https://example.com/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Body != "hello" {
		t.Errorf("expected body 'hello', got %q", resp.Body)
	}
}

func TestAnalyze_Error(t *testing.T) {
	t.Parallel()
	c, err := NewClient("http://127.0.0.1:1", "") // connection refused
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Analyze(context.Background(), "https://example.com")
	if err == nil {
		t.Error("expected error for unreachable server")
	}
}

// recordingOxBrowser is an httptest stand-in for ox-browser that captures the
// X-Internal-Secret header per request path.
type recordingOxBrowser struct {
	mu      sync.Mutex
	secrets map[string]string // path -> X-Internal-Secret value ("" = absent)
}

func newRecordingOxBrowser(t *testing.T) (*httptest.Server, *recordingOxBrowser) {
	t.Helper()
	rec := &recordingOxBrowser{secrets: make(map[string]string)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.secrets[r.URL.Path] = r.Header.Get("X-Internal-Secret")
		rec.mu.Unlock()
		switch r.URL.Path {
		case "/analyze":
			json.NewEncoder(w).Encode(AnalyzeResponse{URL: "https://example.com", Status: 200})
		case "/fetch":
			json.NewEncoder(w).Encode(FetchResponse{Status: 200, Body: "ok"})
		case "/crawl":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "event: done\ndata: {\"pages_crawled\":0,\"errors\":0,\"elapsed_ms\":1}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func (r *recordingOxBrowser) secretFor(path string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.secrets[path]
}

// T1 (ox-browser#173): every request a service-owned ox-browser client makes —
// the 30s JSON transport (/analyze, /fetch) AND the timeout-less SSE transport
// (/crawl) — must carry X-Internal-Secret when a secret is configured.
// Mutation gate: build the client without the svcauth wrap → all three go red.
func TestClientSendsInternalSecret(t *testing.T) {
	t.Parallel()
	srv, rec := newRecordingOxBrowser(t)

	c, err := NewClient(srv.URL, "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Analyze(context.Background(), "https://example.com"); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if _, err := c.Fetch(context.Background(), "https://example.com/a.js"); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if _, err := c.Crawl(context.Background(), CrawlInput{URL: "https://example.com"}); err != nil {
		t.Fatalf("Crawl: %v", err)
	}

	for _, path := range []string{"/analyze", "/fetch", "/crawl"} {
		if got := rec.secretFor(path); got != "s3cret" {
			t.Errorf("%s: X-Internal-Secret = %q, want %q", path, got, "s3cret")
		}
	}
}

// T3 (ox-browser#173): the secret is scoped to the ox-browser origin. A
// redirect from ox-browser to a different origin must NOT carry the header —
// a 302 must not leak the credential to a third party. The stand-in records
// that it DID receive the secret on the first hop, so the test distinguishes
// "scoped correctly" from "never sent at all".
func TestClientNoSecretOnForeignRedirect(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var foreignSecret string
	var foreignHits int
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		foreignSecret = r.Header.Get("X-Internal-Secret")
		foreignHits++
		mu.Unlock()
		json.NewEncoder(w).Encode(AnalyzeResponse{URL: "https://example.com", Status: 200})
	}))
	defer foreign.Close()

	var standinSecret string
	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		standinSecret = r.Header.Get("X-Internal-Secret")
		mu.Unlock()
		// ox-browser answers 302 to a foreign origin; the client follows.
		http.Redirect(w, r, foreign.URL+"/analyze", http.StatusFound)
	}))
	defer redirecting.Close()

	c, err := NewClient(redirecting.URL, "s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Analyze(context.Background(), "https://example.com"); err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if foreignHits != 1 {
		t.Fatalf("expected redirect to reach foreign origin once, got %d hits", foreignHits)
	}
	if standinSecret != "s3cret" {
		t.Errorf("ox-browser stand-in saw X-Internal-Secret %q, want %q", standinSecret, "s3cret")
	}
	if foreignSecret != "" {
		t.Errorf("foreign origin received X-Internal-Secret %q — credential leaked via redirect", foreignSecret)
	}
}

// Degradation contract: empty secret sends no header (soft-auth keeps working
// until the gate flips); the startup WARN is what makes that state visible.
func TestClientEmptySecretSendsNoHeader(t *testing.T) {
	t.Parallel()
	srv, rec := newRecordingOxBrowser(t)

	c, err := NewClient(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Analyze(context.Background(), "https://example.com"); err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got := rec.secretFor("/analyze"); got != "" {
		t.Errorf("X-Internal-Secret = %q, want absent for empty secret", got)
	}
}
