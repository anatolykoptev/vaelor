package embeddings

import (
	"context"
	"testing"
	"time"
)

// busyIndexer is a fakeIndexer that reports one repo as already being indexed
// (a lazy semantic_search index or a previous pass still running).
type busyIndexer struct {
	*fakeIndexer
	busy string
}

func (b busyIndexer) IsIndexing(repoKey string) bool { return repoKey == b.busy }

func runLoop(t *testing.T, idx repoIndexer, dirs []string, interval time.Duration, until func() bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		autoIndexLoop(ctx, idx, dirs, keyByName, AutoIndexOpts{Concurrency: 1, RetryBase: time.Millisecond}, interval)
	}()
	deadline := time.After(5 * time.Second)
	for !until() {
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("condition not reached before the deadline")
		case <-time.After(5 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("autoIndexLoop did not return after context cancel")
	}
}

// A repo whose main moves after boot must be re-synced without a restart: the
// loop re-runs the pass every interval. Before the loop existed, head_sha only
// advanced at boot or when semantic_search happened to be called, so an
// untouched local repo stayed at its boot-time SHA for as long as the service
// stayed up.
func TestAutoIndexLoop_ResyncsEveryInterval(t *testing.T) {
	root := makeFakeRepoTree(t, "alpha", "bravo")
	f := newFakeIndexer()
	runLoop(t, f, []string{root}, 10*time.Millisecond, func() bool {
		return f.callsFor("alpha") >= 3 && f.callsFor("bravo") >= 3
	})
}

// A repo with an index already in flight is skipped, never run concurrently:
// the incremental path writes per-file rows without taking the single-flight
// slot, so overlapping it with a full index of the same repo is a race.
func TestAutoIndexLoop_SkipsRepoAlreadyIndexing(t *testing.T) {
	root := makeFakeRepoTree(t, "alpha", "busy")
	f := newFakeIndexer()
	runLoop(t, busyIndexer{fakeIndexer: f, busy: "busy"}, []string{root}, 10*time.Millisecond, func() bool {
		return f.callsFor("alpha") >= 3
	})
	if got := f.callsFor("busy"); got != 0 {
		t.Errorf("repo already being indexed was synced %d times; want 0", got)
	}
}

// interval <= 0 disables the loop: it returns at once and indexes nothing, so
// the knob can switch the feature off without a code change.
func TestAutoIndexLoop_NonPositiveIntervalIsOff(t *testing.T) {
	root := makeFakeRepoTree(t, "alpha")
	f := newFakeIndexer()
	done := make(chan struct{})
	go func() {
		defer close(done)
		autoIndexLoop(context.Background(), f, []string{root}, keyByName, AutoIndexOpts{}, 0)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("autoIndexLoop with interval 0 must return immediately")
	}
	if got := f.callsFor("alpha"); got != 0 {
		t.Errorf("disabled loop indexed %d times; want 0", got)
	}
}
