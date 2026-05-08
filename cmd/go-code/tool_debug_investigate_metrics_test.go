package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

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

func TestDiscoverFailureMetrics_FiltersByPattern(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/v1/label/__name__/values") {
			fmt.Fprint(w, `{"status":"success","data":["http_requests_total","signaling_call_outcome_total","ws_handshake_failed_total","sfu_chat_relay_dropped_total","go_goroutines","process_cpu_seconds_total"]}`)
			return
		}
		http.Error(w, "not found", 404)
	}))
	defer srv.Close()

	prom := promclient.NewClient(srv.URL, 5*time.Second)
	got, err := discoverFailureMetrics(context.Background(), prom, "any-service")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := []string{"signaling_call_outcome_total", "ws_handshake_failed_total", "sfu_chat_relay_dropped_total"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRankSpikes_TopK(t *testing.T) {
	got := rankSpikes([]MetricSpike{
		{MetricName: "a", Score: 0.6},
		{MetricName: "b", Score: 0.95},
		{MetricName: "c", Score: 0.3},
		{MetricName: "d", Score: 0.8},
	}, 2)
	if len(got) != 2 || got[0].MetricName != "b" || got[1].MetricName != "d" {
		t.Errorf("unexpected: %+v", got)
	}
}
