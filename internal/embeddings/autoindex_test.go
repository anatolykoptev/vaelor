package embeddings

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"

	"github.com/prometheus/client_golang/prometheus"
)

// fakeIndexer is a deterministic *Pipeline replacement used to assert
// retry / concurrency behaviour without standing up Postgres + embed-server.
type fakeIndexer struct {
	mu        sync.Mutex
	calls     map[string]int     // calls per repoKey
	failPlan  map[string][]error // queued errors; nil entry = success
	delay     time.Duration      // optional per-call work simulation
	active    int32              // current in-flight calls
	maxActive int32              // peak observed concurrency
}

func newFakeIndexer() *fakeIndexer {
	return &fakeIndexer{
		calls:    map[string]int{},
		failPlan: map[string][]error{},
	}
}

func (f *fakeIndexer) IndexRepo(ctx context.Context, repoKey, _ string) (*IndexResult, error) {
	now := atomic.AddInt32(&f.active, 1)
	defer atomic.AddInt32(&f.active, -1)
	for {
		old := atomic.LoadInt32(&f.maxActive)
		if now <= old || atomic.CompareAndSwapInt32(&f.maxActive, old, now) {
			break
		}
	}
	if f.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.delay):
		}
	}

	f.mu.Lock()
	f.calls[repoKey]++
	var nextErr error
	if plan, ok := f.failPlan[repoKey]; ok && len(plan) > 0 {
		nextErr = plan[0]
		f.failPlan[repoKey] = plan[1:]
	}
	f.mu.Unlock()

	if nextErr != nil {
		return nil, nextErr
	}
	return &IndexResult{Indexed: 1, Total: 1}, nil
}

// IncrementalSync satisfies the repoIndexer interface. It delegates to IndexRepo
// so that all existing retry / concurrency tests continue to exercise their code
// paths (fail plans, delay, active-count tracking) without modification.
func (f *fakeIndexer) IncrementalSync(ctx context.Context, repoKey, root string) (*IncrementalSyncResult, error) {
	result, err := f.IndexRepo(ctx, repoKey, root)
	if err != nil {
		return nil, err
	}
	return &IncrementalSyncResult{
		Mode:          "incremental",
		FilesEmbedded: result.Indexed,
		FilesSkipped:  result.Skipped,
	}, nil
}

func (f *fakeIndexer) callsFor(repoKey string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[repoKey]
}

// makeFakeRepoTree creates a tmp dir tree with N fake git repos.
func makeFakeRepoTree(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, n := range names {
		repo := filepath.Join(root, n)
		git := filepath.Join(repo, ".git")
		if err := os.MkdirAll(git, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", git, err)
		}
	}
	return root
}

// keyByName returns the basename so test assertions map directly to repo names.
func keyByName(root string) string { return filepath.Base(root) }

func TestAutoIndex_SuccessOnAllRepos(t *testing.T) {
	root := makeFakeRepoTree(t, "alpha", "bravo", "charlie")
	f := newFakeIndexer()

	autoIndex(context.Background(), f, []string{root}, keyByName, AutoIndexOpts{
		Concurrency: 2,
		RetryMax:    3,
		RetryBase:   1 * time.Millisecond,
	})

	for _, name := range []string{"alpha", "bravo", "charlie"} {
		if got := f.callsFor(name); got != 1 {
			t.Errorf("repo %q: expected 1 call, got %d", name, got)
		}
	}
}

func TestAutoIndex_RetriesOnTransientFailureThenSucceeds(t *testing.T) {
	root := makeFakeRepoTree(t, "flaky")
	f := newFakeIndexer()
	transient := errors.New("connection refused")
	f.failPlan["flaky"] = []error{transient, transient, nil} // 2 fails then success on attempt 3

	autoIndex(context.Background(), f, []string{root}, keyByName, AutoIndexOpts{
		Concurrency: 1,
		RetryMax:    3,
		RetryBase:   1 * time.Millisecond,
	})

	if got := f.callsFor("flaky"); got != 3 {
		t.Errorf("flaky: expected 3 calls (2 retries + success), got %d", got)
	}
}

func TestAutoIndex_AllFailReachesMaxRetries(t *testing.T) {
	root := makeFakeRepoTree(t, "broken", "healthy")
	f := newFakeIndexer()
	transient := errors.New("503 service unavailable")
	// broken always fails, healthy always succeeds
	f.failPlan["broken"] = []error{transient, transient, transient, transient, transient}

	autoIndex(context.Background(), f, []string{root}, keyByName, AutoIndexOpts{
		Concurrency: 2,
		RetryMax:    3,
		RetryBase:   1 * time.Millisecond,
	})

	// broken: 1 initial + 3 retries = 4 attempts (RetryMax=3 means 3 retries).
	if got := f.callsFor("broken"); got != 4 {
		t.Errorf("broken: expected 4 attempts (1 + RetryMax=3), got %d", got)
	}
	// healthy unblocked despite broken failing.
	if got := f.callsFor("healthy"); got != 1 {
		t.Errorf("healthy: expected 1 call, got %d", got)
	}
}

