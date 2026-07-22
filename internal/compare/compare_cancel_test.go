package compare

import (
	"context"
	"testing"
	"time"

	"github.com/anatolykoptev/go-kit/llm"
)

// TestCompareRepos_PreCanceledCtx_ReturnsPartialPromptly proves #580: a
// pre-canceled ctx must bail to a PARTIAL result promptly (< 2s), not blow
// past the client timeout computing CPU-bound stages that the client will
// never see. RED-on-revert: if the ctx.Err() checks in MatchSymbols /
// CompareRepos are removed, the test either returns an error (no partial) or
// takes far longer than 2s.
func TestCompareRepos_PreCanceledCtx_ReturnsPartialPromptly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	root := findRepoRootInternal(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel immediately

	t0 := time.Now()
	result, err := CompareRepos(ctx, CompareInput{
		RootA: root,
		RootB: root,
		Query: "test cancel",
		Opts:  SnapshotOpts{Language: "go"},
	}, llm.NoOp{})
	elapsed := time.Since(t0)

	// A canceled ctx must NOT return a hard error — it must return a partial
	// result so the tool handler can render it with a partial footer.
	if err != nil {
		t.Fatalf("CompareRepos returned error on canceled ctx (want partial result): %v", err)
	}
	if result == nil {
		t.Fatal("CompareRepos returned nil result on canceled ctx (want partial result)")
	}
	if !result.Partial {
		t.Fatal("result.Partial = false, want true (ctx was canceled)")
	}

	// Must return promptly — the whole point of #580 is not computing past
	// the point anyone is listening.
	if elapsed > 2*time.Second {
		t.Fatalf("CompareRepos took %s on canceled ctx, want < 2s", elapsed)
	}
	t.Logf("CompareRepos with pre-canceled ctx returned in %s (partial=%v)", elapsed, result.Partial)
}

// TestCompareRepos_ShortDeadline_ReturnsPartial verifies that a short deadline
// that fires during CPU-bound stages produces a partial result, not a hard
// error. Uses a 5ms deadline — enough for IngestRepo to start but not enough
// for the full parse+match pipeline.
func TestCompareRepos_ShortDeadline_ReturnsPartial(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	root := findRepoRootInternal(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	// Burn the deadline before entering CompareRepos so the ctx is already
	// expired when the first stage runs.
	time.Sleep(10 * time.Millisecond)

	t0 := time.Now()
	result, err := CompareRepos(ctx, CompareInput{
		RootA: root,
		RootB: root,
		Query: "test short deadline",
		Opts:  SnapshotOpts{Language: "go"},
	}, llm.NoOp{})
	elapsed := time.Since(t0)

	if err != nil {
		t.Fatalf("CompareRepos returned error on expired ctx (want partial result): %v", err)
	}
	if result == nil {
		t.Fatal("CompareRepos returned nil result on expired ctx")
	}
	if !result.Partial {
		t.Fatal("result.Partial = false, want true (ctx deadline expired)")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("CompareRepos took %s on expired ctx, want < 2s", elapsed)
	}
	t.Logf("CompareRepos with expired ctx returned in %s (partial=%v)", elapsed, result.Partial)
}
