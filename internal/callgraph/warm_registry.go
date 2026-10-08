package callgraph

import (
	"sync"
	"time"
)

// WarmState is the lifecycle phase of go/types enrichment for one repo
// root (issue #746). It replaces the former CallGraph.Warming bool, which
// conflated three situations behind one bit — "a warm is running",
// "a warm finished successfully but never cleared the flag", and "a warm
// failed permanently" — with opposite retry semantics (issues #735 round 3,
// #738).
//
// A cached CallGraph carries the state it was BUILT under as a stamp;
// the per-root registry below is the live authority. Stamps are write-once:
// nobody mutates a cached graph — on divergence the read path returns a
// shallow copy carrying the live state.
type WarmState string

const (
	// WarmNone means no warm is pending: the graph is either enhanced or
	// basic-final (the sync load succeeded with zero typed call edges —
	// nothing a retry could add).
	WarmNone WarmState = ""
	// WarmPending means a background go/types load is in flight for this
	// root; a retry will return the enhanced tier once it lands.
	WarmPending WarmState = "warming"
	// WarmDone means a warm completed successfully. It never appears on a
	// CallGraph stamp — it exists only in the registry, where its `at`
	// timestamp drives the sibling-stale divert in BuildFromRepo.
	WarmDone WarmState = "done"
	// WarmFailed means the last warm failed durably (unbuildable deps, no
	// network, unloadable module). A retry cannot change the answer until
	// the record expires — the honest note, not "retry".
	WarmFailed WarmState = "failed"
)

// WarmCause names which wait degraded the load behind a WarmPending stamp
// (issue #894): still priming the module's export data, or still queued for
// the typed-load memory budget. Empty when the load failure was neither wait
// (a plain post-admission deadline, an L2-imported stamp) — the note then
// stays generic rather than guessing.
type WarmCause string

const (
	// WarmCausePriming means the give-up happened while the module's
	// export-data build was queued for the build gate or running
	// (`go list -export` on a cold GOCACHE). Not a memory problem — the
	// note must not say "memory budget".
	WarmCausePriming WarmCause = "priming"
	// WarmCauseBudget means the give-up happened while the load was queued
	// for the process-wide typed-load memory budget — a real capacity
	// wait, so the note names the budget.
	WarmCauseBudget WarmCause = "budget"
)

// WarmNote renders the agent-facing note for a warm state — the single
// place the sentences live, so every tool surfaces the same wording
// and the failed state cannot silently reuse the "retry" text. For
// WarmPending the cause picks which wait is described (issue #894): the
// export-data build still running, or the memory budget busy — each says
// the real reason and that a retry lands once it clears.
func WarmNote(s WarmState, cause WarmCause) string {
	switch s {
	case WarmPending:
		switch cause {
		case WarmCausePriming:
			return "type-aware enrichment is warming in the background: the repo's export data is still being built (cold GOCACHE prime, not a memory problem); retry for the enhanced tier (go/types interface dispatch resolution)"
		case WarmCauseBudget:
			return "type-aware enrichment is warming in the background: the typed-load memory budget is busy with other module loads; retry for the enhanced tier (go/types interface dispatch resolution)"
		default:
			return "type-aware enrichment is warming in the background; retry for the enhanced tier (go/types interface dispatch resolution)"
		}
	case WarmFailed:
		return "type-aware enrichment is unavailable for this repo (go/types load failed); the tree-sitter tier shown is final for the warm backoff window"
	default:
		return ""
	}
}

// warmRecord is the single per-root lifecycle record. Exactly one writer
// transitions each record (the warm goroutine that claimed it, or the
// synchronous path stamping a terminal state), so no mutex is needed —
// the map's atomic pointer swaps are the whole protocol.
type warmRecord struct {
	phase WarmState
	at    time.Time // transition instant
	err   string    // WarmFailed only: the load error, for logs/operators
}

// goTypesWarm is the warm lifecycle registry: one record per repo root.
// It replaces the former goTypesWarmingSet (single-flight) +
// goTypesWarmedSet (completion instant) pair with one authority — the
// pending phase IS the single-flight claim, and done/failed `at` drive the
// sibling-stale divert and the failure backoff respectively.
//
// Failed records are self-expiring: warmStatus reports WarmNone once a
// failure is older than cgCacheTTL, letting the next cold call re-attempt
// (transient failures heal; durable ones retry once per TTL at most).
var goTypesWarm sync.Map // root → *warmRecord

// warmStatus returns the live warm phase for root plus the transition
// instant. Expired failed records report WarmNone — the caller may treat
// them as absent and re-claim.
func warmStatus(root string) (WarmState, time.Time) {
	v, ok := goTypesWarm.Load(root)
	if !ok {
		return WarmNone, time.Time{}
	}
	rec := v.(*warmRecord)
	if rec.phase == WarmFailed && time.Since(rec.at) >= cgCacheTTL {
		return WarmNone, time.Time{}
	}
	return rec.phase, rec.at
}

// claimWarm attempts to transition root into WarmPending. It returns false
// when a warm is already in flight (single-flight) or when a failed record
// is still fresh (backoff — an immediate re-warm would hit the same
// failure). done and expired-failed records are replaceable: a sync load
// failing after a successful warm, or a failure past its TTL, both earn a
// fresh attempt.
func claimWarm(root string) bool {
	rec := &warmRecord{phase: WarmPending, at: time.Now()}
	for {
		old, loaded := goTypesWarm.LoadOrStore(root, rec)
		if !loaded {
			return true
		}
		o := old.(*warmRecord)
		if o.phase == WarmPending || (o.phase == WarmFailed && time.Since(o.at) < cgCacheTTL) {
			return false
		}
		if goTypesWarm.CompareAndSwap(root, old, rec) {
			return true
		}
	}
}

// setWarmDone records a successful warm/load for root. The timestamp drives
// the sibling-stale divert: cache entries stamped pending BEFORE this
// instant predate the warm and are rebuilt; entries stamped after are
// already the post-warm truth and are not diverted again (loop guard).
func setWarmDone(root string) {
	goTypesWarm.Store(root, &warmRecord{phase: WarmDone, at: time.Now()})
}

// setWarmFailed records a durable warm failure. Consumers hitting a cache
// entry stamped pending reconcile it to WarmFailed via the read path, so
// the "retry will help" note stops lying for the full TTL (issue #738.1).
func setWarmFailed(root string, err error) {
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	goTypesWarm.Store(root, &warmRecord{phase: WarmFailed, at: time.Now(), err: errStr})
}

// cloneWithWarm returns a shallow copy of cg carrying state st. This is the
// ONLY way a cached graph's warm stamp ever changes after set(): the copy
// shares the read-only slices (Edges, Symbols, TypeRels — safe for
// concurrent readers) and costs one struct allocation per call, paid only
// on state divergence, never on the happy path.
func cloneWithWarm(cg *CallGraph, st WarmState) *CallGraph {
	cp := *cg
	cp.Warm = st
	return &cp
}
