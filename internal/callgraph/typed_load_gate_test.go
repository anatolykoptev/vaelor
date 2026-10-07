package callgraph

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
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

// An export-data prime is skipped once the module state was primed.
//
// Mutation that must turn it RED: delete `primed.Store(key, struct{}{})`
// (primeExportData, typed_load.go).
func TestPrimeExportData_Memoised(t *testing.T) {
	var primes atomic.Int32
	withBudget(t, 2048*mib, func(context.Context, string, goanalysis.LoadOpts) (*goanalysis.LoadResult, error) {
		return &goanalysis.LoadResult{}, nil
	})
	primeFn.Store(&primer{prime: func(context.Context, string) ([]string, error) {
		primes.Add(1)
		return nil, nil
	}})
	dir := oneRealPackage(t)
	for range 2 {
		_, rel, err := loadTypedShared(context.Background(), dir, goanalysis.LoadOpts{})
		require.NoError(t, err)
		rel()
	}
	assert.EqualValues(t, 1, primes.Load(), "an unchanged module is primed once")
}

// blockingGo puts a `go` on PATH whose `list -deps` build blocks until
// release() is called (and records that it started); every other go invocation
// goes to the real toolchain. It stands in for a long cold export build holding
// the build gate.
func blockingGo(t *testing.T) (started func() bool, release func()) {
	t.Helper()
	dir := t.TempDir()
	startedFile, releaseFile := filepath.Join(dir, "started"), filepath.Join(dir, "release")
	realGo, err := exec.LookPath("go")
	require.NoError(t, err)
	script := "#!/bin/sh\ncase \" $* \" in *\" -deps \"*) touch " + startedFile +
		"; while [ ! -e " + releaseFile + " ]; do sleep 0.05; done; exit 0;; esac\nexec " + realGo + " \"$@\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go"), []byte(script), 0o700)) //nolint:gosec // test helper script must be executable
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var once sync.Once
	rel := func() { once.Do(func() { _ = os.WriteFile(releaseFile, nil, 0o600) }) }
	t.Cleanup(rel)
	return func() bool { _, err := os.Stat(startedFile); return err == nil }, rel
}

// A cold build is charged to the budget, inside the build gate and only while it
// runs; a request that gives up while it is still building is a prime_wait, not
// budget exhaustion.
//
// Mutation that must turn it RED: in primeCharged (typed_load.go) pass nil
// instead of buildCharge (charge never taken); in degradeReason return
// "budget_wait" unconditionally (reason).
func TestPrimeExportData_ChargedWhileBuilding_PrimeWaitIsNotBudgetWait(t *testing.T) {
	withBudget(t, 2048*mib, func(context.Context, string, goanalysis.LoadOpts) (*goanalysis.LoadResult, error) {
		return &goanalysis.LoadResult{}, nil
	})
	primeFn.Store(&primer{prime: primeCharged})
	started, release := blockingGo(t)
	dir := oneRealPackage(t)

	reason := func(r string) float64 {
		return gatherCounterSum(t, "gocode_callgraph_gotypes_load_degraded_total", map[string]string{"reason": r})
	}
	b0, p0 := reason("budget_wait"), reason("prime_wait")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _, err := loadTypedShared(ctx, dir, goanalysis.LoadOpts{})
	require.Error(t, err, "the request gives up while the cold build runs")
	require.True(t, started(), "the build was running when the request gave up")
	assert.EqualValues(t, p0+1, reason("prime_wait"), "giving up mid-build is a prime_wait")
	assert.EqualValues(t, b0, reason("budget_wait"), "…and is not counted as budget exhaustion")

	assert.False(t, budgetFree(), "a running cold build is charged to the budget")
	assert.EqualValues(t, primeReserveBytes, int64(gaugeValue(t, "gocode_callgraph_gotypes_budget_in_use_bytes")), "exactly the build charge is in use")
	release()
	eventually(t, budgetFree, "the charge is returned when the build ends")
}

// The budget must not be parked behind the build gate. With the gate held by one
// long build (1 GiB charged inside it), a second module's prime waits at the gate
// holding NOTHING, so a third, already-primed module whose load fits is admitted
// at once — instead of FIFO-queuing behind a reservation that cannot be granted.
//
// Mutation that must turn it RED: in primeExportData (typed_load.go) take
// typedBudget.Load().sem.Acquire(ctx, primeReserveBytes) before calling prime.
func TestPrimeQueue_DoesNotParkBudgetBehindTheGate(t *testing.T) {
	withBudget(t, 1536*mib, func(context.Context, string, goanalysis.LoadOpts) (*goanalysis.LoadResult, error) {
		return &goanalysis.LoadResult{}, nil
	})
	primeFn.Store(&primer{prime: primeCharged})
	started, release := blockingGo(t)
	a, b, c := oneRealPackage(t), oneRealPackage(t), oneRealPackage(t)
	primed.Store(c+"|"+scanModule(c).fingerprint, struct{}{})

	var wg sync.WaitGroup
	for _, dir := range []string{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, rel, err := loadTypedShared(context.Background(), dir, goanalysis.LoadOpts{})
			assert.NoError(t, err)
			if rel != nil {
				rel()
			}
		}()
		if dir == a {
			eventually(t, started, "module A's build must hold the gate before B queues")
		}
	}
	time.Sleep(200 * time.Millisecond) // B is now waiting at the gate

	budgetWaits := func() float64 {
		return gatherCounterSum(t, "gocode_callgraph_gotypes_load_degraded_total", nil)
	}
	d0 := budgetWaits()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, rel, err := loadTypedShared(ctx, c, goanalysis.LoadOpts{})
	require.NoError(t, err, "a load that fits (64 MiB free of 512 MiB) must be admitted while a build holds the gate")
	rel()
	assert.EqualValues(t, d0, budgetWaits(), "and nothing is counted as degraded")

	release()
	wg.Wait()
}

// The boot-time prewarm goes through the same charged, gated build as a request:
// while it builds, its charge is visible to admission.
//
// Mutation that must turn it RED: in primeCharged pass nil instead of buildCharge.
func TestEagerPrewarm_IsChargedLikeARequestPrime(t *testing.T) {
	withBudget(t, 2048*mib, func(context.Context, string, goanalysis.LoadOpts) (*goanalysis.LoadResult, error) {
		return &goanalysis.LoadResult{}, nil
	})
	started, release := blockingGo(t)
	dir := oneRealPackage(t)

	done := make(chan error, 1)
	go func() {
		_, err := runGoListPrewarm(context.Background(), dir, "-mod=mod")
		done <- err
	}()
	eventually(t, started, "the prewarm build must start")
	assert.False(t, budgetFree(), "a running prewarm build is charged to the budget")
	release()
	require.NoError(t, <-done)
	eventually(t, budgetFree, "the prewarm's charge is returned")
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
