package main

import (
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
