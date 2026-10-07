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
// research.JeffScorer: ONE batched Ask carrying a single choice question
// whose options are the candidate file paths. The softmax over options
// forces separation — live probes showed noul compresses to ~0.4-0.5
// regardless of topicality (even inverting), while choice spreads
// topical files at 0.3-0.45 and infra at ~0.04-0.09. Latency ≈0.8s at
// 10 candidates edge→kisol. The caller's ctx is the real bound (research
// caps at 5s); the client's timeout is transport headroom for callers
// that forget a deadline.
type jeffScorer struct {
	client *jeff.Client
}

// newJeffScorer wires the scorer from JEFF_URL/JEFF_TOKEN — the same env
// pair go-search/go-wowa/quarryn use for the shared kisol edge
// (https://jeff.krolik.tools), and the same raw-read convention as
// EMBED_URL for shared services. Returns nil when JEFF_URL is unset so
// the research pipeline keeps its byte-identical cold path.
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
	options := make(map[string]any, len(files))
	for _, f := range files {
		options[f] = fmt.Sprintf("candidate seed file %s", f)
	}
	resp, err := s.client.Ask(ctx, jeff.Request{
		State: "Code research seed ranking. Query: " + strings.TrimSpace(query),
		Questions: map[string]jeff.Question{
			"pick": jeff.ChoiceQuestion(
				"Which file most likely contains the primary implementation of the query topic (not generic shared infrastructure like config, caches, wiring, utils, test helpers)?",
				options),
		},
	})
	if err != nil {
		return nil, err
	}
	ans, ok := resp.Answers["pick"]
	if !ok {
		return nil, fmt.Errorf("jeff: response missing pick answer")
	}
	out := make(map[string]float64, len(files))
	for _, f := range files {
		if p, ok := ans.Probabilities[f]; ok {
			out[f] = p
		}
	}
	return out, nil
}
