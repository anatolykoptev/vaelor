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

// Issue #834: PageRank-boosted hub files (config.go, redis.go) whose
// score is structural must drop once jeff's choice distribution ranks
// them below the uniform share. Topical seeds keep their score.
func TestApplyJeffArbitration_DemotesInfra(t *testing.T) {
	seedScores := map[string]float64{
		"internal/config/config.go":   0.35, // hub — boost-only
		"internal/scheduler/reorg.go": 0.9,  // topical
		"internal/search/merge.go":    0.8,  // topical
		"internal/cache/redis.go":     0.33, // hub — boost-only
		"internal/util/misc.go":       0.6,  // topical
	}
	// Choice shares over 5 candidates (uniform = 0.2): topical files
	// absorb most of the mass; hubs sit far below uniform.
	sc := &stubJeffScorer{probs: map[string]float64{
		"internal/config/config.go":   0.05,
		"internal/scheduler/reorg.go": 0.4,
		"internal/search/merge.go":    0.35,
		"internal/cache/redis.go":     0.04,
		"internal/util/misc.go":       0.16,
	}}
	applyJeffArbitration(context.Background(), Deps{JeffScorer: sc}, "consolidate dedup memories", seedScores)

	if !sc.called {
		t.Fatal("scorer was not called")
	}
	// config.go: penalty = 0.35*(0.2-0.05)/0.2 = 0.2625 → 0.35-0.2625 ≈ 0.09
	if got := seedScores["internal/config/config.go"]; got >= 0.35 {
		t.Fatalf("hub config.go must be penalized below its boost-only score, got %v", got)
	}
	if got := seedScores["internal/cache/redis.go"]; got >= 0.33 {
		t.Fatalf("hub redis.go must be penalized, got %v", got)
	}
	if got := seedScores["internal/scheduler/reorg.go"]; got != 0.9 {
		t.Fatalf("topical file must keep its score, got %v", got)
	}
	// misc.go is mildly below uniform (0.16 < 0.2) → small penalty only:
	// 0.35*(0.04)/0.2 = 0.07 → 0.53. Still a seed, still above the hubs.
	if got := seedScores["internal/util/misc.go"]; got <= 0.5 || got >= 0.6 {
		t.Fatalf("borderline file must get a proportional small penalty, got %v", got)
	}
}

// The calibration guard: when jeff's choice answer is compressed around
// uniform (no real signal), NOTHING may be demoted — uncertainty must not
// reshuffle the structural order. noul probing showed all-≈uniform is
// exactly what a no-signal response looks like.
func TestApplyJeffArbitration_CompressedDistributionNoop(t *testing.T) {
	seedScores := map[string]float64{
		"a.go": 0.9, "b.go": 0.8, "c.go": 0.7, "d.go": 0.6, "e.go": 0.5,
	}
	sc := &stubJeffScorer{probs: map[string]float64{
		"a.go": 0.21, "b.go": 0.20, "c.go": 0.20, "d.go": 0.20, "e.go": 0.19,
	}}
	applyJeffArbitration(context.Background(), Deps{JeffScorer: sc}, "q", seedScores)
	// uniform = 0.2; a/b/c/d at-or-above, e.go 0.19 → penalty 0.35*0.01/0.2
	// = 0.0175 — a hair below 0.5, NOT demoted out.
	if got := seedScores["e.go"]; got < 0.48 {
		t.Fatalf("compressed distribution must not demote, e.go got %v", got)
	}
	if seedScores["a.go"] != 0.9 {
		t.Fatal("at-uniform file changed")
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
		probs: map[string]float64{"a.go": 0.01, "b.go": 0.01, "c.go": 0.01, "d.go": 0.01},
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
