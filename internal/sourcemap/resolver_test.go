package sourcemap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const sampleMap = `{"version":3,"sources":["src/foo.svelte"],"names":["handleClick"],"mappings":"AAAA,SAASA,WAAW","file":"chunk-abc.js"}`

func TestResolver_Resolve(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_app/immutable/chunks/chunk-abc.js.map" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(sampleMap))
			return
		}
		http.Error(w, "not found", 404)
	}))
	defer srv.Close()

	r := NewResolver(http.DefaultClient, 10, 5*time.Minute, 0)
	got, err := r.Resolve(context.Background(), srv.URL+"/_app/immutable/chunks/chunk-abc.js", 1, 9)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.File != "src/foo.svelte" || got.Function != "handleClick" {
		t.Errorf("unexpected: %+v", got)
	}
}

func TestResolver_CacheHit(t *testing.T) {
	t.Parallel()
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(sampleMap))
	}))
	defer srv.Close()

	r := NewResolver(http.DefaultClient, 10, 5*time.Minute, 0)
	for i := 0; i < 3; i++ {
		_, _ = r.Resolve(context.Background(), srv.URL+"/x.js", 1, 9)
	}
	if hits != 1 {
		t.Errorf("expected 1 fetch (cached), got %d", hits)
	}
}

func TestResolver_MaxBodyBytes(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleMap))
	}))
	defer srv.Close()

	// Cap smaller than sampleMap so the body is truncated and parse fails.
	r := NewResolver(http.DefaultClient, 10, 5*time.Minute, 64)
	_, err := r.Resolve(context.Background(), srv.URL+"/chunk-abc.js", 1, 9)
	if err == nil {
		t.Fatalf("expected error when body exceeds maxBodyBytes, got nil")
	}
}

func TestIsAllowedURL(t *testing.T) {
	t.Parallel()
	allowed := []string{"cdn.example.com", "static.app.io"}
	tests := []struct {
		url  string
		want bool
	}{
		{"https://cdn.example.com/chunk.js", true},
		{"http://cdn.example.com/chunk.js", true},
		{"https://static.app.io/js/app.js", true},
		{"https://evil.com/cdn.example.com/x.js", false},
		{"https://cdn.example.com.evil.io/x.js", false},
		{"http://localhost/chunk.js", false},
		{"", false},
	}
	for _, tc := range tests {
		got := IsAllowedURL(tc.url, allowed)
		if got != tc.want {
			t.Errorf("IsAllowedURL(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}
