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

// The adapter must send ONE batched /v1/systemone request and map answer
// IDs f0..fN back to the file at the same index — a mapping slip would
// silently penalize the wrong files in production.
func TestJeffScorer_BatchedMapping(t *testing.T) {
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
		if len(req.Questions) != 3 {
			t.Errorf("expected 3 batched questions, got %d", len(req.Questions))
		}
		state, _ := req.State.(string)
		if state == "" || !json.Valid([]byte(`"`+state+`"`)) {
			t.Errorf("state must carry the query, got %v", req.State)
		}
		// f2 deliberately unanswered — partial response.
		_ = json.NewEncoder(w).Encode(jeff.Response{Answers: map[string]jeff.Answer{
			"f0": {Type: "noul", Noul: 0.9},
			"f1": {Type: "noul", Noul: 0.1},
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
	if got["a.go"] != 0.9 || got["b.go"] != 0.1 {
		t.Fatalf("answer IDs mapped to wrong files: %v", got)
	}
	if _, ok := got["c.go"]; ok {
		t.Fatalf("unanswered f2 must be absent, got %v", got["c.go"])
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
