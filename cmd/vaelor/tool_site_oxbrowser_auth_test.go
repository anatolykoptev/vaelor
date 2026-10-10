package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/webanalyze"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// oxBrowserStandIn is an httptest ox-browser that records X-Internal-Secret
// per request path and answers the three endpoints the client calls.
type oxBrowserStandIn struct {
	mu      sync.Mutex
	secrets map[string]string // path -> X-Internal-Secret ("" = absent)
}

func newOxBrowserStandIn(t *testing.T) (*httptest.Server, *oxBrowserStandIn) {
	t.Helper()
	rec := &oxBrowserStandIn{secrets: make(map[string]string)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.secrets[r.URL.Path] = r.Header.Get("X-Internal-Secret")
		rec.mu.Unlock()
		switch r.URL.Path {
		case "/analyze":
			json.NewEncoder(w).Encode(webanalyze.AnalyzeResponse{URL: "https://example.com", Status: 200})
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

func (s *oxBrowserStandIn) secretFor(path string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.secrets[path]
}

// callSiteTool connects an in-memory MCP client to server and invokes one
// tool, returning its result. Named for the site_* tools; mcp_scrub_test's
// callTool is the error-result helper.
func callSiteTool(t *testing.T, server *mcp.Server, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	ctx := context.Background()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	go func() {
		_, _ = server.Connect(ctx, serverTransport, nil)
	}()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("CallTool %s: %v", name, err)
	}
	return res
}

// T1+T2 (ox-browser#173): the production wiring —
// registerSiteAnalyze → Config{OxBrowserURL, InternalServiceSecret} →
// webanalyze.NewClient → svcauth transport — must deliver
// X-Internal-Secret to ox-browser on the real request.
// Mutation gate: drop the secret at the NewClient call site, or build the
// client without the svcauth wrap → this goes RED.
func TestSiteAnalyze_OxBrowserAuth_EndToEnd(t *testing.T) {
	standin, rec := newOxBrowserStandIn(t)

	server := newTestServer(t)
	registerSiteAnalyze(server, Config{
		OxBrowserURL:          standin.URL,
		InternalServiceSecret: "s3cret",
		WorkspaceDir:          t.TempDir(),
	})

	res := callSiteTool(t, server, "site_analyze", map[string]any{"url": "https://example.com"})
	if res.IsError {
		t.Fatalf("site_analyze returned error: %s", textContentOf(t, res))
	}
	if got := rec.secretFor("/analyze"); got != "s3cret" {
		t.Errorf("ox-browser saw X-Internal-Secret %q, want %q — cfg.InternalServiceSecret is not reaching the request", got, "s3cret")
	}
}

// Same gate for the SSE transport: site_crawl uses the timeout-less client.
func TestSiteCrawl_OxBrowserAuth_EndToEnd(t *testing.T) {
	standin, rec := newOxBrowserStandIn(t)

	server := newTestServer(t)
	registerSiteCrawl(server, Config{
		OxBrowserURL:          standin.URL,
		InternalServiceSecret: "s3cret",
		OutputDir:             t.TempDir(),
	})

	res := callSiteTool(t, server, "site_crawl", map[string]any{"url": "https://example.com"})
	if res.IsError {
		t.Fatalf("site_crawl returned error: %s", textContentOf(t, res))
	}
	if got := rec.secretFor("/crawl"); got != "s3cret" {
		t.Errorf("ox-browser saw X-Internal-Secret %q, want %q — cfg.InternalServiceSecret is not reaching the request", got, "s3cret")
	}
}

// Degradation: no secret configured → tools still work, no header sent (the
// startup WARN is what surfaces this state; covered in config_warnings_test).
func TestSiteAnalyze_NoSecret_NoHeader_EndToEnd(t *testing.T) {
	standin, rec := newOxBrowserStandIn(t)

	server := newTestServer(t)
	registerSiteAnalyze(server, Config{
		OxBrowserURL: standin.URL,
		WorkspaceDir: t.TempDir(),
	})

	res := callSiteTool(t, server, "site_analyze", map[string]any{"url": "https://example.com"})
	if res.IsError {
		t.Fatalf("site_analyze returned error: %s", textContentOf(t, res))
	}
	if got := rec.secretFor("/analyze"); got != "" {
		t.Errorf("X-Internal-Secret %q sent with empty secret — should be absent", got)
	}
}
