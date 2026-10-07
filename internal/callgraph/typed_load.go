package callgraph

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/metrics"
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
var errTypedBudget = errors.New("typed load not admitted within the deadline (memory budget exhausted)")

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

// countGoFiles counts .go files under root, skipping directories the typed
// load never matches (vendor, VCS, dependency caches).
func countGoFiles(root string) int {
	n := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // best-effort estimate; unreadable subtrees count as empty
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "node_modules", "testdata":
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") {
			n++
		}
		return nil
	})
	return n
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
// been admitted by the memory budget the error is errTypedBudget.
func loadTypedShared(ctx context.Context, root string, opts goanalysis.LoadOpts) (*goanalysis.LoadResult, func(), error) {
	key := fmt.Sprintf("%s|tests=%t|src=%t", root, opts.Tests, opts.SourceDeps)

	flightsMu.Lock()
	f := flights[key]
	if f == nil {
		f = &sharedLoad{key: key, done: make(chan struct{})}
		flights[key] = f
		go f.run(root, opts)
	}
	f.refs++
	flightsMu.Unlock()

	select {
	case <-f.done:
	case <-ctx.Done():
		f.drop()
		if !f.admitted.Load() {
			recordTypedLoadDegraded("budget_wait")
			return nil, nil, fmt.Errorf("%w: %w", errTypedBudget, ctx.Err())
		}
		return nil, nil, ctx.Err()
	}
	if f.panicVal != nil {
		f.drop()
		panic(f.panicVal)
	}
	if f.err != nil {
		f.drop()
		if errors.Is(f.err, errTypedBudget) {
			recordTypedLoadDegraded("budget_wait")
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
		f.sem.Release(f.weight)
	}
}

func (f *sharedLoad) run(root string, opts goanalysis.LoadOpts) {
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

	b := typedBudget.Load()
	f.sem = b.sem
	f.weight = b.weight(countGoFiles(root))
	if err := b.sem.Acquire(ctx, f.weight); err != nil {
		f.err = fmt.Errorf("%w: %w", errTypedBudget, err)
		return
	}
	f.admitted.Store(true)

	stop := trackHeapPeak()
	f.lr, f.err = (*typedLoadFn.Load())(ctx, root, opts)
	observeTypedLoadPeak(stop())
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
