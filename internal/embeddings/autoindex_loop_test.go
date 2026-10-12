package embeddings

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// busyIndexer is a fakeIndexer that reports one repo as already being indexed
// (a lazy semantic_search index or a previous pass still running).
type busyIndexer struct {
	*fakeIndexer
	busy string
}

func (b busyIndexer) IsIndexing(repoKey string) bool { return repoKey == b.busy }

// gatedIndexer is a fakeIndexer with the periodic-pass eligibility gate.
type gatedIndexer struct {
	*fakeIndexer
	reasons map[string]string
}

func (g gatedIndexer) LoopSkipReason(_ context.Context, repoKey, _ string) string {
	return g.reasons[repoKey]
}

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

// captureLogs routes slog to a buffer at debug level for the test.
func captureLogs(t *testing.T) *lockedBuf {
	t.Helper()
	buf := &lockedBuf{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func (l *lockedBuf) count(sub string) int { return strings.Count(l.String(), sub) }

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

// The skip path is an enforcement path: it must be countable.
func TestAutoIndex_SkippedBusyIsCounted(t *testing.T) {
	root := makeFakeRepoTree(t, "busy")
	before := testutil.ToFloat64(autoindexSkippedBusyTotal.WithLabelValues("busy"))
	autoIndex(context.Background(), busyIndexer{fakeIndexer: newFakeIndexer(), busy: "busy"},
		[]string{root}, keyByName, AutoIndexOpts{Concurrency: 1})
	if got := testutil.ToFloat64(autoindexSkippedBusyTotal.WithLabelValues("busy")) - before; got != 1 {
		t.Errorf("gocode_autoindex_skipped_busy_total moved by %v; want 1", got)
	}
}

// Liveness is stamped by a pass that ran to its end, including one that found
// no repos, and by nothing else.
func TestAutoIndex_LivenessStampedOnlyByCompletedPass(t *testing.T) {
	root := makeFakeRepoTree(t, "alpha")
	empty := t.TempDir()

	autoindexLastPassTimestamp.Set(0)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	autoIndex(cancelled, newFakeIndexer(), []string{root}, keyByName, AutoIndexOpts{Concurrency: 1})
	if got := testutil.ToFloat64(autoindexLastPassTimestamp); got != 0 {
		t.Errorf("a pass cut short by cancellation stamped liveness (%v)", got)
	}

	autoIndex(context.Background(), newFakeIndexer(), []string{root}, keyByName, AutoIndexOpts{Concurrency: 1})
	if got := testutil.ToFloat64(autoindexLastPassTimestamp); got == 0 {
		t.Error("a completed pass did not stamp liveness")
	}

	autoindexLastPassTimestamp.Set(0)
	autoIndex(context.Background(), newFakeIndexer(), []string{empty}, keyByName, AutoIndexOpts{Concurrency: 1})
	if got := testutil.ToFloat64(autoindexLastPassTimestamp); got == 0 {
		t.Error("a pass over a directory with zero repos did not stamp liveness")
	}
}

// A pass in which every repo fails is counted; one failure among successes is not.
func TestAutoIndex_PassFailuresCountOnlyTotalFailure(t *testing.T) {
	root := makeFakeRepoTree(t, "alpha", "bravo")
	boom := errors.New("parse error") // non-retryable
	allFail := newFakeIndexer()
	allFail.failPlan["alpha"] = []error{boom}
	allFail.failPlan["bravo"] = []error{boom}

	before := testutil.ToFloat64(autoindexPassFailuresTotal)
	autoIndex(context.Background(), allFail, []string{root}, keyByName, AutoIndexOpts{Concurrency: 1})
	if got := testutil.ToFloat64(autoindexPassFailuresTotal) - before; got != 1 {
		t.Errorf("pass_failures_total moved by %v for an all-failed pass; want 1", got)
	}

	oneFail := newFakeIndexer()
	oneFail.failPlan["alpha"] = []error{boom}
	before = testutil.ToFloat64(autoindexPassFailuresTotal)
	autoIndex(context.Background(), oneFail, []string{root}, keyByName, AutoIndexOpts{Concurrency: 1})
	if got := testutil.ToFloat64(autoindexPassFailuresTotal) - before; got != 0 {
		t.Errorf("pass_failures_total moved by %v for a partially failed pass; want 0", got)
	}
}

// A repo whose main has not moved is routine on a periodic pass: debug, not
// info. At boot it stays info.
func TestAutoIndex_PeriodicDemotesUnchangedLog(t *testing.T) {
	root := makeFakeRepoTree(t, "alpha")
	logs := captureLogs(t)
	f := newFakeIndexer()
	f.mode = IncrementalSyncSkipSHAMatch

	autoIndex(context.Background(), f, []string{root}, keyByName, AutoIndexOpts{Concurrency: 1, Periodic: true})
	if n := logs.count("level=INFO msg=\"autoindex: done\""); n != 0 {
		t.Errorf("periodic pass logged an unchanged repo at INFO (%d lines)", n)
	}
	autoIndex(context.Background(), f, []string{root}, keyByName, AutoIndexOpts{Concurrency: 1})
	if n := logs.count("level=INFO msg=\"autoindex: done\""); n != 1 {
		t.Errorf("boot pass must log an unchanged repo at INFO; got %d lines", n)
	}
}

// Repos the loop must not touch are skipped, counted by reason and logged once.
// The boot pass is not subject to the gate.
func TestAutoIndex_PeriodicSkipsIneligibleRepos(t *testing.T) {
	root := makeFakeRepoTree(t, "alpha", "worktree", "docs")
	logs := captureLogs(t)
	f := newFakeIndexer()
	g := gatedIndexer{fakeIndexer: f, reasons: map[string]string{"worktree": loopSkipNoGit, "docs": loopSkipNoEmbeddings}}
	opts := AutoIndexOpts{Concurrency: 1, Periodic: true}

	noGit := testutil.ToFloat64(autoindexLoopSkippedTotal.WithLabelValues(loopSkipNoGit))
	loopSkipLogged.Delete("worktree|" + loopSkipNoGit)
	for range 3 {
		autoIndex(context.Background(), g, []string{root}, keyByName, opts)
	}
	if f.callsFor("worktree") != 0 || f.callsFor("docs") != 0 {
		t.Errorf("ineligible repos were synced: worktree=%d docs=%d", f.callsFor("worktree"), f.callsFor("docs"))
	}
	if f.callsFor("alpha") != 3 {
		t.Errorf("eligible repo synced %d times; want 3", f.callsFor("alpha"))
	}
	if got := testutil.ToFloat64(autoindexLoopSkippedTotal.WithLabelValues(loopSkipNoGit)) - noGit; got != 3 {
		t.Errorf("loop_skipped_total{no_git} moved by %v; want 3", got)
	}
	if n := logs.count("repo=worktree reason=no_git"); n != 1 {
		t.Errorf("skip logged %d times across 3 passes; want exactly once", n)
	}

	autoIndex(context.Background(), g, []string{root}, keyByName, AutoIndexOpts{Concurrency: 1})
	if f.callsFor("worktree") != 1 {
		t.Errorf("boot pass must not apply the periodic gate; worktree synced %d times", f.callsFor("worktree"))
	}
}

// A pass stops at its time budget and the next tick continues: repos reach the
// semaphore in no fixed order, so a budget-limited pass hands different repos to
// the next one and none starves.
func TestRunLoopPass_StopsAtBudgetAndNextTickContinues(t *testing.T) {
	names := []string{"a", "b", "c", "d"}
	root := makeFakeRepoTree(t, names...)
	f := newFakeIndexer()
	f.delay = 40 * time.Millisecond
	opts := AutoIndexOpts{Concurrency: 1, Periodic: true, PassBudget: 70 * time.Millisecond, RetryBase: time.Millisecond}
	total := func() (n int) {
		for _, k := range names {
			n += f.callsFor(k)
		}
		return n
	}

	before := testutil.ToFloat64(autoindexPassBudgetExceededTotal)
	runLoopPass(context.Background(), f, []string{root}, keyByName, opts)
	if first := total(); first == 0 || first >= len(names) {
		t.Fatalf("pass 1 completed %d of %d repos; want some but not all (the budget must stop it)", first, len(names))
	}
	if got := testutil.ToFloat64(autoindexPassBudgetExceededTotal) - before; got != 1 {
		t.Errorf("pass_budget_exceeded_total moved by %v; want 1", got)
	}
	allRan := func() bool {
		for _, k := range names {
			if f.callsFor(k) == 0 {
				return false
			}
		}
		return true
	}
	for pass := 0; pass < 150 && !allRan(); pass++ {
		runLoopPass(context.Background(), f, []string{root}, keyByName, opts)
	}
	if !allRan() {
		t.Errorf("some repo never ran across 150 budget-limited passes: starved")
	}
}

// The per-repo file cap reaches the sync through the context on periodic passes
// only.
func TestAutoIndex_PeriodicCarriesFileCap(t *testing.T) {
	root := makeFakeRepoTree(t, "alpha")
	f := newFakeIndexer()
	autoIndex(context.Background(), f, []string{root}, keyByName, AutoIndexOpts{Concurrency: 1, Periodic: true, MaxFilesPerRepo: 7})
	autoIndex(context.Background(), f, []string{root}, keyByName, AutoIndexOpts{Concurrency: 1, MaxFilesPerRepo: 7})
	if len(f.capSeen) != 2 || f.capSeen[0] != 7 || f.capSeen[1] != 0 {
		t.Errorf("file cap seen by the sync = %v; want [7 0] (periodic capped, boot not)", f.capSeen)
	}
}

// A panic while syncing one repo is recovered, counted, and does not stop the
// other repos or the loop.
func TestAutoIndex_PanicInOneRepoIsRecovered(t *testing.T) {
	root := makeFakeRepoTree(t, "alpha", "bad", "charlie")
	f := newFakeIndexer()
	f.panicOn = "bad"
	before := testutil.ToFloat64(autoindexPanicsTotal)
	res := autoIndex(context.Background(), f, []string{root}, keyByName, AutoIndexOpts{Concurrency: 1})
	if got := testutil.ToFloat64(autoindexPanicsTotal) - before; got != 1 {
		t.Errorf("panics_total moved by %v; want 1", got)
	}
	if f.callsFor("alpha") != 1 || f.callsFor("charlie") != 1 {
		t.Errorf("other repos did not run after the panic: alpha=%d charlie=%d", f.callsFor("alpha"), f.callsFor("charlie"))
	}
	if res.Failed != 1 || res.OK != 2 {
		t.Errorf("pass result = %+v; want 1 failed, 2 ok", res)
	}
}

// A panic outside any per-repo goroutine (here: the repo key function) is
// recovered at the pass level and the next pass still runs.
func TestRunLoopPass_PanicInPassIsRecovered(t *testing.T) {
	root := makeFakeRepoTree(t, "alpha")
	f := newFakeIndexer()
	boom := func(string) string { panic("keyFn exploded") }
	before := testutil.ToFloat64(autoindexPanicsTotal)
	runLoopPass(context.Background(), f, []string{root}, boom, AutoIndexOpts{Concurrency: 1, Periodic: true, PassBudget: time.Minute})
	if got := testutil.ToFloat64(autoindexPanicsTotal) - before; got != 1 {
		t.Errorf("panics_total moved by %v; want 1", got)
	}
	runLoopPass(context.Background(), f, []string{root}, keyByName, AutoIndexOpts{Concurrency: 1, Periodic: true, PassBudget: time.Minute})
	if f.callsFor("alpha") != 1 {
		t.Error("the pass after a recovered panic did not run")
	}
}
