package callgraph

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
)

const mib = int64(1) << 20

// withBudget swaps the process-wide typed-load budget and loader for one test.
func withBudget(t *testing.T, capacity int64, fn func(context.Context, string, goanalysis.LoadOpts) (*goanalysis.LoadResult, error)) {
	t.Helper()
	oldB, oldFn := typedBudget.Load(), typedLoadFn.Load()
	typedBudget.Store(newBudget(capacity))
	typedLoadFn.Store(&fn)
	t.Cleanup(func() {
		// Detached flights read typedBudget/typedLoadFn: let them drain before
		// the globals are restored.
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			flightsMu.Lock()
			n := len(flights)
			flightsMu.Unlock()
			if n == 0 && budgetFree() {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		typedBudget.Store(oldB)
		typedLoadFn.Store(oldFn)
	})
}

func goModDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/t\n\ngo 1.21\n"), 0o600))
	return dir
}

// budgetFree reports whether the whole budget can be taken right now.
func budgetFree() bool {
	if typedBudget.Load().sem.TryAcquire(typedBudget.Load().capacity) {
		typedBudget.Load().sem.Release(typedBudget.Load().capacity)
		return true
	}
	return false
}

func eventually(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	require.Eventually(t, cond, 3*time.Second, 5*time.Millisecond, msg)
}

// N concurrent requests for one module must run ONE load and share its result.
//
// Mutation that must turn it RED: in loadTypedShared (typed_load.go) change
// `if f == nil {` to `if true {` so every caller starts its own load.
func TestLoadTypedShared_ConcurrentCallersShareOneLoad(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	shared := &goanalysis.LoadResult{}
	withBudget(t, 1024*mib, func(context.Context, string, goanalysis.LoadOpts) (*goanalysis.LoadResult, error) {
		calls.Add(1)
		<-release
		return shared, nil
	})
	dir := goModDir(t)

	const n = 8
	var wg sync.WaitGroup
	got := make([]*goanalysis.LoadResult, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lr, rel, err := loadTypedShared(context.Background(), dir, goanalysis.LoadOpts{Tests: true})
			assert.NoError(t, err)
			got[i] = lr
			if rel != nil {
				rel()
			}
		}()
	}
	// Let every caller register before the one load completes.
	eventually(t, func() bool {
		flightsMu.Lock()
		defer flightsMu.Unlock()
		f := flights[dir+"|tests=true|src=false"]
		return f != nil && f.refs == n
	}, "all callers must join the same flight")
	close(release)
	wg.Wait()

	assert.EqualValues(t, 1, calls.Load(), "one load for %d concurrent requests", n)
	for i, lr := range got {
		assert.Same(t, shared, lr, "caller %d must share the one result", i)
	}
	eventually(t, budgetFree, "budget must be returned once every holder released")
}

// Loads of different modules are admitted against the budget: with room for one
// at a time, four concurrent loads never overlap.
//
// Mutation that must turn it RED: in sharedLoad.run (typed_load.go) replace
// `f.weight = b.weight(countGoFiles(root))` with `f.weight = 0`.
func TestTypedBudget_BoundsConcurrentLoads(t *testing.T) {
	var running, peak atomic.Int32
	withBudget(t, 64*mib, func(context.Context, string, goanalysis.LoadOpts) (*goanalysis.LoadResult, error) {
		cur := running.Add(1)
		for {
			p := peak.Load()
			if cur <= p || peak.CompareAndSwap(p, cur) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		running.Add(-1)
		return &goanalysis.LoadResult{}, nil
	})

	var wg sync.WaitGroup
	for range 4 {
		dir := goModDir(t)
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, rel, err := loadTypedShared(context.Background(), dir, goanalysis.LoadOpts{})
			assert.NoError(t, err)
			if rel != nil {
				rel()
			}
		}()
	}
	wg.Wait()
	assert.EqualValues(t, 1, peak.Load(), "a 64 MiB budget admits one 64 MiB load at a time")
}

