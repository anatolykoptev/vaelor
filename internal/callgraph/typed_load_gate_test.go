package callgraph

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
)

func gaugeValue(t *testing.T, name string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() == name && len(mf.GetMetric()) > 0 {
			return mf.GetMetric()[0].GetGauge().GetValue()
		}
	}
	return 0
}

func realLoader(ctx context.Context, root string, opts goanalysis.LoadOpts) (*goanalysis.LoadResult, error) {
	return goanalysis.LoadPackages(ctx, root, opts)
}

// oneRealPackage is a module that really type-checks and has a typed call edge.
func oneRealPackage(t *testing.T) string {
	t.Helper()
	dir := goModDir(t)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n\nfunc helper() {}\n\nfunc main() { helper() }\n"), 0o600))
	return dir
}

// A panic in a consumer between the load and its end (the typed-edge conversion,
// the IMPLEMENTS pass) must still release the shared load: otherwise the budget
// is never returned, the arena stays pinned, and every later request for the
// module joins the finished flight and reads the same stale result.
//
// Mutation that must turn it RED: in enrichWithGoTypes (repo.go) delete the
// `defer release()`.
func TestEnrichWithGoTypes_ConsumerPanicReleasesTheLoad(t *testing.T) {
	// A package slice holding a nil makes the resolver dereference nil: the panic
	// shape of a consumer bug.
	withBudget(t, 64*mib, func(context.Context, string, goanalysis.LoadOpts) (*goanalysis.LoadResult, error) {
		return &goanalysis.LoadResult{Packages: []*packages.Package{nil}}, nil
	})
	dir := goModDir(t)

	func() {
		defer func() { assert.NotNil(t, recover(), "the consumer panic must propagate to the caller") }()
		enrichWithGoTypes(context.Background(), dir, &CallGraph{Tier: "basic"}, nil)
	}()

	flightsMu.Lock()
	live := len(flights)
	flightsMu.Unlock()
	assert.Zero(t, live, "no flight may outlive a panicking consumer")
	eventually(t, budgetFree, "the panicking consumer's budget must be returned")
}

// The request path must go THROUGH the budget: with the budget held by another
// module, a real, loadable module degrades to the basic tier with WarmPending,
// and the reason is the budget — shown by the degraded counter moving and the
// generic fallback counter not. With budget free the same module is enhanced, so
// the degrade above is not an artefact of an empty or unloadable module.
//
// Mutation that must turn it RED: in enrichWithGoTypes (repo.go) replace
// loadTypedShared(...) with a direct goanalysis.LoadPackages and a no-op release.
func TestEnrichWithTypedResolution_BudgetGatesTheRequestPath(t *testing.T) {
	withBudget(t, 64*mib, realLoader)
	real := oneRealPackage(t)

	holder := goModDir(t)
	hold := make(chan struct{})
	typedLoadFn.Store(ptr(func(ctx context.Context, root string, opts goanalysis.LoadOpts) (*goanalysis.LoadResult, error) {
		if root == holder {
			<-hold
			return &goanalysis.LoadResult{}, nil
		}
		return realLoader(ctx, root, opts)
	}))
	holderRel := make(chan func(), 1)
	go func() {
		_, rel, _ := loadTypedShared(context.Background(), holder, goanalysis.LoadOpts{})
		holderRel <- rel
	}()
	eventually(t, func() bool { return !budgetFree() }, "the other module must be holding the budget")

	old := syncLoadBudget
	syncLoadBudget = 150 * time.Millisecond
	defer func() { syncLoadBudget = old }()

	degraded := func() float64 {
		return gatherCounterSum(t, "gocode_callgraph_gotypes_load_degraded_total", map[string]string{"reason": "budget_wait"})
	}
	fallbacks := func() float64 { return gatherCounterSum(t, "gocode_callgraph_gotypes_fallback_total", nil) }
	d0, f0 := degraded(), fallbacks()

	cg := EnrichWithTypedResolution(context.Background(), real, &CallGraph{Tier: "basic"}, nil, nil)
	assert.Equal(t, "basic", cg.Tier)
	assert.Equal(t, WarmPending, cg.Warm)
	assert.EqualValues(t, d0+1, degraded(), "the request must have degraded because of the budget")
	assert.EqualValues(t, f0, fallbacks(), "a budget degrade must not also be counted as a generic load failure")

	// Control: budget free, same module, same call.
	close(hold)
	(<-holderRel)()
	eventually(t, budgetFree, "holder released")
	syncLoadBudget = 30 * time.Second
	ok := EnrichWithTypedResolution(context.Background(), real, &CallGraph{Tier: "basic", Backend: BackendTreeSitter}, nil, nil)
	assert.Equal(t, "enhanced", ok.Tier, "with budget available the same module loads (so the degrade above was the budget)")
}

