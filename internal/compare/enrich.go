package compare

import (
	"context"
	"sync"
	"time"

	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/oxcodes"
)

// qualityTimeout limits how long enrichment checks (quality, freshness, dataflow) can run.
// All enrichment is informational — must never block core comparison.
const qualityTimeout = 7 * time.Second

// enrichResult holds all optional, non-fatal enrichment data gathered in parallel.
type enrichResult struct {
	qualityA, qualityB     *QualityIndicators
	freshnessA, freshnessB *FreshnessStats
	dataflowA, dataflowB   *DataflowStats
	couplingA, couplingB   []CoupledPair
	archMetricsA           *ArchMetrics
	archMetricsB           *ArchMetrics
}

// enrichInput bundles the inputs needed for enrichment.
type enrichInput struct {
	rootA, rootB string
	langA, langB string
	oxCodes      *oxcodes.Client
	graphStore   *codegraph.Store
}

// collectEnrichment runs all optional enrichment passes in parallel and returns
// the aggregated results. Failures are non-fatal — each goroutine silently skips.
//
// Two timeout tiers are used:
//   - qualityTimeout (7s): ox-codes + freshness + coupling
//   - 30s: architecture graph (multiple DB queries per repo)
func collectEnrichment(ctx context.Context, in enrichInput) enrichResult {
	var r enrichResult

	// --- Tier 1: quality + freshness + coupling ---
	{
		ectx, ecancel := context.WithTimeout(ctx, qualityTimeout)
		defer ecancel()

		var wg sync.WaitGroup

		if in.oxCodes != nil {
			wg.Add(4)
			go func() {
				defer wg.Done()
				r.qualityA = GatherQualityIndicators(ectx, in.oxCodes, in.rootA, in.langA)
			}()
			go func() {
				defer wg.Done()
				r.qualityB = GatherQualityIndicators(ectx, in.oxCodes, in.rootB, in.langB)
			}()
			go func() {
				defer wg.Done()
				r.dataflowA = GatherDataflow(ectx, in.oxCodes, in.rootA, in.langA)
			}()
			go func() {
				defer wg.Done()
				r.dataflowB = GatherDataflow(ectx, in.oxCodes, in.rootB, in.langB)
			}()
		}

		wg.Add(2)
		go func() {
			defer wg.Done()
			r.freshnessA, _, _ = CollectFreshness(ectx, in.rootA)
		}()
		go func() {
			defer wg.Done()
			r.freshnessB, _, _ = CollectFreshness(ectx, in.rootB)
		}()

		wg.Add(2)
		go func() { defer wg.Done(); r.couplingA = CollectCoupling(ectx, in.rootA, defaultMinCoChanges) }()
		go func() { defer wg.Done(); r.couplingB = CollectCoupling(ectx, in.rootB, defaultMinCoChanges) }()

		wg.Wait()
	}

	// --- Tier 2: architecture graph ---
	if in.graphStore != nil {
		gctx, gcancel := context.WithTimeout(ctx, 30*time.Second)
		defer gcancel()

		var gwg sync.WaitGroup
		gwg.Add(2)
		go func() {
			defer gwg.Done()
			r.archMetricsA = CollectArchMetrics(gctx, in.graphStore, in.rootA)
		}()
		go func() {
			defer gwg.Done()
			r.archMetricsB = CollectArchMetrics(gctx, in.graphStore, in.rootB)
		}()
		gwg.Wait()
	}

	return r
}
