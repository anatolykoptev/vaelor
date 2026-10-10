package main

import (
	"net/http"
	"strings"
	"sync"
	"time"

	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// mcpSessionIdleTimeout is how long a Streamable-HTTP session may sit without
// an in-flight POST before the SDK reaps it. The SDK only pauses the idle timer
// while a POST is running: an open standalone GET stream does NOT keep a
// session alive, so any gap between tool calls longer than this kills a
// connected client (issue #925). 10 minutes was shorter than normal agent
// think time / a coffee break; 6 hours outlasts a working session while memory
// stays bounded (a session is a few KB and the timer still reaps abandoned
// ones).
const mcpSessionIdleTimeout = 6 * time.Hour

// maxTrackedMCPSessions bounds the session-id memory used to tell an expired
// session from one this process never issued.
const maxTrackedMCPSessions = 10000

const (
	mcpPathPrefix     = "/mcp"
	mcpSessionHeader  = "Mcp-Session-Id"
	sessionReasonIdle = "idle_ttl"   // we issued this id, the SDK reaped it (idle timer) without a DELETE
	sessionReasonUnk  = "unknown_id" // this process never issued the id (restart/redeploy, or garbage)
)

// mcpSessionExpiredTotal counts requests rejected with 404 "session not found"
// (the spec's signal for the client to re-initialise), by why the id is gone.
//
//   - reason: idle_ttl | unknown_id
//
// A burst of unknown_id right after a deploy is expected (in-memory session
// store dropped by the restart); a steady idle_ttl rate means the idle timeout
// is too short for how clients actually behave. Cardinality: 2 series.
var mcpSessionExpiredTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "vaelor_mcp_session_expired_total",
		Help: "MCP requests rejected with 404 session-not-found, by reason (idle_ttl, unknown_id).",
	},
	[]string{"reason"},
)

func init() {
	mcpSessionExpiredTotal.WithLabelValues(sessionReasonIdle).Add(0)
	mcpSessionExpiredTotal.WithLabelValues(sessionReasonUnk).Add(0)
}

// sessionGuard remembers the session ids this process handed out so a 404 can
// be attributed, and counts it. It never alters a response.
type sessionGuard struct {
	now  func() time.Time
	ttl  time.Duration
	mu   sync.Mutex
	seen map[string]time.Time // id -> last activity
}

func newSessionGuard(now func() time.Time, ttl time.Duration) *sessionGuard {
	return &sessionGuard{now: now, ttl: ttl, seen: make(map[string]time.Time)}
}

// applyMCPSessionConfig is the single place the session policy is set, shared
// by main and the tests so a regression here is observable.
func applyMCPSessionConfig(cfg *mcpserver.Config, g *sessionGuard) {
	cfg.SessionTimeout = g.ttl
	// Stateful (new(bool) == *false): the standalone GET SSE stream must work
	// for rmcp-based clients (issue #906); stateless answers GET with 405.
	cfg.Stateless = new(bool)
	cfg.Middleware = append(cfg.Middleware, g.Middleware)
}

// Middleware observes /mcp traffic: records ids, attributes 404s.
func (g *sessionGuard) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, mcpPathPrefix) {
			next.ServeHTTP(w, r)
			return
		}
		reqID := r.Header.Get(mcpSessionHeader)
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		g.observe(r.Method, reqID, rec.Header().Get(mcpSessionHeader), rec.status)
	})
}

func (g *sessionGuard) observe(method, reqID, respID string, status int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	switch {
	case status == http.StatusNotFound && reqID != "" && (method == http.MethodGet || method == http.MethodPost):
		reason := sessionReasonUnk
		if _, ok := g.seen[reqID]; ok {
			reason = sessionReasonIdle
			delete(g.seen, reqID)
		}
		mcpSessionExpiredTotal.WithLabelValues(reason).Inc()
	case method == http.MethodDelete && status < http.StatusBadRequest:
		delete(g.seen, reqID)
	case status < http.StatusBadRequest && respID != "":
		g.touch(respID)
	case status < http.StatusBadRequest && reqID != "":
		g.touch(reqID)
	}
}

// touch records activity for id, evicting stale entries once the map is full.
// Caller holds g.mu.
func (g *sessionGuard) touch(id string) {
	now := g.now()
	if _, ok := g.seen[id]; !ok && len(g.seen) >= maxTrackedMCPSessions {
		for k, t := range g.seen {
			if now.Sub(t) > g.ttl {
				delete(g.seen, k)
			}
		}
		for k := range g.seen { // still full: drop arbitrary entries
			if len(g.seen) < maxTrackedMCPSessions {
				break
			}
			delete(g.seen, k)
		}
	}
	g.seen[id] = now
}

// statusRecorder captures the status code while staying transparent to SSE:
// Flush is forwarded and Unwrap lets http.ResponseController reach the
// underlying writer.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wroteHeader {
		s.status = code
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	s.wroteHeader = true
	return s.ResponseWriter.Write(b)
}

func (s *statusRecorder) Flush() {
	s.wroteHeader = true
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }
