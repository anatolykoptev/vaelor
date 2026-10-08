package callgraph

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/metrics"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
	"github.com/anatolykoptev/vaelor/internal/memlimit"
)

// The typed (go/packages) load is the memory spike of the server: tens to
// hundreds of MB per module even with dependencies read from export data, and
// it used to run once per request, concurrently, with a second copy built by the
// background warm of the very request whose 10 s probe had just given up on the
// first (issues #747, #868 round 3). Everything here exists so that:
//
//   - concurrent requests for one module share ONE load (loadTypedShared), and
//     a probe that gave up and the warm that follows it join the same load
//     instead of building a second arena;
//   - loads of different modules are admitted against a process-wide memory
//     budget (typedBudget), a waiter degrading to the basic tier at its own
//     deadline instead of adding an arena the process cannot hold;
//   - the budget is held until the LAST holder has finished reading the
//     result, because the arena is live until then, not until the load returns.

const (
	// typedLoadTimeout bounds one shared load, including the wait for budget.
	// It is the patient background-warm budget; the request-path probe only
	// stops WAITING at its own 10 s (syncLoadBudget), it does not cancel the load.
	typedLoadTimeout = 15 * time.Minute

	// bytesPerGoFile is the budget cost of one Go file in the module. Measured
	// peak RSS with dependencies from export data and test variants loaded:
	// 0.28 MB/file on v1.65.16 (1.1k files, 311 MB) and 0.78 MB/file on a 172
	// package module (1.1k files, 0.9 GB). 1 MiB is the conservative round-up.
	bytesPerGoFile = int64(1) << 20
	// minLoadWeight keeps a tiny module from costing nothing.
	minLoadWeight = int64(64) << 20

	// budgetShareOfLimit is the share of the container memory limit that
	// concurrent typed loads may account for.
	budgetShareOfLimit = 2 // 1/2
	// defaultBudgetBytes applies when no memory limit is detectable.
	defaultBudgetBytes = int64(2) << 30

	// EnvTypedLoadBudgetMB overrides the typed-load memory budget.
	EnvTypedLoadBudgetMB = "VAELOR_TYPED_LOAD_BUDGET_MB"

	heapSampleInterval = 50 * time.Millisecond
	heapMetric         = "/memory/classes/heap/objects:bytes"
)

// errTypedBudget is returned to a caller whose deadline expired while the load
// was still queued for budget (as opposed to loading). It wraps the context
// error so deadline checks keep working.
// Where a load is: building export data (waiting for the build gate or running
// the build), queued for its heap budget, or running.
const (
	phasePriming int32 = iota
	phaseQueued
	phaseAdmitted
)

// degradeReason names why a caller that gave up on f did: waiting on the memory
// budget itself, or still priming (waiting for / running the export-data build).
// Keeping them apart stops a cold cache from reading as budget exhaustion.
func (f *sharedLoad) degradeReason() string {
	if f.phase.Load() == phasePriming {
		return "prime_wait"
	}
	return "budget_wait"
}

// degradeErr is the error text matching degradeReason's phase: a give-up while
// the export-data build is still queued or running says so, not "memory budget
// exhausted" — the same wording for both sends operators hunting a memory
// problem that does not exist (issue #894). Both sentinels satisfy
// isTypedLoadDegraded.
func (f *sharedLoad) degradeErr() error {
	if f.phase.Load() == phasePriming {
		return errTypedPriming
	}
	return errTypedBudget
}

// errTypedBudget is returned to a caller whose deadline expired while the load
// was still queued for budget (as opposed to loading or priming). It wraps the
// context error so deadline checks keep working.
var errTypedBudget = errors.New("typed load not admitted within the deadline (memory budget exhausted)")

// errTypedPriming is the same give-up while the load was still in the prime
// phase — queued for the export-data build gate or running `go list -export`.
// The build keeps running detached, so a retry lands on a warm cache.
var errTypedPriming = errors.New("typed load not ready within the deadline (export data is still being built)")

