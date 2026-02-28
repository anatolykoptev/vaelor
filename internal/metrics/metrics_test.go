package metrics_test

import (
	"errors"
	"testing"

	"github.com/anatolykoptev/go-code/internal/metrics"
)

func TestIncrAndSnapshot(t *testing.T) {
	metrics.Reset()

	metrics.Incr(metrics.LLMCalls)
	metrics.Incr(metrics.LLMCalls)
	metrics.Incr(metrics.LLMErrors)

	snap := metrics.Snapshot()

	if got := snap[metrics.LLMCalls]; got != 2 {
		t.Errorf("LLMCalls: want 2, got %d", got)
	}

	if got := snap[metrics.LLMErrors]; got != 1 {
		t.Errorf("LLMErrors: want 1, got %d", got)
	}

	if got, ok := snap[metrics.SearchRequests]; ok {
		t.Errorf("SearchRequests: expected absent, got %d", got)
	}
}

func TestSnapshotIsCopy(t *testing.T) {
	metrics.Reset()

	metrics.Incr(metrics.CacheHits)
	snap := metrics.Snapshot()
	snap[metrics.CacheHits] = 999

	snap2 := metrics.Snapshot()
	if got := snap2[metrics.CacheHits]; got != 1 {
		t.Errorf("Snapshot not isolated: want 1, got %d", got)
	}
}

func TestTrackOperationSuccess(t *testing.T) {
	metrics.Reset()

	err := metrics.TrackOperation(metrics.LLMCalls, metrics.LLMErrors, func() error {
		return nil
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	snap := metrics.Snapshot()
	if got := snap[metrics.LLMCalls]; got != 1 {
		t.Errorf("LLMCalls: want 1, got %d", got)
	}
	if got := snap[metrics.LLMErrors]; got != 0 {
		t.Errorf("LLMErrors: want 0, got %d", got)
	}
}

func TestTrackOperationFailure(t *testing.T) {
	metrics.Reset()

	sentinel := errors.New("boom")
	err := metrics.TrackOperation(metrics.GitHubAPICalls, metrics.LLMErrors, func() error {
		return sentinel
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("want sentinel error, got %v", err)
	}

	snap := metrics.Snapshot()
	if got := snap[metrics.GitHubAPICalls]; got != 1 {
		t.Errorf("GitHubAPICalls: want 1, got %d", got)
	}
	if got := snap[metrics.LLMErrors]; got != 1 {
		t.Errorf("LLMErrors: want 1, got %d", got)
	}
}
