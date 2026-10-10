package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// HTTP bearer auth for the MCP listener, rolled out in three stages so merging
// changes nothing:
//
//	off     (default) middleware not installed
//	report  every request is let through but counted in gocode_http_auth_total
//	enforce requests without a valid token get 401
//
// DENY BY DEFAULT: every request on the listener is gated except an exact
// allowlist of paths (httpAuthExempt) matched on r.URL.EscapedPath(). That is
// the string net/http's ServeMux routes on, so the gate and the mux cannot
// disagree about which handler a request reaches. Matching on the decoded or
// path.Clean()ed path does not work: "/mcp/%2E%2E" decodes to "/" for the gate
// but the mux still routes it into the /mcp/ subtree.
//
// go-mcpserver's BearerAuth is not reused on purpose: it is enforce-only (no
// report stage), takes a single verifier, and rejects a missing header before
// the verifier runs, so "missing" could neither be counted nor let through.
//
// Env (VAELOR_ prefix wins, GO_CODE_ accepted — see getenvRebrand):
//
//	HTTP_AUTH_MODE         off | report | enforce
//	HTTP_AUTH_TOKENS_FILE  path to a file with one token per line (blank lines
//	                       and lines starting with '#' are ignored; a UTF-8 BOM
//	                       is stripped). Every token must be at least
//	                       httpAuthMinTokenLen bytes or the whole load fails.
//
// Rotation: add the new token line, roll clients, drop the old line. The file
// is re-read when its identity (inode), mtime or size changes, checked every
// httpAuthReloadEvery. A failed or EMPTY reload keeps the previous set and bumps
// gocode_http_auth_reload_failures_total, so emptying the file does NOT revoke
// anything. Emergency revoke: replace the file with the new token set and
// recreate the container (a restart loads the file fresh and refuses to boot
// on a bad one). When the file is bind-mounted into a container, mount its
// DIRECTORY: a single-file bind mount pins the inode, so a rename-style edit
// (sed -i, an editor, mv) is invisible inside the container.
//
// Middleware order: Recovery, RequestID, RequestLog and CORS (go-mcpserver)
// wrap the gate, which is harmless; the gate runs before this repo's tracing
// and session middleware and before every handler.

const (
	httpAuthModeOff     = "off"
	httpAuthModeReport  = "report"
	httpAuthModeEnforce = "enforce"

	httpAuthRouteMCP   = "mcp"
	httpAuthRouteAPI   = "api"
	httpAuthRouteOther = "other"

	httpAuthResultOK      = "ok"
	httpAuthResultMissing = "missing"
	httpAuthResultInvalid = "invalid"

	pathHealth      = "/health"
	pathHealthLive  = "/health/live"
	pathHealthReady = "/health/ready"
	pathMetrics     = "/metrics"
	httpAuthPathMCP = "/mcp"
	httpAuthPathAPI = "/api"
	labelRoute      = "route"
	labelResult     = "result"

	httpAuthMinTokenLen = 32
	httpAuthReloadEvery = 10 * time.Second
	httpAuthLogEvery    = time.Minute
	httpAuthLogMaxKeys  = 256
	httpAuthMaxUALen    = 200
	bearerScheme        = "bearer"
	utf8BOM             = "\xef\xbb\xbf"
)

// httpAuthExempt lists the only paths that bypass the gate, compared with
// ==, against r.URL.EscapedPath(). These are exactly the non-/mcp, non-/api
// routes the server registers: go-mcpserver health probes and /metrics, plus
// vaelor's own /resolve (host allowlist + rate limit) and /webhook/github
// (HMAC). Anything not listed here, including unknown paths, is gated.
var httpAuthExempt = map[string]struct{}{
	pathHealth:        {},
	pathHealthLive:    {},
	pathHealthReady:   {},
	pathMetrics:       {},
	"/resolve":        {},
	"/webhook/github": {},
}

var (
	httpAuthTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gocode_http_auth_total",
			Help: "HTTP bearer-auth decisions by route (mcp, api, other) and result (ok, missing, invalid). Counted in report and enforce modes; exempt health/resolve/webhook paths are not counted.",
		},
		[]string{labelRoute, labelResult},
	)
	httpAuthTokensLoaded = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gocode_http_auth_tokens_loaded",
		Help: "Number of bearer tokens currently accepted (0 when HTTP auth is off).",
	})
	httpAuthReloadLastSuccess = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "gocode_http_auth_reload_last_success_timestamp_seconds",
		Help: "Unix time of the last successful tokens-file load (initial load included).",
	})
	httpAuthReloadFailures = promauto.NewCounter(prometheus.CounterOpts{
		Name: "gocode_http_auth_reload_failures_total",
		Help: "Tokens-file reloads that failed (stat/read error, empty file, short or oversized token); the previous token set stayed active.",
	})
)