// isTypedLoadDegraded reports whether err is either not-admitted sentinel —
// budget wait or prime wait. Both are counted under
// gocode_callgraph_gotypes_load_degraded_total, never under the generic
// go/types fallback counter.
func isTypedLoadDegraded(err error) bool {
	return errors.Is(err, errTypedBudget) || errors.Is(err, errTypedPriming)
}

// warmCauseOf maps a load error to the WarmCause stamped on the degraded graph,
// so the WarmPending note can say which wait it was and what to do. A load
// failure that was neither wait (e.g. a plain post-admission deadline) carries
// no cause and keeps the generic pending note.
func warmCauseOf(err error) WarmCause {
	switch {
	case errors.Is(err, errTypedPriming):
		return WarmCausePriming
	case errors.Is(err, errTypedBudget):
		return WarmCauseBudget
	default:
		return ""
	}
}

// typedLoadFn is the raw loader the shared load runs and typedBudget the
// process-wide admission budget. Detached loads outlive the request (and the
// test) that started them and read these concurrently with a test swapping them,
// hence atomic pointers.
var (
	typedLoadFn atomic.Pointer[func(context.Context, string, goanalysis.LoadOpts) (*goanalysis.LoadResult, error)]
	typedBudget atomic.Pointer[budget]
)

func init() {
	fn := goanalysis.LoadPackages
	typedLoadFn.Store(&fn)
	typedBudget.Store(newBudget(detectBudgetBytes()))
}

type budget struct {
	capacity int64
	sem      *semaphore.Weighted
}

func newBudget(capacity int64) *budget {
	return &budget{capacity: capacity, sem: semaphore.NewWeighted(capacity)}
}

// detectBudgetBytes derives the budget: explicit env, else half the container
// memory limit, else 2 GiB.
func detectBudgetBytes() int64 {
	return budgetFrom(os.Getenv(EnvTypedLoadBudgetMB), func() int64 { l, _ := memlimit.Detect(); return l })
}

func budgetFrom(envMB string, limit func() int64) int64 {
	if envMB != "" {
		if n, err := memlimit.ParseSize(envMB + "MiB"); err == nil && n > 0 {
			return n
		}
		slog.Warn("callgraph: ignoring unparsable "+EnvTypedLoadBudgetMB, "value", envMB)
	}
	if l := limit(); l > 0 {
		return l / budgetShareOfLimit
	}
	return defaultBudgetBytes
}

// weight is what a load of root costs: its Go file count times bytesPerGoFile,
// clamped to [minLoadWeight, capacity] so an oversize module still runs, alone.
func (b *budget) weight(files int) int64 {
	w := int64(files) * bytesPerGoFile
	if w < minLoadWeight {
		w = minLoadWeight
	}
	if w > b.capacity {
		w = b.capacity
	}
	return w
}

// moduleScan is what one walk of a module tells the typed load: how many Go
// files it has (the budget weight) and a fingerprint of their identity.
type moduleScan struct {
	files       int
	fingerprint string
}

// scanModule walks root once, skipping directories the typed load never matches
// (vendor, VCS, dependency caches). The fingerprint hashes every .go file's
// path, size and mtime plus go.mod/go.sum, so it changes when the code the load
// would read changes — it is part of the flight key, so a request made after a
// `git pull` never joins a load that started before it.
func scanModule(root string) moduleScan {
	h := fnv.New64a()
	n := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // best-effort; unreadable subtrees count as empty
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		isGo := strings.HasSuffix(name, ".go")
		if !isGo && name != "go.mod" && name != "go.sum" {
			return nil
		}
		if isGo {
			n++
		}
		if info, ierr := d.Info(); ierr == nil {
			rel, _ := filepath.Rel(root, path)
			fmt.Fprintf(h, "%s|%d|%d\n", rel, info.Size(), info.ModTime().UnixNano())
		}
		return nil
	})
	return moduleScan{files: n, fingerprint: strconv.FormatUint(h.Sum64(), 36)}
}

// sharedLoad is one in-flight or completed typed load, shared by every caller
// that asked for the same module while it was alive.
type sharedLoad struct {
	key  string
	done chan struct{} // closed when lr/err/panicVal are set

	lr       *goanalysis.LoadResult
	err      error
	panicVal any

	// guarded by flightsMu
	refs     int
	finished bool

	phase    atomic.Int32        // phasePriming -> phaseQueued -> phaseAdmitted
	admitted atomic.Bool         // budget acquired (the load is actually running)
	weight   int64               // held budget, released when the last ref drops
	sem      *semaphore.Weighted // the semaphore the budget was taken from
}

