package httputil_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/httputil"
)

// helpers

type payload struct {
	Msg string `json:"msg"`
}

func okHandler(resp any) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func statusHandler(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "server error body", code)
	}
}

// TestGetJSON_Happy verifies a successful GET decodes the response into dst.
func TestGetJSON_Happy(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(okHandler(payload{Msg: "hello"}))
	defer srv.Close()

	c := httputil.New(srv.URL)
	var got payload
	if err := c.GetJSON(context.Background(), "/", &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Msg != "hello" {
		t.Errorf("got %q, want hello", got.Msg)
	}
}

// TestPostJSON_Happy verifies a successful POST encodes body and decodes response.
func TestPostJSON_Happy(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "wrong method", http.StatusMethodNotAllowed)
			return
		}
		var in payload
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, "bad body", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(payload{Msg: "got:" + in.Msg})
	}))
	defer srv.Close()

	c := httputil.New(srv.URL)
	var got payload
	if err := c.PostJSON(context.Background(), "/", payload{Msg: "ping"}, &got); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Msg != "got:ping" {
		t.Errorf("got %q, want got:ping", got.Msg)
	}
}

// TestGetJSON_4xx verifies a 4xx response returns an error.
func TestGetJSON_4xx(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(statusHandler(http.StatusNotFound))
	defer srv.Close()

	c := httputil.New(srv.URL)
	if err := c.GetJSON(context.Background(), "/", nil); err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

// TestGetJSON_5xx verifies a 5xx response returns an error including status code.
func TestGetJSON_5xx(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(statusHandler(http.StatusInternalServerError))
	defer srv.Close()

	c := httputil.New(srv.URL)
	err := c.GetJSON(context.Background(), "/", nil)
	if err == nil {
		t.Fatal("expected error for 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should contain status code 500, got: %v", err)
	}
}

// TestGetJSON_MalformedJSON verifies malformed JSON body returns decode error.
func TestGetJSON_MalformedJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{not json`))
	}))
	defer srv.Close()

	c := httputil.New(srv.URL)
	var got payload
	if err := c.GetJSON(context.Background(), "/", &got); err == nil {
		t.Fatal("expected decode error, got nil")
	}
}

// TestWithHeader verifies WithHeader option propagates headers in requests.
func TestWithHeader(t *testing.T) {
	t.Parallel()
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Test-Key")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`null`))
	}))
	defer srv.Close()

	c := httputil.New(srv.URL, httputil.WithHeader("X-Test-Key", "test-value"))
	if err := c.GetJSON(context.Background(), "/", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotHeader != "test-value" {
		t.Errorf("X-Test-Key: got %q, want test-value", gotHeader)
	}
}

// TestWithTimeout verifies WithTimeout option is applied. Uses a done channel
// so the handler exits cleanly when the client disconnects, allowing
// httptest.Server.Close to complete without blocking.
func TestWithTimeout(t *testing.T) {
	t.Parallel()
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(done)
		srv.Close()
	})

	c := httputil.New(srv.URL, httputil.WithTimeout(5*time.Millisecond))
	err := c.GetJSON(context.Background(), "/", nil)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

// TestContextCancel verifies context cancellation is respected. Uses a done
// channel so the handler exits when the client disconnects.
func TestContextCancel(t *testing.T) {
	t.Parallel()
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() {
		close(done)
		srv.Close()
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	c := httputil.New(srv.URL)
	err := c.GetJSON(ctx, "/", nil)
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
}

// TestNew_TrailingSlash verifies base URL trailing slash is stripped.
func TestNew_TrailingSlash(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`null`))
	}))
	defer srv.Close()

	// Pass URL with trailing slash; path with leading slash.
	c := httputil.New(srv.URL+"/", httputil.WithTimeout(5*time.Second))
	_ = c.GetJSON(context.Background(), "/api/v1/test", nil)
	if gotPath != "/api/v1/test" {
		t.Errorf("path: got %q, want /api/v1/test", gotPath)
	}
}

// TestNewWithHTTPClient verifies reusing an existing http.Client.
func TestNewWithHTTPClient(t *testing.T) {
	t.Parallel()
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Custom")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`null`))
	}))
	defer srv.Close()

	hc := &http.Client{Timeout: 5 * time.Second}
	c := httputil.NewWithHTTPClient(srv.URL, hc, httputil.WithHeader("X-Custom", "reused"))
	if err := c.GetJSON(context.Background(), "/", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotHeader != "reused" {
		t.Errorf("X-Custom: got %q, want reused", gotHeader)
	}
}
