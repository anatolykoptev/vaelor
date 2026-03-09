package freshness

import (
	"context"
	"net/http"
	"testing"
	"time"
)

func TestRegistry_NotFound(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	defer srv.Close()

	reg := &NpmRegistry{BaseURL: srv.URL, Client: srv.Client()}
	_, err := reg.Latest(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for 404 response")
	}
}

func TestRegistry_BadJSON(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{not json`))
	})
	defer srv.Close()

	reg := &GoRegistry{BaseURL: srv.URL, Client: srv.Client()}
	_, err := reg.Latest(context.Background(), "example.com/mod")
	if err == nil {
		t.Error("expected error for bad JSON")
	}
}

func TestRegistry_Timeout(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte(`{"version":"1.0.0"}`))
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	reg := &NpmRegistry{BaseURL: srv.URL, Client: srv.Client()}
	_, err := reg.Latest(ctx, "pkg")
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestNuGetRegistry_EmptyVersions(t *testing.T) {
	srv := newTestServer(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"versions":[]}`))
	})
	defer srv.Close()

	reg := &NuGetRegistry{BaseURL: srv.URL, Client: srv.Client()}
	_, err := reg.Latest(context.Background(), "empty-pkg")
	if err == nil {
		t.Error("expected error for empty versions array")
	}
}

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertEqual(t *testing.T, want, got string) {
	t.Helper()
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