var (
	flightsMu sync.Mutex
	flights   = map[string]*sharedLoad{}
)

// loadTypedShared returns the typed load of root, starting it only if no live
// load of the same module exists, together with a release func the caller MUST
// call once it stops reading the result. The load keeps running if every
// caller gives up (a probe that timed out); the next caller — the background
// warm — joins it instead of building a second arena.
//
// A caller whose ctx expires first gets ctx's error; if the load had not yet
// been admitted by the memory budget the error is f.degradeErr() — priming or
// budget wording matching the phase it gave up in (issue #894).
func loadTypedShared(ctx context.Context, root string, opts goanalysis.LoadOpts) (*goanalysis.LoadResult, func(), error) {
	scan := scanModule(root)
	key := fmt.Sprintf("%s|%s|tests=%t|src=%t", root, scan.fingerprint, opts.Tests, opts.SourceDeps)

	flightsMu.Lock()
	f := flights[key]
	if f == nil {
		f = &sharedLoad{key: key, done: make(chan struct{})}
		flights[key] = f
		go f.run(root, opts, scan)
	}
	f.refs++
	flightsMu.Unlock()

	select {
	case <-f.done:
	case <-ctx.Done():
		select {
		case <-f.done: // finished at the same instant: the result wins over the deadline
		default:
			f.drop()
			if !f.admitted.Load() {
				recordTypedLoadDegraded(f.degradeReason())
				return nil, nil, fmt.Errorf("%w: %w", f.degradeErr(), ctx.Err())
			}
			return nil, nil, ctx.Err()
		}
	}
	if f.panicVal != nil {
		f.drop()
		panic(f.panicVal)
	}
	if f.err != nil {
		f.drop()
		if isTypedLoadDegraded(f.err) {
			recordTypedLoadDegraded(f.degradeReason())
		}
		return nil, nil, f.err
	}
	var once sync.Once
	return f.lr, func() { once.Do(f.drop) }, nil
}

// drop releases one reference. When the last one goes and the load has
// finished, the result is unreachable and its budget is returned; when the load
// is still running its own completion does that.
func (f *sharedLoad) drop() {
	flightsMu.Lock()
	f.refs--
	last := f.refs == 0 && f.finished
	if last && flights[f.key] == f {
		delete(flights, f.key)
	}
	flightsMu.Unlock()
	if last {
		f.releaseBudget()
	}
}

func (f *sharedLoad) releaseBudget() {
	if f.admitted.Swap(false) {
		budgetInUse.Sub(float64(f.weight))
		f.sem.Release(f.weight)
	}
}

func (f *sharedLoad) run(root string, opts goanalysis.LoadOpts, scan moduleScan) {
	defer func() {
		if r := recover(); r != nil {
			f.panicVal = r
		}
		flightsMu.Lock()
		f.finished = true
		orphan := f.refs == 0
		if orphan && flights[f.key] == f {
			delete(flights, f.key)
		}
		flightsMu.Unlock()
		if orphan { // nobody will ever read the result: return its budget now
			f.lr = nil
			f.releaseBudget()
		}
		close(f.done)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), typedLoadTimeout)
	defer cancel()

	if err := primeExportData(ctx, root, scan); err != nil {
		f.err = fmt.Errorf("%w: %w", errTypedPriming, err)
		return
	}

	b := typedBudget.Load()
	f.phase.Store(phaseQueued)
	f.sem = b.sem
	f.weight = b.weight(scan.files)
	loadsQueued.Inc()
	err := b.sem.Acquire(ctx, f.weight)
	loadsQueued.Dec()
	if err != nil {
		f.err = fmt.Errorf("%w: %w", errTypedBudget, err)
		return
	}
	budgetInUse.Add(float64(f.weight))
	f.phase.Store(phaseAdmitted)
	f.admitted.Store(true)

	stop := trackHeapPeak()
	f.lr, f.err = (*typedLoadFn.Load())(ctx, root, opts)
	observeTypedLoadPeak(stop())
}

