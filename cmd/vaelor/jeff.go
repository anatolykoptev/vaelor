package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/anatolykoptev/go-kit/jeff"
	"github.com/anatolykoptev/vaelor/internal/research"
)

// jeffScorer adapts the jeff System One service (gliformer) to
// research.JeffScorer: one batched Ask with a noul question per candidate
// file. One HTTP call for all files — latency grows ~linearly with label
// count (≈0.24s/question edge→kisol). The caller's ctx is the real bound
// (research caps at 5s); the client's timeout is transport headroom for
// callers that forget a deadline.
type jeffScorer struct {
	client *jeff.Client
}

// newJeffScorer wires the scorer from JEFF_URL/JEFF_TOKEN — the same env
// pair go-search uses for the shared kisol edge (https://jeff.krolik.tools),
// and the same raw-read convention as EMBED_URL for shared services.
// Returns nil when JEFF_URL is unset so the research pipeline keeps its
// byte-identical cold path.
func newJeffScorer() research.JeffScorer {
	url := os.Getenv("JEFF_URL")
	if url == "" {
		return nil
	}
	c, err := jeff.NewClient(url,
		jeff.WithModel(envOr("JEFF_MODEL", "gliformer-large-v1")),
		jeff.WithTimeout(5*time.Second),
	)
	if err != nil {
		slog.Warn("jeff scorer: disabled", "error", err)
		return nil
	}
	return &jeffScorer{client: c}
}

// researchJeffScorer memoizes the scorer for the process — constructing a
// Client per request would allocate a fresh http.Client each call.
var researchJeffScorer = sync.OnceValue(newJeffScorer)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func (s *jeffScorer) ScoreTopical(ctx context.Context, query string, files []string) (map[string]float64, error) {
	if len(files) == 0 {
		return nil, nil
	}
	questions := make(map[string]jeff.Question, len(files))
	for i, f := range files {
		questions[fmt.Sprintf("f%d", i)] = jeff.NoulQuestion(
			fmt.Sprintf("File %q contains code implementing the query topic, not generic shared infrastructure (config, caches, wiring, utils, test helpers)", f))
	}
	resp, err := s.client.Ask(ctx, jeff.Request{
		State:     "Code research seed ranking. Query: " + strings.TrimSpace(query),
		Questions: questions,
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]float64, len(files))
	for i, f := range files {
		if ans, ok := resp.Answers[fmt.Sprintf("f%d", i)]; ok {
			out[f] = ans.Noul
		}
	}
	return out, nil
}