// The background warm must reach the load through the shared seam: a probe that
// timed out is still loading, and the warm that follows joins it. Observed
// through goTypesLoadFn, the variable warmGoTypesCache really calls.
//
// Mutation that must turn it RED: replace `var goTypesLoadFn = loadTypedShared`
// (repo.go) with a func that calls (*typedLoadFn.Load())(ctx, root, opts) and
// returns a no-op release.
func TestWarmGoTypesCache_JoinsTheProbesLoad(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	withBudget(t, 1024*mib, func(context.Context, string, goanalysis.LoadOpts) (*goanalysis.LoadResult, error) {
		calls.Add(1)
		<-release
		return &goanalysis.LoadResult{}, nil
	})
	dir := goModDir(t)
	goTypesWarm.Delete(dir)

	probeCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := loadTypedShared(probeCtx, dir, goanalysis.LoadOpts{Tests: true})
	require.Error(t, err, "the probe gives up while the load runs")

	done := make(chan struct{})
	go func() {
		defer close(done)
		warmGoTypesCache(dir, nil, cgCacheKey(TraceRepoInput{Root: dir}))
	}()
	time.Sleep(100 * time.Millisecond)
	close(release)
	<-done

	assert.EqualValues(t, 1, calls.Load(), "the warm must join the probe's load, not start a second one")
}

// warmGoTypesCache must release the shared load when it is done with it.
//
// Mutation that must turn it RED: in warmGoTypesCache (repo.go) replace
// `defer release()` with `_ = release`.
func TestWarmGoTypesCache_ReleasesTheLoad(t *testing.T) {
	var released atomic.Int32
	old := goTypesLoadFn
	goTypesLoadFn = func(context.Context, string, goanalysis.LoadOpts) (*goanalysis.LoadResult, func(), error) {
		return &goanalysis.LoadResult{}, func() { released.Add(1) }, nil
	}
	defer func() { goTypesLoadFn = old }()
	dir := goModDir(t)
	goTypesWarm.Delete(dir)

	warmGoTypesCache(dir, nil, cgCacheKey(TraceRepoInput{Root: dir}))
	assert.EqualValues(t, 1, released.Load())
}

// A request after the module changed must not join a load that started (or
// finished but is still held) before the change.
//
// Mutation that must turn it RED: remove scan.fingerprint from the key in
// loadTypedShared.
func TestLoadTypedShared_ChangedModuleDoesNotJoinStaleLoad(t *testing.T) {
	var calls atomic.Int32
	withBudget(t, 4096*mib, func(context.Context, string, goanalysis.LoadOpts) (*goanalysis.LoadResult, error) {
		calls.Add(1)
		return &goanalysis.LoadResult{}, nil
	})
	dir := oneRealPackage(t)

	_, relA, err := loadTypedShared(context.Background(), dir, goanalysis.LoadOpts{})
	require.NoError(t, err)
	defer relA()

	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n\nfunc helper() {}\n\nfunc other() {}\n\nfunc main() { helper() }\n"), 0o600))

	_, relB, err := loadTypedShared(context.Background(), dir, goanalysis.LoadOpts{})
	require.NoError(t, err)
	defer relB()
	assert.EqualValues(t, 2, calls.Load(), "a changed module needs a fresh load even while the old result is held")

	_, relC, err := loadTypedShared(context.Background(), dir, goanalysis.LoadOpts{})
	require.NoError(t, err)
	defer relC()
	assert.EqualValues(t, 2, calls.Load(), "an unchanged module still shares")
}

// Cold export builds are charged to the budget and happen once per module state.
//
// Mutation that must turn it RED: delete `primed.Store(key, struct{}{})`
// (primeExportData, typed_load.go) for the memo; delete the b.sem.Acquire(ctx, w)
// there for the charge.
func TestPrimeExportData_ChargedToBudgetAndMemoised(t *testing.T) {
	var primes atomic.Int32
	inPrime := make(chan struct{})
	finish := make(chan struct{})
	withBudget(t, 2048*mib, func(context.Context, string, goanalysis.LoadOpts) (*goanalysis.LoadResult, error) {
		return &goanalysis.LoadResult{}, nil
	})
	primeFn.Store(&primer{prime: func(context.Context, string) ([]string, error) {
		if primes.Add(1) == 1 {
			close(inPrime)
			<-finish
		}
		return nil, nil
	}})
	dir := oneRealPackage(t)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, rel, err := loadTypedShared(context.Background(), dir, goanalysis.LoadOpts{})
		assert.NoError(t, err)
		rel()
	}()
	<-inPrime
	assert.False(t, budgetFree(), "a running cold build must be charged to the budget (its compile children are invisible to the heap)")
	close(finish)
	<-done

	_, rel, err := loadTypedShared(context.Background(), dir, goanalysis.LoadOpts{})
	require.NoError(t, err)
	rel()
	assert.EqualValues(t, 1, primes.Load(), "an unchanged module is primed once")
}

// The in-use gauge must return to baseline: a holder that never releases is
// otherwise visible only as requests degrading.
//
// Mutation that must turn it RED: delete the budgetInUse.Sub line in
// sharedLoad.releaseBudget.
func TestBudgetInUseGauge_ReturnsToBaseline(t *testing.T) {
	withBudget(t, 1024*mib, func(context.Context, string, goanalysis.LoadOpts) (*goanalysis.LoadResult, error) {
		return &goanalysis.LoadResult{}, nil
	})
	base := gaugeValue(t, "gocode_callgraph_gotypes_budget_in_use_bytes")
	_, rel, err := loadTypedShared(context.Background(), goModDir(t), goanalysis.LoadOpts{})
	require.NoError(t, err)
	assert.Greater(t, gaugeValue(t, "gocode_callgraph_gotypes_budget_in_use_bytes"), base, "a held load is visible")
	rel()
	eventually(t, func() bool { return gaugeValue(t, "gocode_callgraph_gotypes_budget_in_use_bytes") == base }, "released load leaves no residue")
}

func ptr[T any](v T) *T { return &v }