// primeReserveBytes is what a cold export-data build is charged against the
// budget while it runs. `go list -export` compiles in child processes
// (cmd/compile, cgo) that live in the server's cgroup but outside the Go heap,
// so neither GOMEMLIMIT nor the heap weight of the load sees them; measured on
// v1.65.16 they reached ~1.5 GB above the server's own RSS at -p=NumCPU. With
// -p capped at NumCPU/2 and one build at a time (goanalysis.exportGate) 1 GiB is
// the charge; it is clamped to the budget so a small budget still admits one.
const primeReserveBytes = int64(1) << 30

// primed remembers modules whose export data this process already built, keyed
// by root+fingerprint, so only the first load of a module (or the first after it
// changed) pays for the extra `go list`.
var primed sync.Map

// primeExportData builds dir's export data before the load, so the load itself
// only reads it: cold builds are then serialised by the gate and charged to the
// budget, instead of happening inside go/packages where nothing bounds them.
// A build failure is not an error — go/packages falls back to source for
// whatever has no export data — only not being admitted in time is.
func primeExportData(ctx context.Context, root string, scan moduleScan) error {
	key := root + "|" + scan.fingerprint
	if _, ok := primed.Load(key); ok {
		return nil
	}
	// No budget is taken here: the build charges itself inside the gate (see
	// buildCharge). Reserving first would park a gibibyte per queued module
	// behind whatever build holds the gate.
	errored, err := primeFn.Load().prime(ctx, root)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		slog.Warn("go/types: export-data priming failed; the load will build what it needs itself",
			"root", root, "err", err)
		return nil
	}
	if len(errored) > 0 {
		slog.Debug("go/types: packages without export data (type-checked from source)",
			"root", root, "count", len(errored))
	}
	primed.Store(key, struct{}{})
	return nil
}

// buildCharge charges a running export-data build (primeReserveBytes, clamped to
// the budget) against the typed-load budget. goanalysis.PrimeExportData calls it
// with the build gate already held, for the build's duration only. The eager
// prewarm and the request-path prime both go through primeCharged, so the two
// cannot drift: a boot-time build is as visible to admission as a request's.
func buildCharge(ctx context.Context) (func(), error) {
	b := typedBudget.Load()
	w := min(primeReserveBytes, b.capacity)
	if err := b.sem.Acquire(ctx, w); err != nil {
		return nil, err
	}
	budgetInUse.Add(float64(w))
	return func() {
		budgetInUse.Sub(float64(w))
		b.sem.Release(w)
	}, nil
}

// primeCharged is the one way export data is built: one build at a time,
// charged against the budget while it runs.
func primeCharged(ctx context.Context, root string) ([]string, error) {
	return goanalysis.PrimeExportData(ctx, root, buildCharge)
}

// primer is the seam over goanalysis.PrimeExportData.
type primer struct {
	prime func(ctx context.Context, root string) ([]string, error)
}

var primeFn atomic.Pointer[primer]

func init() {
	primeFn.Store(&primer{prime: primeCharged})
}

// trackHeapPeak samples the process heap until the returned func is called and
// reports the peak in bytes. runtime/metrics reads are cheap and do not stop
// the world, unlike runtime.ReadMemStats.
func trackHeapPeak() (stop func() uint64) {
	var peak atomic.Uint64
	quit := make(chan struct{})
	finished := make(chan struct{})
	sample := []metrics.Sample{{Name: heapMetric}}
	read := func() {
		metrics.Read(sample)
		if sample[0].Value.Kind() == metrics.KindUint64 {
			if v := sample[0].Value.Uint64(); v > peak.Load() {
				peak.Store(v)
			}
		}
	}
	read()
	go func() {
		defer close(finished)
		t := time.NewTicker(heapSampleInterval)
		defer t.Stop()
		for {
			select {
			case <-quit:
				read()
				return
			case <-t.C:
				read()
			}
		}
	}()
	return func() uint64 {
		close(quit)
		<-finished
		return peak.Load()
	}
}
