package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/anatolykoptev/go-kit/jeff"
)

// The adapter must send ONE batched /v1/systemone request with a single
// choice question and map the per-option probabilities back to file
// paths — a mapping slip would silently penalize the wrong files in
// production.
func TestJeffScorer_ChoiceMapping(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/systemone" {
			http.NotFound(w, r)
			return
		}
		var req jeff.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		q, ok := req.Questions["pick"]
		if !ok || q.Type != "choice" {
			t.Errorf("expected one choice question 'pick', got %+v", req.Questions)
		}
		opts, _ := q.Criteria.(map[string]any)
		if len(opts) != 3 {
			t.Errorf("expected 3 choice options, got %d", len(opts))
		}
		state, _ := req.State.(string)
		if state == "" {
			t.Error("state must carry the query")
		}
		// c.go deliberately missing from probabilities — partial response.
		_ = json.NewEncoder(w).Encode(jeff.Response{Answers: map[string]jeff.Answer{
			"pick": {Type: "choice", Choice: "a.go", Probabilities: map[string]float64{
				"a.go": 0.6, "b.go": 0.1,
			}},
		}})
	}))
	defer srv.Close()

	c, err := jeff.NewClient(srv.URL, jeff.WithToken("t"))
	if err != nil {
		t.Fatal(err)
	}
	s := &jeffScorer{client: c}
	got, err := s.ScoreTopical(context.Background(), "consolidate dedup", []string{"a.go", "b.go", "c.go"})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 batched HTTP call, got %d", calls.Load())
	}
	if got["a.go"] != 0.6 || got["b.go"] != 0.1 {
		t.Fatalf("probabilities mapped to wrong files: %v", got)
	}
	if _, ok := got["c.go"]; ok {
		t.Fatalf("unscored option must be absent, got %v", got["c.go"])
	}
}

// Empty file list must not hit the network at all.
func TestJeffScorer_EmptyNoCall(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
	}))
	defer srv.Close()

	c, _ := jeff.NewClient(srv.URL, jeff.WithToken("t"))
	s := &jeffScorer{client: c}
	out, err := s.ScoreTopical(context.Background(), "q", nil)
	if err != nil || out != nil {
		t.Fatalf("empty input → nil,nil; got %v, %v", out, err)
	}
	if calls.Load() != 0 {
		t.Fatal("empty input must not make an HTTP call")
	}
}
