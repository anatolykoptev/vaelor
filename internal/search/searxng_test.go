package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchSearXNG(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/search" {
				http.NotFound(w, r)
				return
			}
			if got := r.URL.Query().Get("q"); got != "golang context" {
				t.Errorf("unexpected query param q=%q", got)
			}
			if got := r.URL.Query().Get("format"); got != "json" {
				t.Errorf("unexpected format param: %q", got)
			}

			payload := searxngResponse{
				Results: []searxngResult{
					{Title: "Go context docs", URL: "https://pkg.go.dev/context", Content: "Package context...", Score: 0.95},
					{Title: "Blog post", URL: "https://example.com/go-context", Content: "Using context in Go...", Score: 0.7},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(payload)
		}))
		defer srv.Close()

		client := NewSearXNGClient(srv.URL)
		results, err := client.Search(context.Background(), "golang context", SearchOpts{})
		if err != nil {
			t.Fatalf("Search returned error: %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("expected 2 results, got %d", len(results))
		}
		if results[0].Title != "Go context docs" {
			t.Errorf("unexpected first title: %q", results[0].Title)
		}
		if results[0].Score != 0.95 {
			t.Errorf("unexpected first score: %v", results[0].Score)
		}
	})

	t.Run("opts forwarded", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if got := q.Get("language"); got != "ru" {
				t.Errorf("language param: want ru, got %q", got)
			}
			if got := q.Get("time_range"); got != "month" {
				t.Errorf("time_range param: want month, got %q", got)
			}
			if got := q.Get("engines"); got != "google,bing" {
				t.Errorf("engines param: want google,bing, got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(searxngResponse{})
		}))
		defer srv.Close()

		client := NewSearXNGClient(srv.URL)
		_, err := client.Search(context.Background(), "test", SearchOpts{
			Language:  "ru",
			TimeRange: "month",
			Engines:   "google,bing",
		})
		if err != nil {
			t.Fatalf("Search returned error: %v", err)
		}
	})

	t.Run("language=all not forwarded", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.URL.Query().Get("language"); got != "" {
				t.Errorf("language param should be absent when 'all', got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(searxngResponse{})
		}))
		defer srv.Close()

		client := NewSearXNGClient(srv.URL)
		_, err := client.Search(context.Background(), "test", SearchOpts{Language: "all"})
		if err != nil {
			t.Fatalf("Search returned error: %v", err)
		}
	})

	t.Run("non-200 status", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
		}))
		defer srv.Close()

		client := NewSearXNGClient(srv.URL)
		_, err := client.Search(context.Background(), "test", SearchOpts{})
		if err == nil {
			t.Fatal("expected error for non-200 status, got nil")
		}
	})

	t.Run("x-forwarded-for header", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("X-Forwarded-For"); got != "127.0.0.1" {
				t.Errorf("X-Forwarded-For: want 127.0.0.1, got %q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(searxngResponse{})
		}))
		defer srv.Close()

		client := NewSearXNGClient(srv.URL)
		_, err := client.Search(context.Background(), "test", SearchOpts{})
		if err != nil {
			t.Fatalf("Search returned error: %v", err)
		}
	})
}

func TestFilterByScore(t *testing.T) {
	results := []Result{
		{Title: "A", Score: 0.9},
		{Title: "B", Score: 0.1},
		{Title: "C", Score: 0.5},
	}

	t.Run("filters below threshold", func(t *testing.T) {
		filtered := FilterByScore(results, 0.5, 1)
		if len(filtered) != 2 {
			t.Fatalf("expected 2 results (A and C), got %d", len(filtered))
		}
		if filtered[0].Title != "A" || filtered[1].Title != "C" {
			t.Errorf("unexpected titles: %v", titlesOf(filtered))
		}
	})

	t.Run("falls back to minKeep when too few pass filter", func(t *testing.T) {
		filtered := FilterByScore(results, 0.95, 2)
		// 0 pass the threshold, fallback to first 2 of original
		if len(filtered) != 2 {
			t.Fatalf("expected 2 results from fallback, got %d", len(filtered))
		}
		if filtered[0].Title != "A" || filtered[1].Title != "B" {
			t.Errorf("unexpected titles: %v", titlesOf(filtered))
		}
	})

	t.Run("returns all original when original shorter than minKeep", func(t *testing.T) {
		short := []Result{{Title: "X", Score: 0.1}}
		filtered := FilterByScore(short, 0.9, 5)
		if len(filtered) != 1 {
			t.Fatalf("expected 1 result, got %d", len(filtered))
		}
	})

	t.Run("empty input", func(t *testing.T) {
		filtered := FilterByScore(nil, 0.5, 3)
		if len(filtered) != 0 {
			t.Fatalf("expected 0 results, got %d", len(filtered))
		}
	})

	t.Run("all pass filter", func(t *testing.T) {
		filtered := FilterByScore(results, 0.0, 1)
		if len(filtered) != 3 {
			t.Fatalf("expected 3 results, got %d", len(filtered))
		}
	})
}

func TestDedupByDomain(t *testing.T) {
	results := []Result{
		{URL: "https://example.com/a"},
		{URL: "https://example.com/b"},
		{URL: "https://example.com/c"},
		{URL: "https://other.com/a"},
	}

	t.Run("limits to maxPerDomain", func(t *testing.T) {
		deduped := DedupByDomain(results, 2)
		if len(deduped) != 3 {
			t.Fatalf("expected 3 results (2 from example.com + 1 from other.com), got %d", len(deduped))
		}
		if deduped[0].URL != "https://example.com/a" {
			t.Errorf("unexpected first URL: %q", deduped[0].URL)
		}
		if deduped[1].URL != "https://example.com/b" {
			t.Errorf("unexpected second URL: %q", deduped[1].URL)
		}
		if deduped[2].URL != "https://other.com/a" {
			t.Errorf("unexpected third URL: %q", deduped[2].URL)
		}
	})

	t.Run("maxPerDomain=1 keeps one per domain", func(t *testing.T) {
		deduped := DedupByDomain(results, 1)
		if len(deduped) != 2 {
			t.Fatalf("expected 2 results, got %d", len(deduped))
		}
	})

	t.Run("unparseable URL always included", func(t *testing.T) {
		bad := []Result{
			{URL: "://bad-url"},
			{URL: "://bad-url"},
		}
		deduped := DedupByDomain(bad, 1)
		if len(deduped) != 2 {
			t.Fatalf("expected both unparseable URLs included, got %d", len(deduped))
		}
	})

	t.Run("empty input", func(t *testing.T) {
		deduped := DedupByDomain(nil, 2)
		if len(deduped) != 0 {
			t.Fatalf("expected 0 results, got %d", len(deduped))
		}
	})
}

// titlesOf is a test helper to extract titles for cleaner error messages.
func titlesOf(results []Result) []string {
	titles := make([]string, len(results))
	for i, r := range results {
		titles[i] = r.Title
	}
	return titles
}
