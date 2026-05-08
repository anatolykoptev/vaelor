package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anatolykoptev/go-code/internal/promclient"
)

func TestMaxSampleValue_EmptyResponse(t *testing.T) {
	if got := maxSampleValue(nil); got != 0 {
		t.Errorf("nil resp: got %v", got)
	}
	if got := maxSampleValue(&promclient.QueryRangeResponse{}); got != 0 {
		t.Errorf("empty resp: got %v", got)
	}
}

func TestMaxSampleValue_PicksMaxAcrossSeries(t *testing.T) {
	resp := &promclient.QueryRangeResponse{}
	resp.Data.Result = []promclient.SeriesResult{
		{Values: [][2]any{{float64(0), "1.5"}, {float64(60), "3.0"}}},
		{Values: [][2]any{{float64(0), "2.0"}, {float64(60), "10.5"}}},
	}
	if got := maxSampleValue(resp); got != 10.5 {
		t.Errorf("got %v, want 10.5", got)
	}
}

func TestMaxSampleValue_IgnoresUnparseable(t *testing.T) {
	resp := &promclient.QueryRangeResponse{}
	resp.Data.Result = []promclient.SeriesResult{
		{Values: [][2]any{{float64(0), "not-a-number"}, {float64(60), "5.0"}}},
	}
	if got := maxSampleValue(resp); got != 5.0 {
		t.Errorf("got %v, want 5.0", got)
	}
}

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