func init() {
	// Create every series up front so `result!="ok"` queries and alerts see
	// an explicit 0 instead of an absent series.
	for _, route := range []string{httpAuthRouteMCP, httpAuthRouteAPI, httpAuthRouteOther} {
		for _, result := range []string{httpAuthResultOK, httpAuthResultMissing, httpAuthResultInvalid} {
			httpAuthTotal.WithLabelValues(route, result)
		}
	}
}

// tokenSet holds the accepted tokens as SHA-256 digests. Comparing fixed-size
// digests with subtle.ConstantTimeCompare hides both the token contents and
// their lengths from timing.
type tokenSet struct {
	file string

	mu       sync.Mutex
	digests  [][sha256.Size]byte
	info     os.FileInfo
	lastStat time.Time
}

func newTokenSet(file string) (*tokenSet, error) {
	ts := &tokenSet{file: file}
	if err := ts.load(); err != nil {
		return nil, err
	}
	return ts, nil
}

// parseTokens reads one token per line; blank lines and '#' comments are
// skipped. It fails if any token is shorter than httpAuthMinTokenLen or a line
// is too long to scan. Errors name the line number, never the content.
func parseTokens(data []byte) ([][sha256.Size]byte, error) {
	data = bytes.TrimPrefix(data, []byte(utf8BOM))
	var out [][sha256.Size]byte
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) < httpAuthMinTokenLen {
			return nil, fmt.Errorf("http auth: token on line %d is shorter than %d bytes", n, httpAuthMinTokenLen)
		}
		out = append(out, sha256.Sum256([]byte(line)))
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("http auth: scan tokens file: %w", err)
	}
	if len(out) == 0 {
		return nil, errors.New("http auth: tokens file holds no tokens")
	}
	return out, nil
}

func (ts *tokenSet) load() error {
	fi, err := os.Stat(ts.file)
	if err != nil {
		return fmt.Errorf("http auth: stat tokens file: %w", err)
	}
	data, err := os.ReadFile(ts.file)
	if err != nil {
		return fmt.Errorf("http auth: read tokens file: %w", err)
	}
	digests, err := parseTokens(data)
	if err != nil {
		return err
	}
	ts.mu.Lock()
	ts.digests, ts.info, ts.lastStat = digests, fi, time.Now()
	ts.mu.Unlock()
	httpAuthTokensLoaded.Set(float64(len(digests)))
	httpAuthReloadLastSuccess.SetToCurrentTime()
	return nil
}

// changed reports whether the file at ts.file differs from the loaded one by
// identity (inode), mtime or size, so a rename-over edit is noticed even when
// size and mtime happen to match. Caller holds ts.mu.
func (ts *tokenSet) changed(fi os.FileInfo) bool {
	return !os.SameFile(ts.info, fi) || !fi.ModTime().Equal(ts.info.ModTime()) || fi.Size() != ts.info.Size()
}

// refresh re-reads the file when it changed. A failed or empty reload keeps
// the previous set: a half-written rotation must not lock everyone out.
func (ts *tokenSet) refresh() {
	ts.mu.Lock()
	if time.Since(ts.lastStat) < httpAuthReloadEvery {
		ts.mu.Unlock()
		return
	}
	ts.lastStat = time.Now()
	ts.mu.Unlock()

	fi, err := os.Stat(ts.file)
	if err != nil {
		httpAuthReloadFailures.Inc()
		slog.Warn("http auth: tokens file stat failed; keeping previous tokens")
		return
	}
	ts.mu.Lock()
	unchanged := !ts.changed(fi)
	ts.mu.Unlock()
	if unchanged {
		return
	}
	if err := ts.load(); err != nil {
		httpAuthReloadFailures.Inc()
		slog.Warn("http auth: tokens reload failed; keeping previous tokens", slog.String("reason", err.Error()))
		return
	}
	slog.Info("http auth: tokens reloaded")
}

// match reports whether presented equals any accepted token. It always walks
// the full set so timing does not reveal which entry matched.
func (ts *tokenSet) match(presented string) bool {
	ts.refresh()
	got := sha256.Sum256([]byte(presented))
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ok := 0
	for i := range ts.digests {
		ok |= subtle.ConstantTimeCompare(got[:], ts.digests[i][:])
	}
	return ok == 1
}

