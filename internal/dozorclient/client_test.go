package dozorclient_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/dozorclient"
)

func makeServer(t *testing.T, status int, body any, checkFn func(r *http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if checkFn != nil {
			checkFn(r)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if body != nil {
			_ = json.NewEncoder(w).Encode(body)
		}
	}))
}

func TestGetLogs_HappyPath(t *testing.T) {
	t.Parallel()
	want := dozorclient.LogsResponse{
		Service:     "acme-web",
		ContainerID: "abc123",
		Lines: []dozorclient.LogLine{
			{Ts: "2026-05-08T10:00:00Z", Level: "ERROR", Msg: "connection refused", Raw: `{"level":"ERROR","msg":"connection refused"}`},
		},
		Truncated: false,
	}
	srv := makeServer(t, http.StatusOK, want, nil)
	defer srv.Close()

	c := dozorclient.NewClient(srv.URL, "", 5*time.Second)
	got, err := c.GetLogs(context.Background(), "acme-web", time.Time{}, time.Time{}, "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Service != want.Service {
		t.Errorf("service: got %q want %q", got.Service, want.Service)
	}
	if len(got.Lines) != 1 {
		t.Fatalf("lines count: got %d want 1", len(got.Lines))
	}
	if got.Lines[0].Level != "ERROR" {
		t.Errorf("level: got %q want ERROR", got.Lines[0].Level)
	}
}

func TestGetLogs_AuthHeader(t *testing.T) {
	t.Parallel()
	var gotAuth string
	srv := makeServer(t, http.StatusOK, dozorclient.LogsResponse{Service: "svc", Lines: nil}, func(r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
	})
	defer srv.Close()

	c := dozorclient.NewClient(srv.URL, "mysecrettoken", 5*time.Second)
	_, err := c.GetLogs(context.Background(), "svc", time.Time{}, time.Time{}, "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer mysecrettoken" {
		t.Errorf("auth header: got %q want %q", gotAuth, "Bearer mysecrettoken")
	}
}

func TestGetLogs_NoAuthWhenNoToken(t *testing.T) {
	t.Parallel()
	var gotAuth string
	srv := makeServer(t, http.StatusOK, dozorclient.LogsResponse{Service: "svc", Lines: nil}, func(r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
	})
	defer srv.Close()

	c := dozorclient.NewClient(srv.URL, "", 5*time.Second)
	_, err := c.GetLogs(context.Background(), "svc", time.Time{}, time.Time{}, "", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("expected no auth header, got %q", gotAuth)
	}
}

func TestGetLogs_502Error(t *testing.T) {
	t.Parallel()
	srv := makeServer(t, http.StatusBadGateway, map[string]string{"error": "docker unreachable"}, nil)
	defer srv.Close()

	c := dozorclient.NewClient(srv.URL, "", 5*time.Second)
	_, err := c.GetLogs(context.Background(), "svc", time.Time{}, time.Time{}, "", 0)
	if err == nil {
		t.Fatal("expected error for 502, got nil")
	}
}

func TestGetLogs_BadJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{invalid json"))
	}))
	defer srv.Close()

	c := dozorclient.NewClient(srv.URL, "", 5*time.Second)
	_, err := c.GetLogs(context.Background(), "svc", time.Time{}, time.Time{}, "", 0)
	if err == nil {
		t.Fatal("expected error for bad JSON, got nil")
	}
}

func TestGetLogs_NilClient(t *testing.T) {
	t.Parallel()
	var c *dozorclient.Client
	_, err := c.GetLogs(context.Background(), "svc", time.Time{}, time.Time{}, "", 0)
	if err == nil {
		t.Fatal("expected error for nil client, got nil")
	}
}
