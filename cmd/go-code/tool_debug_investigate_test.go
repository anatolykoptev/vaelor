package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anatolykoptev/go-code/internal/promclient"
)

func TestListLabelValues_ReturnsNames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/label/__name__/values" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data":   []string{"go_gc_duration_seconds", "process_cpu_seconds_total", "up"},
		})
	}))
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 0)
	vals, err := listLabelValues(context.Background(), prom, "__name__")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vals) != 3 {
		t.Errorf("got %d values, want 3", len(vals))
	}
}

func TestListLabelValues_HTTP404ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 0)
	_, err := listLabelValues(context.Background(), prom, "__name__")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

func TestListLabelValues_RejectsInvalidLabel(t *testing.T) {
	cases := []string{"../etc", "label/values", "name space", ""}
	for _, c := range cases {
		if _, err := listLabelValues(context.Background(), nil, c); err == nil {
			t.Errorf("expected error for %q", c)
		}
	}
}

func TestListLabelValues_TruncatesAt200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := make([]string, 250)
		for i := range data {
			data[i] = "metric"
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": data})
	}))
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 0)
	vals, err := listLabelValues(context.Background(), prom, "__name__")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vals) != 200 {
		t.Errorf("got %d values, want 200", len(vals))
	}
}
