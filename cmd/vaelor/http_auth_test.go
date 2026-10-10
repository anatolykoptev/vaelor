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
	"sync/atomic"
	"testing"
	"time"

	kitmetrics "github.com/anatolykoptev/go-kit/metrics"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

const (
	tokA = "tok-AAAA-0123456789abcdef-0123456789"
	tokB = "tok-BBBB-fedcba9876543210-9876543210"

	probeBody = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"probe","arguments":{}}}`
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
	for _, route := range []string{"mcp", "api", "other"} {
		path := "/" + route + "/x"
		if route == "other" {
			path = "/somewhere/else"
		}
		before := counter(route, "invalid")
		do(h, path, "Bearer nope")
		if d := counter(route, "invalid") - before; d != 1 {
			t.Fatalf("route=%s invalid delta = %v, want 1", route, d)
		}
	}
}

func TestHTTPAuthRoute(t *testing.T) {
	type want struct {
		route  string
		exempt bool
	}
	cases := map[string]want{
		"/mcp": {"mcp", false}, "/mcp/": {"mcp", false}, "/mcp/sub": {"mcp", false},
		"/mcp/%2E%2E": {"mcp", false}, "/mcp/%2E%2E/health": {"mcp", false},
		"/api": {"api", false}, "/api/tools": {"api", false},
		"/health": {"", true}, "/health/live": {"", true}, "/health/ready": {"", true},
		"/metrics": {"", true}, "/resolve": {"", true}, "/webhook/github": {"", true},
		// Anything not in the exact allowlist is gated, never exempt.
		"/health/": {"other", false}, "/healthz": {"other", false}, "/": {"other", false},
		"/./health": {"other", false}, "/%68ealth": {"other", false}, "//health": {"other", false},
		"/apix": {"other", false}, "/mcpx": {"other", false}, "/resolve/x": {"other", false},
	}
	for p, w := range cases {
		route, exempt := httpAuthRoute(p)
		if route != w.route || exempt != w.exempt {
			t.Errorf("httpAuthRoute(%q) = (%q,%v), want (%q,%v)", p, route, exempt, w.route, w.exempt)
		}
	}
}

// realStack builds the production Config via buildMCPConfig (env-driven, so the
// real wiring is under test), registers a probe tool that counts executions,
// and returns the handler plus the execution counter.
func realStack(t *testing.T, mode, tokensFile string) (http.Handler, *atomic.Int64) {
	t.Helper()
	t.Setenv("GO_CODE_HTTP_AUTH_MODE", mode)
	t.Setenv("GO_CODE_HTTP_AUTH_TOKENS_FILE", tokensFile)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	srv := mcpserver.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, mcpserver.Config{})
	var ran atomic.Int64
	mcp.AddTool(srv, &mcp.Tool{Name: "probe"}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, any, error) {
		ran.Add(1)
		return &mcp.CallToolResult{}, nil, nil
	})
	cfg, err := buildMCPConfig(mcpConfigDeps{
		ctx: ctx, port: "0", reg: kitmetrics.NewPrometheusRegistry("httpauthtest" + strings.ReplaceAll(t.Name(), "/", "_")),
		routes: func(mux *http.ServeMux) {
			mux.HandleFunc("POST /resolve", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// argnorm rejects tool names absent from its registry (populated by the real
	// tool registration, not by this stand-in), and it is irrelevant to the HTTP
	// gate under test.
	cfg.MCPReceivingMiddleware = nil
	cfg.JSONResponse = true
	h, err := mcpserver.Build(srv, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return h, &ran
}

func postProbe(h http.Handler, path, authz string) int {
	req := newReq(http.MethodPost, path, strings.NewReader(probeBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

// bypassPaths are encoded dot-segment forms that net/http's ServeMux routes
// into the /mcp/ subtree (it matches on EscapedPath) while a gate that looks at
// the decoded or cleaned path sees something else.
var bypassPaths = []string{
	"/mcp/%2E%2E", "/mcp/x%2F..%2F..%2Fy", "/mcp/%2e%2e/",
	"/mcp/%2E%2E/health", "/mcp/x%2F..%2F..%2Fhealth",
}

func TestHTTPAuth_RealStack(t *testing.T) {
	tokens := writeTokens(t, tokA+"\n")
	h, ran := realStack(t, "enforce", tokens)

	// Harness sanity: a valid token really runs the tool, so "did not run"
	// below is not vacuous.
	if got := postProbe(h, "/mcp", "Bearer "+tokA); got != http.StatusOK || ran.Load() != 1 {
		t.Fatalf("valid token on /mcp: status %d, tool runs %d; want 200 and 1", got, ran.Load())
	}
	ran.Store(0)

	for _, p := range append([]string{"/mcp", "/api/tools", "/", "/unknown", "/health/", "/mcp/"}, bypassPaths...) {
		if got := postProbe(h, p, ""); got != http.StatusUnauthorized {
			t.Errorf("%s without token: status %d, want 401", p, got)
		}
		if got := postProbe(h, p, "Bearer wrong-wrong-wrong-wrong-wrong-wrong"); got != http.StatusUnauthorized {
			t.Errorf("%s with invalid token: status %d, want 401", p, got)
		}
	}
	if n := ran.Load(); n != 0 {
		t.Fatalf("tool ran %d time(s) for unauthenticated requests", n)
	}
	if got := postProbe(h, "/api/tools", "Bearer "+tokA); got == http.StatusUnauthorized {
		t.Error("/api/tools with valid token: got 401")
	}

	for _, p := range []string{"/health", "/health/live", "/health/ready"} {
		req := newReq(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s must stay open and healthy, got %d", p, rec.Code)
		}
	}
	if got := do(h, "/resolve", ""); got != http.StatusOK {
		t.Errorf("/resolve must stay open, got %d", got)
	}
}

func TestHTTPAuth_RealStack_ReportCountsBypassAttempts(t *testing.T) {
	h, _ := realStack(t, "report", writeTokens(t, tokA+"\n"))
	for _, p := range bypassPaths {
		before := counter("mcp", "missing")
		if got := postProbe(h, p, ""); got == http.StatusUnauthorized {
			t.Errorf("%s: report mode must not reject, got 401", p)
		}
		if d := counter("mcp", "missing") - before; d != 1 {
			t.Errorf("%s: gocode_http_auth_total{route=mcp,result=missing} delta = %v, want 1", p, d)
		}
	}
}

func TestBuildMCPConfig_BootRefusal(t *testing.T) {
	good := writeTokens(t, tokA+"\n")
	short := writeTokens(t, "short\n")
	cases := []struct{ name, mode, file string }{
		{"unknown mode", "enforced", good},
		{"enforce without file", "enforce", ""},
		{"report missing file", "report", filepath.Join(t.TempDir(), "absent")},
		{"enforce short token", "enforce", short},
	}
	for _, c := range cases {
		t.Setenv("GO_CODE_HTTP_AUTH_MODE", c.mode)
		t.Setenv("GO_CODE_HTTP_AUTH_TOKENS_FILE", c.file)
		if _, err := buildMCPConfig(mcpConfigDeps{ctx: context.Background(), reg: kitmetrics.NewPrometheusRegistry("bootrefusal" + strings.ReplaceAll(c.name, " ", "_"))}); err == nil {
			t.Errorf("%s: buildMCPConfig must refuse to boot", c.name)
		}
	}
	t.Setenv("GO_CODE_HTTP_AUTH_MODE", "")
	t.Setenv("GO_CODE_HTTP_AUTH_TOKENS_FILE", "")
	cfg, err := buildMCPConfig(mcpConfigDeps{ctx: context.Background(), reg: kitmetrics.NewPrometheusRegistry("bootrefusal_off")})
	if err != nil {
		t.Fatalf("off must boot: %v", err)
	}
	if len(cfg.Middleware) == 0 {
		t.Fatal("production middleware chain is empty")
	}
}

func TestHTTPAuth_ReloadAddsAndDropsTokens(t *testing.T) {
	p := writeTokens(t, tokA+"\n")
	g, err := newHTTPAuth("enforce", p)
	if err != nil {
		t.Fatal(err)
	}
	h := g.Middleware()(okHandler())
	expire := func() { g.tokens.lastStat = time.Now().Add(-time.Hour) }

	if err := os.WriteFile(p, []byte(tokA+"\n"+tokB+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	expire()
	if do(h, "/mcp", "Bearer "+tokB) != 200 || do(h, "/mcp", "Bearer "+tokA) != 200 {
		t.Fatal("both tokens must be accepted after reload")
	}
	if got := testutil.ToFloat64(httpAuthTokensLoaded); got != 2 {
		t.Fatalf("gocode_http_auth_tokens_loaded = %v, want 2", got)
	}
	if err := os.WriteFile(p, []byte(tokB+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	expire()
	if do(h, "/mcp", "Bearer "+tokA) != 401 || do(h, "/mcp", "Bearer "+tokB) != 200 {
		t.Fatal("old token must be rejected after it is dropped")
	}

	// Emptying the file does NOT revoke: previous set stays, failure counted.
	fails := testutil.ToFloat64(httpAuthReloadFailures)
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	expire()
	if do(h, "/mcp", "Bearer "+tokB) != 200 {
		t.Fatal("empty reload must keep previous tokens")
	}
	if d := testutil.ToFloat64(httpAuthReloadFailures) - fails; d != 1 {
		t.Fatalf("gocode_http_auth_reload_failures_total delta = %v, want 1", d)
	}
	if testutil.ToFloat64(httpAuthReloadLastSuccess) <= 0 {
		t.Fatal("last-success timestamp not set")
	}
}

// A rename-over edit (sed -i, editors, mv) swaps the inode; size and mtime may
// be identical, so identity must be part of the change check.
func TestHTTPAuth_ReloadSeesRenamedReplacement(t *testing.T) {
	p := writeTokens(t, tokA+"\n")
	g, err := newHTTPAuth("enforce", p)
	if err != nil {
		t.Fatal(err)
	}
	h := g.Middleware()(okHandler())
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	repl := p + ".new"
	if err := os.WriteFile(repl, []byte(tokB+"\n"), 0o600); err != nil { // same length as tokA
		t.Fatal(err)
	}
	if err := os.Chtimes(repl, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(repl, p); err != nil {
		t.Fatal(err)
	}
	g.tokens.lastStat = time.Now().Add(-time.Hour)
	if do(h, "/mcp", "Bearer "+tokA) != 401 || do(h, "/mcp", "Bearer "+tokB) != 200 {
		t.Fatal("renamed replacement with identical size+mtime must be picked up")
	}
}

func TestParseTokens(t *testing.T) {
	long := strings.Repeat("a", 64*1024+10)
	cases := []struct {
		name, in string
		want     int
		wantErr  bool
	}{
		{"ok with CRLF and comments", "# c\r\n" + tokA + "\r\n\r\n" + tokB + "\r\n", 2, false},
		{"BOM stripped", "\xef\xbb\xbf" + tokA + "\n", 1, false},
		{"short token fails whole load", tokA + "\nshort\n", 0, true},
		{"31 bytes fails", strings.Repeat("x", 31) + "\n", 0, true},
		{"32 bytes ok", strings.Repeat("x", 32) + "\n", 1, false},
		{"over-long line fails", long + "\n", 0, true},
		{"empty fails", "\n# only\n", 0, true},
	}
	for _, c := range cases {
		got, err := parseTokens([]byte(c.in))
		if (err != nil) != c.wantErr || len(got) != c.want {
			t.Errorf("%s: got %d tokens, err=%v; want %d, wantErr=%v", c.name, len(got), err, c.want, c.wantErr)
		}
		if err != nil && strings.Contains(err.Error(), "xxxxxxxx") {
			t.Errorf("%s: error leaks token content: %v", c.name, err)
		}
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
// strings with bytes.Equal.
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

func captureLogs(t *testing.T) *captureHandler {
	t.Helper()
	ch := &captureHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(ch))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return ch
}

func TestHTTPAuth_ReportLogsAreRateLimitedAndAttributed(t *testing.T) {
	ch := captureLogs(t)
	g := newGate(t, "report")
	h := g.Middleware()(okHandler())
	secret := "SECRET-presented-token-9f8e7d6c5b4a39281716"
	for range 5 {
		req := newReq(http.MethodPost, "/mcp", nil)
		req.RemoteAddr = "10.1.2.3:4444"
		req.Header.Set("User-Agent", "claude-code/9.9")
		req.Header.Set("Authorization", "Bearer "+secret)
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	logs := ch.buf.String()
	if n := strings.Count(logs, "request without a valid token"); n != 1 {
		t.Fatalf("want exactly 1 rate-limited report line for 5 identical callers, got %d:\n%s", n, logs)
	}
	for _, want := range []string{"10.1.2.3:4444", "claude-code/9.9", "result=invalid"} {
		if !strings.Contains(logs, want) {
			t.Errorf("report line lacks %q: %s", want, logs)
		}
	}
	if strings.Contains(logs, secret) {
		t.Fatal("presented token leaked into logs")
	}
}

func TestHTTPAuth_EnforceHasNoPerRequestWarnAndNeverLogsTokens(t *testing.T) {
	ch := captureLogs(t)
	g := newGate(t, "enforce")
	h := g.Middleware()(okHandler())
	secret := "SECRET-presented-token-9f8e7d6c5b4a39281716"
	req := newReq(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", rec.Code)
	}
	// Force a successful and a failing reload so those log paths run too.
	g.tokens.lastStat = time.Now().Add(-time.Hour)
	_ = os.WriteFile(g.tokens.file, []byte(tokA+"\n"), 0o600)
	do(h, "/mcp", "Bearer "+tokA)
	g.tokens.lastStat = time.Now().Add(-time.Hour)
	_ = os.WriteFile(g.tokens.file, nil, 0o600)
	do(h, "/mcp", "Bearer "+tokA)

	logs := ch.buf.String()
	if strings.Contains(logs, "rejected") {
		t.Fatalf("enforce must not log per rejected request: %s", logs)
	}
	for _, s := range []string{secret, tokA, tokB} {
		if strings.Contains(logs, s) || strings.Contains(rec.Body.String(), s) {
			t.Fatalf("token %q leaked into logs/response", s)
		}
	}
}