func TestAutoIndex_NonRetryableFailsImmediately(t *testing.T) {
	root := makeFakeRepoTree(t, "schemabreak")
	f := newFakeIndexer()
	parseErr := errors.New("parse: unexpected token")
	f.failPlan["schemabreak"] = []error{parseErr, nil, nil}

	autoIndex(context.Background(), f, []string{root}, keyByName, AutoIndexOpts{
		Concurrency: 1,
		RetryMax:    3,
		RetryBase:   1 * time.Millisecond,
	})

	if got := f.callsFor("schemabreak"); got != 1 {
		t.Errorf("non-retryable: expected 1 attempt, got %d", got)
	}
}

func TestAutoIndex_ContextCanceledNoRetry(t *testing.T) {
	root := makeFakeRepoTree(t, "willcancel")
	f := newFakeIndexer()
	f.failPlan["willcancel"] = []error{context.Canceled, nil, nil}

	autoIndex(context.Background(), f, []string{root}, keyByName, AutoIndexOpts{
		Concurrency: 1,
		RetryMax:    3,
		RetryBase:   1 * time.Millisecond,
	})

	if got := f.callsFor("willcancel"); got != 1 {
		t.Errorf("ctx.Canceled: expected 1 attempt (non-retryable), got %d", got)
	}
}

func TestAutoIndex_DeadlineExceededIsRetryable(t *testing.T) {
	root := makeFakeRepoTree(t, "slow")
	f := newFakeIndexer()
	f.failPlan["slow"] = []error{
		fmt.Errorf("embed: %w", context.DeadlineExceeded),
		nil,
	}

	autoIndex(context.Background(), f, []string{root}, keyByName, AutoIndexOpts{
		Concurrency: 1,
		RetryMax:    3,
		RetryBase:   1 * time.Millisecond,
	})

	if got := f.callsFor("slow"); got != 2 {
		t.Errorf("deadline exceeded: expected 2 attempts (retry), got %d", got)
	}
}

// TestAutoIndex_SerialNoRetryRollbackInvariant proves the rollback config
// (Concurrency=1, RetryMax=0) executes exactly one IndexRepo per repo with
// no retries — byte-identical observable behavior to the pre-Stream-5
// serial loop.
func TestAutoIndex_SerialNoRetryRollbackInvariant(t *testing.T) {
	repos := []string{"a", "b", "c", "d"}
	root := makeFakeRepoTree(t, repos...)
	f := newFakeIndexer()
	transient := errors.New("connection refused")
	// Every repo fails transiently — under retry-disabled this must NOT retry.
	for _, name := range repos {
		f.failPlan[name] = []error{transient, nil, nil}
	}

	autoIndex(context.Background(), f, []string{root}, keyByName, AutoIndexOpts{
		Concurrency: 1,
		RetryMax:    0,
		RetryBase:   1 * time.Millisecond,
	})

	// Each repo: exactly one attempt (no retry), failure swallowed with warn.
	for _, name := range repos {
		if got := f.callsFor(name); got != 1 {
			t.Errorf("rollback invariant: repo %q expected 1 attempt, got %d", name, got)
		}
	}
	// Concurrency=1 must serialize.
	peak := atomic.LoadInt32(&f.maxActive)
	if peak > 1 {
		t.Errorf("rollback invariant: expected serial execution (peak=1), got peak=%d", peak)
	}
}

func TestAutoIndex_ConcurrencyCapRespected(t *testing.T) {
	names := []string{"r1", "r2", "r3", "r4", "r5", "r6"}
	root := makeFakeRepoTree(t, names...)
	f := newFakeIndexer()
	f.delay = 25 * time.Millisecond // ensure overlap window

	autoIndex(context.Background(), f, []string{root}, keyByName, AutoIndexOpts{
		Concurrency: 2,
		RetryMax:    0,
		RetryBase:   1 * time.Millisecond,
	})

	peak := atomic.LoadInt32(&f.maxActive)
	if peak > 2 {
		t.Errorf("concurrency cap: expected peak<=2, got %d", peak)
	}
	if peak < 2 {
		// On extremely loaded test runners we may not observe true overlap.
		// Soft-fail: log but don't fail the test for this signal alone.
		t.Logf("concurrency cap: peak=%d (cap=2). Test runner may be slow.", peak)
	}
	// All 6 repos must complete.
	var total int
	for _, n := range names {
		total += f.callsFor(n)
	}
	if total != len(names) {
		t.Errorf("expected %d total calls, got %d", len(names), total)
	}
}

