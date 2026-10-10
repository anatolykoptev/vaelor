package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
)

const (
	testInitBody  = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`
	testListBody  = `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	testShortTTL  = 300 * time.Millisecond
	testAfterTTL  = 700 * time.Millisecond
	testSessionID = "never-issued-by-this-process"
)

// newSessionTestServer builds the MCP handler from the SAME session policy
// main uses (applyMCPSessionConfig), with only the TTL shortened.
func newSessionTestServer(t *testing.T, ttl time.Duration) *httptest.Server {
	t.Helper()
	srv := mcpserver.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, mcpserver.Config{})
	cfg := mcpserver.Config{Name: "t", Version: "0", DisableLocalhostProtection: true}
	applyMCPSessionConfig(&cfg, newSessionGuard(time.Now, ttl))
	h, err := mcpserver.Build(srv, cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts
}

func mcpDo(t *testing.T, ts *httptest.Server, method, sid, body string) (*http.Response, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, method, ts.URL+"/mcp", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if sid != "" {
		req.Header.Set("Mcp-Session-Id", sid)
		req.Header.Set("MCP-Protocol-Version", "2025-06-18")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp, string(b)
}

func initSession(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	resp, _ := mcpDo(t, ts, http.MethodPost, "", testInitBody)
	sid := resp.Header.Get("Mcp-Session-Id")
	if resp.StatusCode != http.StatusOK || sid == "" {
		t.Fatalf("initialize: status=%d sid=%q", resp.StatusCode, sid)
	}
	return sid
}

// expiredCounter reads the exposed text line for one reason from the default
// registry with an anchored match (not a substring).
func expiredCounter(t *testing.T, reason string) string {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for _, mf := range mfs {
		if mf.GetName() == "vaelor_mcp_session_expired_total" {
			if _, err := expfmt.MetricFamilyToText(&sb, mf); err != nil {
				t.Fatal(err)
			}
		}
	}
	re := regexp.MustCompile(`(?m)^vaelor_mcp_session_expired_total\{reason="` + reason + `"\} (\S+)$`)
	m := re.FindStringSubmatch(sb.String())
	if m == nil {
		t.Fatalf("no exposed line for reason=%q in:\n%s", reason, sb.String())
	}
	return m[1]
}

func counterDelta(t *testing.T, reason string, fn func()) float64 {
	t.Helper()
	parse := func() float64 {
		var f float64
		if _, err := fmt.Sscan(expiredCounter(t, reason), &f); err != nil {
			t.Fatal(err)
		}
		return f
	}
	before := parse()
	fn()
	return parse() - before
}

func TestMCPSessionIdleTimeoutOutlastsAgentThinkTime(t *testing.T) {
	if mcpSessionIdleTimeout < time.Hour {
		t.Fatalf("mcpSessionIdleTimeout = %v; must outlast normal agent think time (>= 1h), 10m caused #925", mcpSessionIdleTimeout)
	}
}

func TestMCPLiveSessionSurvivesIdleWithinTTL(t *testing.T) {
	ts := newSessionTestServer(t, time.Hour)
	sid := initSession(t, ts)
	time.Sleep(testAfterTTL) // past the short TTL used by the expiry test below
	resp, _ := mcpDo(t, ts, http.MethodPost, sid, testListBody)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("idle session within TTL: status=%d, want 200", resp.StatusCode)
	}
}

func TestMCPUnknownSessionGets404AndCountsUnknownID(t *testing.T) {
	ts := newSessionTestServer(t, time.Hour)
	d := counterDelta(t, "unknown_id", func() {
		resp, body := mcpDo(t, ts, http.MethodPost, testSessionID, testListBody)
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("unknown session id: status=%d body=%q, want 404 (spec: client must re-initialise)", resp.StatusCode, body)
		}
	})
	if d != 1 {
		t.Fatalf("unknown_id delta = %v, want 1", d)
	}
}

func TestMCPIdleExpiredSessionGets404AndCountsIdleTTL(t *testing.T) {
	ts := newSessionTestServer(t, testShortTTL)
	sid := initSession(t, ts)
	if resp, _ := mcpDo(t, ts, http.MethodPost, sid, testListBody); resp.StatusCode != http.StatusOK {
		t.Fatalf("fresh session: status=%d", resp.StatusCode)
	}
	time.Sleep(testAfterTTL)
	idle := counterDelta(t, "idle_ttl", func() {
		unk := counterDelta(t, "unknown_id", func() {
			resp, _ := mcpDo(t, ts, http.MethodGet, sid, "")
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("expired session GET: status=%d, want 404", resp.StatusCode)
			}
		})
		if unk != 0 {
			t.Fatalf("expired issued session counted as unknown_id (%v)", unk)
		}
	})
	if idle != 1 {
		t.Fatalf("idle_ttl delta = %v, want 1", idle)
	}
}

func TestMCPDeleteForgetsSessionSoLaterReuseIsUnknown(t *testing.T) {
	ts := newSessionTestServer(t, time.Hour)
	sid := initSession(t, ts)
	if resp, _ := mcpDo(t, ts, http.MethodDelete, sid, ""); resp.StatusCode >= http.StatusBadRequest {
		t.Fatalf("DELETE status=%d", resp.StatusCode)
	}
	d := counterDelta(t, "unknown_id", func() {
		mcpDo(t, ts, http.MethodPost, sid, testListBody)
	})
	if d != 1 {
		t.Fatalf("explicitly closed session reuse: unknown_id delta = %v, want 1", d)
	}
}

func TestSessionGuardEvictsWhenFull(t *testing.T) {
	var mu sync.Mutex
	now := time.Unix(0, 0)
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	g := newSessionGuard(clock, time.Hour)
	for i := range maxTrackedMCPSessions {
		g.observe(http.MethodPost, "", "s"+strconv.Itoa(i), http.StatusOK)
	}
	mu.Lock()
	now = now.Add(2 * time.Hour)
	mu.Unlock()
	g.observe(http.MethodPost, "", "fresh", http.StatusOK)
	g.mu.Lock()
	n := len(g.seen)
	g.mu.Unlock()
	if n != 1 {
		t.Fatalf("tracked sessions after eviction = %d, want 1 (stale entries purged)", n)
	}
}

type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (f *flushRecorder) Flush() { f.flushed = true }

func TestStatusRecorderForwardsFlush(t *testing.T) {
	under := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	var w http.ResponseWriter = &statusRecorder{ResponseWriter: under, status: http.StatusOK}
	f, ok := w.(http.Flusher)
	if !ok {
		t.Fatal("statusRecorder must implement http.Flusher or SSE responses buffer")
	}
	f.Flush()
	if !under.flushed {
		t.Fatal("Flush not forwarded to the underlying writer")
	}
}
