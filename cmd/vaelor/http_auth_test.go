package main

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

const (
	tokA = "tok-AAAA-0123456789abcdef"
	tokB = "tok-BBBB-fedcba9876543210"
)

func newReq(method, target string, body io.Reader) *http.Request {
	return httptest.NewRequestWithContext(context.Background(), method, target, body)
}

func writeTokens(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "tokens")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func newGate(t *testing.T, mode string) *httpAuth {
	t.Helper()
	g, err := newHTTPAuth(mode, writeTokens(t, "# comment\n"+tokA+"\n\n"+tokB+"\n"))
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
}

func do(h http.Handler, path, authz string) int {
	req := newReq(http.MethodPost, path, nil)
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func counter(route, result string) float64 {
	return testutil.ToFloat64(httpAuthTotal.WithLabelValues(route, result))
}

func TestHTTPAuth_Modes(t *testing.T) {
	cases := []struct {
		mode, name, authz string
		want              int
		result            string
	}{
		{"report", "missing", "", 200, "missing"},
		{"report", "invalid", "Bearer nope", 200, "invalid"},
		{"report", "valid", "Bearer " + tokA, 200, "ok"},
		{"enforce", "missing", "", 401, "missing"},
		{"enforce", "invalid", "Bearer nope", 401, "invalid"},
		{"enforce", "wrong scheme", "Basic " + tokA, 401, "missing"},
		{"enforce", "valid first token", "Bearer " + tokA, 200, "ok"},
		{"enforce", "valid second token", "bearer " + tokB, 200, "ok"},
		{"enforce", "token prefix", "Bearer " + tokA[:len(tokA)-1], 401, "invalid"},
		{"enforce", "token plus suffix", "Bearer " + tokA + "x", 401, "invalid"},
	}
	for _, c := range cases {
		t.Run(c.mode+"/"+c.name, func(t *testing.T) {
			h := newGate(t, c.mode).Middleware()(okHandler())
			before := counter("mcp", c.result)
			if got := do(h, "/mcp", c.authz); got != c.want {
				t.Fatalf("status = %d, want %d", got, c.want)
			}
			if d := counter("mcp", c.result) - before; d != 1 {
				t.Fatalf("gocode_http_auth_total{route=mcp,result=%s} delta = %v, want 1", c.result, d)
			}
		})
	}
}

func TestHTTPAuth_OffIsPassThrough(t *testing.T) {
	g, err := newHTTPAuth("", "")
	if err != nil || g != nil {
		t.Fatalf("mode off: got (%v, %v), want (nil, nil)", g, err)
	}
	before := counter("mcp", "missing")
	if got := do(g.Middleware()(okHandler()), "/mcp", ""); got != 200 {
		t.Fatalf("off mode status = %d, want 200", got)
	}
	if counter("mcp", "missing") != before {
		t.Fatal("off mode must not count")
	}
}

func TestHTTPAuth_RouteMetricLabels(t *testing.T) {
	h := newGate(t, "report").Middleware()(okHandler())
	for _, route := range []string{"mcp", "api"} {
		before := counter(route, "invalid")
		do(h, "/"+route+"/x", "Bearer nope")
		if d := counter(route, "invalid") - before; d != 1 {
			t.Fatalf("route=%s invalid delta = %v, want 1", route, d)
		}
	}
}

func TestHTTPAuthRoute(t *testing.T) {
	cases := map[string]string{
		"/mcp": "mcp", "/mcp/": "mcp", "/mcp/sub": "mcp", "/./mcp": "mcp", "/x/../mcp": "mcp",
		"/api": "api", "/api/tools": "api", "/api//tools": "api",
		"/health": "", "/health/live": "", "/health/ready": "", "/metrics": "",
		"/apix": "", "/mcpx": "", "/resolve": "", "/webhook/github": "", "/": "",
	}
	for p, want := range cases {
		if got := httpAuthRoute(p); got != want {
			t.Errorf("httpAuthRoute(%q) = %q, want %q", p, got, want)
		}
	}
}

// TestHTTPAuth_RealStack drives the real go-mcpserver handler (MCP + REST
// bridge + health) so route coverage is proven against the actual mux, not a
// stub handler.
func TestHTTPAuth_RealStack(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	srv := mcpserver.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, mcpserver.Config{})
	g := newGate(t, "enforce")
	h, err := mcpserver.Build(srv, mcpserver.Config{
		Name: "t", Version: "0", Context: ctx, RESTBridge: true,
		Middleware: []mcpserver.Middleware{g.Middleware()},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"/mcp", "/api/tools"} {
		if got := do(h, p, ""); got != http.StatusUnauthorized {
			t.Errorf("%s without token: status %d, want 401", p, got)
		}
		if got := do(h, p, "Bearer "+tokA); got == http.StatusUnauthorized {
			t.Errorf("%s with valid token: got 401", p)
		}
	}
	for _, p := range []string{"/health", "/health/live", "/health/ready"} {
		req := newReq(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusUnauthorized {
			t.Errorf("%s must stay open, got 401", p)
		}
	}
}

func TestHTTPAuth_ReloadAddsAndDropsTokens(t *testing.T) {
	p := writeTokens(t, tokA+"\n")
	g, err := newHTTPAuth("enforce", p)
	if err != nil {
		t.Fatal(err)
	}
	h := g.Middleware()(okHandler())
	// Rotation step 1: both tokens live.
	if err := os.WriteFile(p, []byte(tokA+"\n"+tokB+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	g.tokens.lastStat = time.Now().Add(-time.Hour)
	if do(h, "/mcp", "Bearer "+tokB) != 200 || do(h, "/mcp", "Bearer "+tokA) != 200 {
		t.Fatal("both tokens must be accepted after reload")
	}
	// Step 2: old token dropped.
	if err := os.WriteFile(p, []byte(tokB+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	g.tokens.lastStat = time.Now().Add(-time.Hour)
	if do(h, "/mcp", "Bearer "+tokA) != 401 || do(h, "/mcp", "Bearer "+tokB) != 200 {
		t.Fatal("old token must be rejected after it is dropped")
	}
	// A broken (empty) file keeps the previous set.
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	g.tokens.lastStat = time.Now().Add(-time.Hour)
	if do(h, "/mcp", "Bearer "+tokB) != 200 {
		t.Fatal("empty reload must keep previous tokens")
	}
}

func TestNewHTTPAuth_ConfigErrors(t *testing.T) {
	good := writeTokens(t, tokA+"\n")
	empty := writeTokens(t, "\n# only a comment\n")
	cases := []struct{ name, mode, file string }{
		{"unknown mode", "enforced", good},
		{"report without file", "report", ""},
		{"enforce without file", "enforce", ""},
		{"missing file", "enforce", filepath.Join(t.TempDir(), "absent")},
		{"empty file", "report", empty},
	}
	for _, c := range cases {
		if g, err := newHTTPAuth(c.mode, c.file); err == nil || g != nil {
			t.Errorf("%s: got (%v, %v), want error", c.name, g, err)
		}
	}
}

// TestHTTPAuth_UsesConstantTimeCompare pins the comparison primitive: the
// source must call subtle.ConstantTimeCompare and must not compare token
// strings with == / bytes.Equal.
func TestHTTPAuth_UsesConstantTimeCompare(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "http_auth.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var calls int
	ast.Inspect(f, func(n ast.Node) bool {
		pkg, fn := selectorCall(n)
		switch {
		case pkg == "subtle" && fn == "ConstantTimeCompare":
			calls++
		case pkg == "bytes" && fn == "Equal":
			t.Error("bytes.Equal is not constant time")
		}
		return true
	})
	if calls == 0 {
		t.Fatal("http_auth.go must compare tokens with subtle.ConstantTimeCompare")
	}
}

type captureHandler struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.buf.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool { h.buf.WriteString(" " + a.Key + "=" + a.Value.String()); return true })
	h.buf.WriteString("\n")
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

func TestHTTPAuth_TokenNeverLogged(t *testing.T) {
	ch := &captureHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(ch))
	t.Cleanup(func() { slog.SetDefault(prev) })

	secret := "SECRET-presented-token-9f8e7d"
	g := newGate(t, "enforce")
	h := g.Middleware()(okHandler())
	req := newReq(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// Force a reload (logs "reloaded") and a failing reload (logs reason).
	g.tokens.lastStat = time.Now().Add(-time.Hour)
	_ = os.WriteFile(g.tokens.file, []byte(tokA+"\n"), 0o600)
	do(h, "/mcp", "Bearer "+tokA)
	g.tokens.lastStat = time.Now().Add(-time.Hour)
	_ = os.WriteFile(g.tokens.file, nil, 0o600)
	do(h, "/mcp", "Bearer "+tokA)

	logs := ch.buf.String()
	if !strings.Contains(logs, "rejected") {
		t.Fatalf("expected a rejection log line, got %q", logs)
	}
	for _, s := range []string{secret, tokA, tokB} {
		if strings.Contains(logs, s) || strings.Contains(rec.Body.String(), s) {
			t.Fatalf("token %q leaked into logs/response", s)
		}
	}
}

// selectorCall returns (pkg, func) for a call of the form pkg.Func(...).
func selectorCall(n ast.Node) (string, string) {
	c, ok := n.(*ast.CallExpr)
	if !ok {
		return "", ""
	}
	sel, ok := c.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", ""
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return "", ""
	}
	return id.Name, sel.Sel.Name
}