func TestClassifyAutoIndexError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"canceled", context.Canceled, retryReasonNonRetryable},
		{"deadline", context.DeadlineExceeded, retryReasonDeadline},
		{"deadline_wrapped", fmt.Errorf("wrap: %w", context.DeadlineExceeded), retryReasonDeadline},
		{"conn_refused", errors.New("dial tcp: connection refused"), retryReasonConnRefused},
		{"503", errors.New("embed-server returned 503"), retryReason5xx},
		{"504", errors.New("upstream 504 gateway timeout"), retryReason5xx},
		{"502", errors.New("502 bad gateway"), retryReason5xx},
		{"parse_error", errors.New("parse: unexpected EOF"), retryReasonNonRetryable},
		{"schema_error", errors.New("invalid embedding dimension"), retryReasonNonRetryable},
	}
	// Sort to keep test output stable across runs.
	sort.Slice(cases, func(i, j int) bool { return cases[i].name < cases[j].name })
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyAutoIndexError(tc.err)
			if got != tc.want {
				t.Errorf("classifyAutoIndexError(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func TestNormalizeOpts(t *testing.T) {
	got := normalizeOpts(AutoIndexOpts{Concurrency: 0, RetryMax: -1, RetryBase: 0})
	if got.Concurrency != 1 {
		t.Errorf("Concurrency<1 should clamp to 1, got %d", got.Concurrency)
	}
	if got.RetryMax != 0 {
		t.Errorf("RetryMax<0 should clamp to 0, got %d", got.RetryMax)
	}
	if got.RetryBase != defaultAutoIndexRetryBase {
		t.Errorf("RetryBase<=0 should default, got %v", got.RetryBase)
	}
}

func TestDefaultAutoIndexOpts(t *testing.T) {
	d := DefaultAutoIndexOpts()
	// Concurrency=1 serializes embed calls onto the single-worker embed backend
	// so queue depth stays bounded. Reverted from 2 after fleet overload caused
	// unbounded queuing and context deadline exceeded on every second-repo batch.
	if d.Concurrency != 1 {
		t.Errorf("default Concurrency=1 (single-worker embed backend guard), got %d", d.Concurrency)
	}
	if d.RetryMax != 3 {
		t.Errorf("default RetryMax=3, got %d", d.RetryMax)
	}
	if d.RetryBase != 5*time.Second {
		t.Errorf("default RetryBase=5s, got %v", d.RetryBase)
	}
}

// TestAutoIndex_InFlightGaugeZeroAfterCompletion verifies the in-flight gauge
// returns to 0 after all repos finish. This test would fail if the Inc/Dec
// calls in autoIndex were removed or mis-paired (e.g. only Inc, no defer Dec).
// Falsification: deleting the "defer autoindexInFlight.Dec()" line in autoindex.go
// would leave the gauge at N>0 after this test, causing it to fail.
func TestAutoIndex_InFlightGaugeZeroAfterCompletion(t *testing.T) {
	root := makeFakeRepoTree(t, "g1", "g2", "g3")
	f := newFakeIndexer()
	f.delay = 5 * time.Millisecond

	autoIndex(context.Background(), f, []string{root}, keyByName, AutoIndexOpts{
		Concurrency: 1,
		RetryMax:    0,
		RetryBase:   1 * time.Millisecond,
	})

	// After autoIndex returns all goroutines have exited; gauge must be 0.
	got := gaugeValue(t, autoindexInFlight)
	if got != 0 {
		t.Errorf("in-flight gauge: expected 0 after completion, got %g", got)
	}
}

// TestAutoIndex_DeferredCounterIncremented verifies that every repo goroutine
// bumps gocode_autoindex_deferred_total before acquiring the semaphore.
// Falsification: removing "recordAutoIndexDeferred(r.key)" from autoIndex would
// leave the counter unchanged and this test would fail (counter stays at 0 for
// these repos, so we'd get 0 != n).
func TestAutoIndex_DeferredCounterIncremented(t *testing.T) {
	repos := []string{"d1", "d2", "d3"}
	root := makeFakeRepoTree(t, repos...)
	f := newFakeIndexer()

	// Snapshot counter before the run.
	before := counterVecSum(t, autoindexDeferredTotal)

	autoIndex(context.Background(), f, []string{root}, keyByName, AutoIndexOpts{
		Concurrency: 1,
		RetryMax:    0,
		RetryBase:   1 * time.Millisecond,
	})

	after := counterVecSum(t, autoindexDeferredTotal)
	added := after - before
	if int(added) != len(repos) {
		t.Errorf("deferred counter: expected +%d (one per repo), got +%g", len(repos), added)
	}
}

// gaugeValue reads the current float value of a prometheus.Gauge.
func gaugeValue(t *testing.T, g prometheus.Gauge) float64 {
	t.Helper()
	var m dto.Metric
	if err := g.Write(&m); err != nil {
		t.Fatalf("gauge.Write: %v", err)
	}
	if m.Gauge == nil {
		return 0
	}
	return m.Gauge.GetValue()
}

// counterVecSum returns the sum of all label-value pairs in a CounterVec by
// collecting via the prometheus.Collector interface.
func counterVecSum(t *testing.T, cv prometheus.Collector) float64 {
	t.Helper()
	ch := make(chan prometheus.Metric, 128)
	cv.Collect(ch)
	close(ch)
	var sum float64
	for m := range ch {
		var dm dto.Metric
		if err := m.Write(&dm); err == nil && dm.Counter != nil {
			sum += dm.Counter.GetValue()
		}
	}
	return sum
}