// A caller whose deadline expires while the load is still queued for budget gets
// errTypedBudget (so the request degrades to the basic tier with the existing
// Warming contract) and is counted; the queued load still runs once budget
// frees, so the background warm has something to join.
//
// Mutation that must turn it RED: in loadTypedShared replace the
// `!f.admitted.Load()` condition with `false`.
func TestLoadTypedShared_DegradesWhenBudgetExhausted(t *testing.T) {
	var started atomic.Int32
	gate := make(chan struct{})
	withBudget(t, 64*mib, func(context.Context, string, goanalysis.LoadOpts) (*goanalysis.LoadResult, error) {
		if started.Add(1) > 1 {
			<-gate // the queued load, once admitted
		}
		return &goanalysis.LoadResult{}, nil
	})
	holder, other := goModDir(t), goModDir(t)

	// holder completes and keeps the whole 64 MiB budget while it reads the result.
	_, relHolder, err := loadTypedShared(context.Background(), holder, goanalysis.LoadOpts{})
	require.NoError(t, err)

	before := gatherCounterSum(t, "gocode_callgraph_gotypes_load_degraded_total", map[string]string{"reason": "budget_wait"})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, rel, err := loadTypedShared(ctx, other, goanalysis.LoadOpts{})
	require.Error(t, err)
	assert.Nil(t, rel)
	assert.True(t, errors.Is(err, errTypedBudget), "queued for budget: %v", err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded), "must still wrap the deadline: %v", err)
	after := gatherCounterSum(t, "gocode_callgraph_gotypes_load_degraded_total", map[string]string{"reason": "budget_wait"})
	assert.EqualValues(t, before+1, after, "degrade must be counted")
	assert.EqualValues(t, 1, started.Load(), "the queued load must not run while the budget is held")

	relHolder()
	close(gate)
	eventually(t, budgetFree, "the abandoned queued load must finish and return its budget")
	assert.EqualValues(t, 2, started.Load(), "the queued load runs once budget frees, so a warm can join it")
}

// The request-path probe gives up at its deadline but the load keeps running;
// the background warm that follows must JOIN it, not build a second arena.
//
// Mutation that must turn it RED: in sharedLoad.drop (typed_load.go) add
// `if f.refs == 0 && !f.finished { delete(flights, f.key) }` inside the lock.
func TestLoadTypedShared_WarmJoinsProbeThatTimedOut(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	withBudget(t, 1024*mib, func(context.Context, string, goanalysis.LoadOpts) (*goanalysis.LoadResult, error) {
		calls.Add(1)
		<-release
		return &goanalysis.LoadResult{}, nil
	})
	dir := goModDir(t)

	probeCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := loadTypedShared(probeCtx, dir, goanalysis.LoadOpts{Tests: true})
	require.Error(t, err)
	assert.False(t, errors.Is(err, errTypedBudget), "the load was admitted: this is a plain deadline")

	done := make(chan error, 1)
	go func() {
		lr, rel, werr := loadTypedShared(context.Background(), dir, goanalysis.LoadOpts{Tests: true})
		if werr == nil {
			assert.NotNil(t, lr)
			rel()
		}
		done <- werr
	}()
	time.Sleep(50 * time.Millisecond)
	close(release)
	require.NoError(t, <-done)
	assert.EqualValues(t, 1, calls.Load(), "probe and warm must share one load")
}

// The budget covers the arena's whole life: it is returned when the last holder
// releases, not when the load returns.
//
// Mutation that must turn it RED: in sharedLoad.run's deferred func
// (typed_load.go) change `if orphan {` to `if true {`.
func TestLoadTypedShared_BudgetHeldUntilLastRelease(t *testing.T) {
	withBudget(t, 64*mib, func(context.Context, string, goanalysis.LoadOpts) (*goanalysis.LoadResult, error) {
		return &goanalysis.LoadResult{}, nil
	})
	dir := goModDir(t)

	_, relA, err := loadTypedShared(context.Background(), dir, goanalysis.LoadOpts{})
	require.NoError(t, err)
	_, relB, err := loadTypedShared(context.Background(), dir, goanalysis.LoadOpts{})
	require.NoError(t, err)

	assert.False(t, budgetFree(), "a live result must keep its budget")
	relA()
	relA() // idempotent: a double release must not free the other holder's share
	assert.False(t, budgetFree(), "one holder still reads the result")
	relB()
	assert.True(t, budgetFree(), "budget returned when the last holder released")
}

