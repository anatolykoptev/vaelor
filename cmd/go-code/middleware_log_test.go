package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	mcpserver "github.com/anatolykoptev/go-mcpserver"
)

// testHandler captures slog records for level assertions.
type testHandler struct {
	records []slog.Record
}

func (h *testHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *testHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}
func (h *testHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *testHandler) WithGroup(_ string) slog.Handler      { return h }

// applyRequestLog wraps a trivial handler with RequestLog middleware and
// returns the slog records captured after a single request.
func applyRequestLog(t *testing.T, path string) []slog.Record {
	t.Helper()
	th := &testHandler{}
	logger := slog.New(th)
	mw := mcpserver.RequestLogWithSkip(logger, []string{"/health", "/health/live", "/health/ready", "/metrics"})
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := mw(inner)

	req := httptest.NewRequest(http.MethodGet, path, nil)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)
	return th.records
}

// TestRequestLog_HealthAtDebug verifies that /health* requests are logged at
// Debug level so they do not appear under the default Info threshold.
func TestRequestLog_HealthAtDebug(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/health", "/health/live", "/health/ready"} {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			records := applyRequestLog(t, path)
			if len(records) != 1 {
				t.Fatalf("want 1 log record, got %d", len(records))
			}
			if records[0].Level != slog.LevelDebug {
				t.Errorf("path %s: log level = %v, want Debug", path, records[0].Level)
			}
		})
	}
}

// TestRequestLog_MetricsAtDebug verifies that /metrics scrapes are logged at
// Debug level (Prometheus scrapes every 15-30s — same noise problem as health).
func TestRequestLog_MetricsAtDebug(t *testing.T) {
	t.Parallel()
	records := applyRequestLog(t, "/metrics")
	if len(records) != 1 {
		t.Fatalf("want 1 log record, got %d", len(records))
	}
	if records[0].Level != slog.LevelDebug {
		t.Errorf("/metrics: log level = %v, want Debug", records[0].Level)
	}
}

// TestRequestLog_OtherAtInfo verifies that real traffic paths keep Info level.
func TestRequestLog_OtherAtInfo(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/mcp", "/api/tools/foo", "/"} {
		path := path
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			records := applyRequestLog(t, path)
			if len(records) != 1 {
				t.Fatalf("want 1 log record, got %d", len(records))
			}
			if records[0].Level != slog.LevelInfo {
				t.Errorf("path %s: log level = %v, want Info", path, records[0].Level)
			}
		})
	}
}