// logLimiter allows one log line per key per interval and bounds its memory.
type logLimiter struct {
	mu   sync.Mutex
	last map[string]time.Time
	now  func() time.Time
}

func newLogLimiter() *logLimiter {
	return &logLimiter{last: make(map[string]time.Time), now: time.Now}
}

func (l *logLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if t, ok := l.last[key]; ok && now.Sub(t) < httpAuthLogEvery {
		return false
	}
	if len(l.last) >= httpAuthLogMaxKeys {
		clear(l.last)
	}
	l.last[key] = now
	return true
}

// httpAuth is the configured gate. A nil *httpAuth means mode=off.
type httpAuth struct {
	mode    string
	tokens  *tokenSet
	limiter *logLimiter
}

// newHTTPAuth builds the gate from raw env values. mode "" or "off" returns
// (nil, nil). Any other non-enforce/report value, or a missing/empty/invalid
// tokens file in report/enforce, is a startup error: a security knob that
// silently degrades to "open" is worse than a refused boot.
func newHTTPAuth(mode, tokensFile string) (*httpAuth, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", httpAuthModeOff:
		return nil, nil
	case httpAuthModeReport:
		mode = httpAuthModeReport
	case httpAuthModeEnforce:
		mode = httpAuthModeEnforce
	default:
		return nil, fmt.Errorf("http auth: unknown HTTP_AUTH_MODE %q (want off|report|enforce)", mode)
	}
	if strings.TrimSpace(tokensFile) == "" {
		return nil, errors.New("http auth: HTTP_AUTH_TOKENS_FILE is required when HTTP_AUTH_MODE is report or enforce")
	}
	ts, err := newTokenSet(tokensFile)
	if err != nil {
		return nil, err
	}
	return &httpAuth{mode: mode, tokens: ts, limiter: newLogLimiter()}, nil
}

func newHTTPAuthFromEnv() (*httpAuth, error) {
	return newHTTPAuth(getenvRebrand("HTTP_AUTH_MODE"), getenvRebrand("HTTP_AUTH_TOKENS_FILE"))
}

// httpAuthRoute classifies a request path (as returned by EscapedPath) into a
// fixed metric label set. Exempt paths return ("", true).
func httpAuthRoute(escapedPath string) (route string, exempt bool) {
	if _, ok := httpAuthExempt[escapedPath]; ok {
		return "", true
	}
	switch {
	case escapedPath == httpAuthPathMCP || strings.HasPrefix(escapedPath, httpAuthPathMCP+"/"):
		return httpAuthRouteMCP, false
	case escapedPath == httpAuthPathAPI || strings.HasPrefix(escapedPath, httpAuthPathAPI+"/"):
		return httpAuthRouteAPI, false
	}
	return httpAuthRouteOther, false
}

// bearerToken extracts the token from "Authorization: Bearer <token>".
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	scheme, tok, found := strings.Cut(h, " ")
	if !found || !strings.EqualFold(scheme, bearerScheme) {
		return ""
	}
	return strings.TrimSpace(tok)
}

func (a *httpAuth) classify(r *http.Request) string {
	tok := bearerToken(r)
	switch {
	case tok == "":
		return httpAuthResultMissing
	case a.tokens.match(tok):
		return httpAuthResultOK
	default:
		return httpAuthResultInvalid
	}
}

// logReport attributes a non-ok request in report mode, rate-limited per
// caller. It logs the peer address and User-Agent, never the Authorization
// header.
func (a *httpAuth) logReport(r *http.Request, route, result string) {
	ua := r.UserAgent()
	if len(ua) > httpAuthMaxUALen {
		ua = ua[:httpAuthMaxUALen]
	}
	if !a.limiter.allow(r.RemoteAddr + "|" + ua + "|" + route + "|" + result) {
		return
	}
	slog.Warn("http auth: request without a valid token (report mode)",
		slog.String("route", route), slog.String("result", result),
		slog.String("remote_addr", r.RemoteAddr), slog.String("user_agent", ua))
}

// Middleware returns the gate as a go-mcpserver Middleware. A nil receiver
// (mode=off) is a pass-through.
func (a *httpAuth) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if a == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route, exempt := httpAuthRoute(r.URL.EscapedPath())
			if exempt {
				next.ServeHTTP(w, r)
				return
			}
			result := a.classify(r)
			httpAuthTotal.WithLabelValues(route, result).Inc()
			if result != httpAuthResultOK {
				if a.mode == httpAuthModeEnforce {
					w.Header().Set("WWW-Authenticate", `Bearer realm="vaelor"`)
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				a.logReport(r, route, result)
			}
			next.ServeHTTP(w, r)
		})
	}
}