// A panic inside the load surfaces to the caller (not a process crash in the
// flight goroutine) and does not leak the budget.
//
// Mutation that must turn it RED: in loadTypedShared delete the `f.drop()` that
// precedes `panic(f.panicVal)`.
func TestLoadTypedShared_PanicPropagatesAndReleasesBudget(t *testing.T) {
	withBudget(t, 64*mib, func(context.Context, string, goanalysis.LoadOpts) (*goanalysis.LoadResult, error) {
		panic("boom")
	})
	dir := goModDir(t)

	func() {
		defer func() { assert.Equal(t, "boom", recover(), "panic must reach the caller") }()
		_, _, _ = loadTypedShared(context.Background(), dir, goanalysis.LoadOpts{})
	}()
	eventually(t, budgetFree, "a panicking load must not leak its budget")
}

// A request-path call whose load cannot be admitted degrades to the existing
// contract: tree-sitter graph stamped WarmPending, no typed edges, no panic.
func TestEnrichWithTypedResolution_BudgetExhaustedDegradesToWarmPending(t *testing.T) {
	block := make(chan struct{})
	withBudget(t, 64*mib, func(context.Context, string, goanalysis.LoadOpts) (*goanalysis.LoadResult, error) {
		<-block
		return &goanalysis.LoadResult{}, nil
	})
	// Hold the whole budget with another module's load.
	holder := goModDir(t)
	holderRel := make(chan func(), 1)
	go func() {
		_, rel, _ := loadTypedShared(context.Background(), holder, goanalysis.LoadOpts{})
		holderRel <- rel
	}()
	eventually(t, func() bool { return !budgetFree() }, "holder must be admitted")
	defer func() {
		close(block)
		(<-holderRel)()
	}()

	old := syncLoadBudget
	syncLoadBudget = 100 * time.Millisecond
	defer func() { syncLoadBudget = old }()

	cg := EnrichWithTypedResolution(context.Background(), goModDir(t), &CallGraph{Tier: "basic"}, nil, nil)
	assert.Equal(t, "basic", cg.Tier)
	assert.Equal(t, WarmPending, cg.Warm, "degraded request keeps the warming contract")
}

func TestBudgetFrom(t *testing.T) {
	limit := func(n int64) func() int64 { return func() int64 { return n } }
	assert.Equal(t, 512*mib, budgetFrom("512", limit(8<<30)), "explicit env wins")
	assert.Equal(t, int64(3)<<29, budgetFrom("", limit(3<<30)), "half the container limit")
	assert.Equal(t, defaultBudgetBytes, budgetFrom("", limit(0)), "no limit detectable")
	assert.Equal(t, int64(3)<<29, budgetFrom("nonsense", limit(3<<30)), "unparsable env falls through")
}

func TestBudgetWeight(t *testing.T) {
	b := newBudget(1024 * mib)
	assert.Equal(t, minLoadWeight, b.weight(3), "tiny module has a floor")
	assert.Equal(t, 500*mib, b.weight(500))
	assert.Equal(t, 1024*mib, b.weight(100000), "an oversize module is clamped so it can still run, alone")
}

func TestCountGoFiles_SkipsVendorAndVCS(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"a.go", "sub/b.go", "sub/b_test.go", "vendor/x/c.go", ".git/d.go", "README.md"} {
		p := filepath.Join(dir, f)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(t, os.WriteFile(p, []byte("package x\n"), 0o600))
	}
	assert.Equal(t, 3, countGoFiles(dir))
}

// The peak-heap histogram must actually be fed by a load, and the sampler must
// see a nonzero heap.
//
// Mutation that must turn it RED: delete the observeTypedLoadPeak(stop()) line
// in sharedLoad.run (typed_load.go).
func TestTypedLoad_ObservesPeakHeap(t *testing.T) {
	withBudget(t, 64*mib, func(context.Context, string, goanalysis.LoadOpts) (*goanalysis.LoadResult, error) {
		return &goanalysis.LoadResult{}, nil
	})
	count := func() uint64 {
		mfs, err := prometheus.DefaultGatherer.Gather()
		require.NoError(t, err)
		for _, mf := range mfs {
			if mf.GetName() == "gocode_callgraph_gotypes_load_peak_heap_bytes" {
				return mf.GetMetric()[0].GetHistogram().GetSampleCount()
			}
		}
		return 0
	}
	before := count()
	_, rel, err := loadTypedShared(context.Background(), goModDir(t), goanalysis.LoadOpts{})
	require.NoError(t, err)
	rel()
	assert.Equal(t, before+1, count(), "one observation per shared load")

	stop := trackHeapPeak()
	assert.Positive(t, stop(), "the sampler must read a nonzero heap")
}
