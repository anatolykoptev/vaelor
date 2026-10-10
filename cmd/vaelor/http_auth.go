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
	"path"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// HTTP bearer auth for /mcp and the /api REST bridge, rolled out in three
// stages so merging changes nothing:
//
//	off     (default) middleware not installed
//	report  every request is let through but counted in gocode_http_auth_total
//	enforce requests without a valid token get 401
//
// go-mcpserver's BearerAuth is not reused on purpose: it is enforce-only (no
// report stage), takes a single verifier, and rejects a missing header before
// the verifier runs, so "missing" could neither be counted nor let through.
//
// Env (VAELOR_ prefix wins, GO_CODE_ accepted — see getenvRebrand):
//
//	HTTP_AUTH_MODE         off | report | enforce
//	HTTP_AUTH_TOKENS_FILE  path to a file with one token per line (blank lines
//	                       and lines starting with '#' are ignored). Several
//	                       tokens may be live at once so rotation is: add the
//	                       new line, roll clients, drop the old line. The file
//	                       is re-read when it changes (checked every
//	                       httpAuthReloadEvery), no restart needed.

const (
	httpAuthModeOff     = "off"
	httpAuthModeReport  = "report"
	httpAuthModeEnforce = "enforce"

	httpAuthRouteMCP = "mcp"
	httpAuthRouteAPI = "api"

	httpAuthPathMCP = "/mcp"
	httpAuthPathAPI = "/api"
	labelRoute      = "route"
	labelResult     = "result"

	httpAuthResultOK      = "ok"
	httpAuthResultMissing = "missing"
	httpAuthResultInvalid = "invalid"

	httpAuthReloadEvery = 10 * time.Second
	bearerScheme        = "bearer"
)

var httpAuthTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_http_auth_total",
		Help: "HTTP bearer-auth decisions on /mcp and /api by route and result (ok, missing, invalid). Counted in report and enforce modes.",
	},
	[]string{labelRoute, labelResult},
)

func init() {
	// Create every series up front so `result!="ok"` queries and alerts see
	// an explicit 0 instead of an absent series.
	for _, route := range []string{httpAuthRouteMCP, httpAuthRouteAPI} {
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
	modTime  time.Time
	size     int64
	lastStat time.Time
}

func newTokenSet(file string) (*tokenSet, error) {
	ts := &tokenSet{file: file}
	if err := ts.load(); err != nil {
		return nil, err
	}
	return ts, nil
}

// parseTokens reads one token per line; blank lines and '#' comments are skipped.
func parseTokens(data []byte) [][sha256.Size]byte {
	var out [][sha256.Size]byte
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, sha256.Sum256([]byte(line)))
	}
	return out
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
	digests := parseTokens(data)
	if len(digests) == 0 {
		return errors.New("http auth: tokens file holds no tokens")
	}
	ts.mu.Lock()
	ts.digests, ts.modTime, ts.size, ts.lastStat = digests, fi.ModTime(), fi.Size(), time.Now()
	ts.mu.Unlock()
	return nil
}

// refresh re-reads the file when its mtime/size changed. A failed or empty
// reload keeps the previous set: a half-written rotation must not lock
// everyone out.
func (ts *tokenSet) refresh() {
	ts.mu.Lock()
	if time.Since(ts.lastStat) < httpAuthReloadEvery {
		ts.mu.Unlock()
		return
	}
	ts.lastStat = time.Now()
	prevMod, prevSize := ts.modTime, ts.size
	ts.mu.Unlock()

	fi, err := os.Stat(ts.file)
	if err != nil {
		slog.Warn("http auth: tokens file stat failed; keeping previous tokens")
		return
	}
	if fi.ModTime().Equal(prevMod) && fi.Size() == prevSize {
		return
	}
	if err := ts.load(); err != nil {
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

// httpAuth is the configured gate. A nil *httpAuth means mode=off.
type httpAuth struct {
	mode   string
	tokens *tokenSet
}

// newHTTPAuth builds the gate from raw env values. mode "" or "off" returns
// (nil, nil). Any other non-enforce/report value, or a missing/empty tokens
// file in report/enforce, is a startup error: a security knob that silently
// degrades to "open" is worse than a refused boot.
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
	return &httpAuth{mode: mode, tokens: ts}, nil
}

func newHTTPAuthFromEnv() (*httpAuth, error) {
	return newHTTPAuth(getenvRebrand("HTTP_AUTH_MODE"), getenvRebrand("HTTP_AUTH_TOKENS_FILE"))
}

// httpAuthRoute maps a request path to its metric route, or "" when the path
// is not gated (health, readiness, /metrics, /resolve, /webhook/github keep
// their own access rules). path.Clean makes the match a superset of what the
// mux will route, so "/./mcp" cannot slip past.
func httpAuthRoute(p string) string {
	c := path.Clean(p)
	switch {
	case c == httpAuthPathMCP || strings.HasPrefix(c, httpAuthPathMCP+"/"):
		return httpAuthRouteMCP
	case c == httpAuthPathAPI || strings.HasPrefix(c, httpAuthPathAPI+"/"):
		return httpAuthRouteAPI
	}
	return ""
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

// Middleware returns the gate as a go-mcpserver Middleware. A nil receiver
// (mode=off) is a pass-through.
func (a *httpAuth) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if a == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route := httpAuthRoute(r.URL.Path)
			if route == "" {
				next.ServeHTTP(w, r)
				return
			}
			result := a.classify(r)
			httpAuthTotal.WithLabelValues(route, result).Inc()
			if result != httpAuthResultOK && a.mode == httpAuthModeEnforce {
				slog.Warn("http auth: rejected", slog.String("route", route), slog.String("result", result))
				w.Header().Set("WWW-Authenticate", `Bearer realm="vaelor"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
