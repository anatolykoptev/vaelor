package research

import (
	"context"
	"errors"
	"testing"
)

type stubJeffScorer struct {
	probs    map[string]float64
	err      error
	called   bool
	gotQ     string
	gotFiles []string
}

func (s *stubJeffScorer) ScoreTopical(_ context.Context, query string, files []string) (map[string]float64, error) {
	s.called = true
	s.gotQ = query
	s.gotFiles = files
	return s.probs, s.err
}

// Issue #834: a PageRank-boosted hub file (config.go, redis.go) whose only
// score is structural must drop below topical seeds — the penalty erases
// the artificial boost rather than the file itself.
func TestApplyJeffArbitration_DemotesInfra(t *testing.T) {
	seedScores := map[string]float64{
		"internal/config/config.go":   0.35, // hub — boost-only
		"internal/scheduler/reorg.go": 0.9,  // topical
		"internal/search/merge.go":    0.8,  // topical
		"internal/cache/redis.go":     0.33, // hub — boost-only
		"internal/util/misc.go":       0.6,  // topical
	}
	sc := &stubJeffScorer{probs: map[string]float64{
		"internal/config/config.go":   0.1,
		"internal/scheduler/reorg.go": 0.9,
		"internal/search/merge.go":    0.85,
		"internal/cache/redis.go":     0.15,
		"internal/util/misc.go":       0.7,
	}}
	applyJeffArbitration(context.Background(), Deps{JeffScorer: sc}, "consolidate dedup memories", seedScores)

	if !sc.called {
		t.Fatal("scorer was not called")
	}
	if got := seedScores["internal/config/config.go"]; got >= 0.35 {
		t.Fatalf("hub config.go must be penalized below its boost-only score, got %v", got)
	}
	if got := seedScores["internal/cache/redis.go"]; got >= 0.33 {
		t.Fatalf("hub redis.go must be penalized, got %v", got)
	}
	if got := seedScores["internal/scheduler/reorg.go"]; got != 0.9 {
		t.Fatalf("topical file must keep its score, got %v", got)
	}
}

// A scorer failure must leave the structural order untouched — jeff is
// best-effort, never a hard dependency.
func TestApplyJeffArbitration_ScorerErrorKeepsOrder(t *testing.T) {
	seedScores := map[string]float64{
		"a.go": 0.9, "b.go": 0.8, "c.go": 0.7, "d.go": 0.6,
	}
	sc := &stubJeffScorer{
		err:   errors.New("jeff down"),
		probs: map[string]float64{"a.go": 0.1, "b.go": 0.1, "c.go": 0.1, "d.go": 0.1},
	}
	applyJeffArbitration(context.Background(), Deps{JeffScorer: sc}, "q", seedScores)

	for f, want := range map[string]float64{"a.go": 0.9, "b.go": 0.8, "c.go": 0.7, "d.go": 0.6} {
		if seedScores[f] != want {
			t.Fatalf("%s changed on scorer error: %v", f, seedScores[f])
		}
	}
}

// Files missing from the scorer's answer are treated as unscored and keep
// their score — a partial response must not demote by accident.
func TestApplyJeffArbitration_MissingKeyKeepsScore(t *testing.T) {
	seedScores := map[string]float64{
		"a.go": 0.9, "b.go": 0.8, "c.go": 0.7, "d.go": 0.6,
	}
	sc := &stubJeffScorer{probs: map[string]float64{"a.go": 0.9}} // b/c/d unscored
	applyJeffArbitration(context.Background(), Deps{JeffScorer: sc}, "q", seedScores)
	if seedScores["b.go"] != 0.8 {
		t.Fatalf("unscored file must keep score, got %v", seedScores["b.go"])
	}
}

// The candidate set is capped at jeffTopFiles — removing the bound would be
// silent in prod (one fat request instead of an error), slowing every
// research call on the shared model host.
func TestApplyJeffArbitration_CapsCandidates(t *testing.T) {
	seedScores := make(map[string]float64, 30)
	for i := 0; i < 30; i++ {
		seedScores[string(rune('a'+i%26))+string(rune('a'+i/26))+".go"] = float64(30 - i)
	}
	sc := &stubJeffScorer{probs: map[string]float64{}}
	applyJeffArbitration(context.Background(), Deps{JeffScorer: sc}, "q", seedScores)
	if !sc.called {
		t.Fatal("scorer must run on 30 seeds")
	}
	if len(sc.gotFiles) > jeffTopFiles {
		t.Fatalf("candidate set unbounded: %d files", len(sc.gotFiles))
	}
	// And the candidates must be the highest-scored files.
	top := topSeedFiles(seedScores, jeffTopFiles)
	for _, f := range sc.gotFiles {
		found := false
		for _, tf := range top {
			if f == tf {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("non-top file sent to scorer: %s", f)
		}
	}
}

// Small seed sets skip arbitration entirely — nothing to arbitrate.
func TestApplyJeffArbitration_TooFewSeedsSkips(t *testing.T) {
	seedScores := map[string]float64{"a.go": 0.9, "b.go": 0.8}
	sc := &stubJeffScorer{}
	applyJeffArbitration(context.Background(), Deps{JeffScorer: sc}, "q", seedScores)
	if sc.called {
		t.Fatal("scorer must not run on <jeffMinSeeds seeds")
	}
}

// Nil scorer → pipeline identical to pre-jeff behavior.
func TestApplyJeffArbitration_NilScorerNoop(t *testing.T) {
	seedScores := map[string]float64{"a.go": 0.9}
	applyJeffArbitration(context.Background(), Deps{}, "q", seedScores)
	if seedScores["a.go"] != 0.9 {
		t.Fatal("nil scorer must be a no-op")
	}
}
