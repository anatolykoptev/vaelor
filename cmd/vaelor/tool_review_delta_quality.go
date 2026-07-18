package main

import (
	"context"
	"time"

	"github.com/anatolykoptev/vaelor/internal/compare"
	"github.com/anatolykoptev/vaelor/internal/oxcodes"
)

// xmlQualitySignals captures current-HEAD quality indicators reported alongside
// a delta review. These are NOT delta-vs-base — they reflect the post-change state,
// so reviewers can spot regressions at a glance.
type xmlQualitySignals struct {
	Freshness    *xmlFreshnessSignal    `xml:"freshness,omitempty"`
	Dataflow     *xmlDataflowSignal     `xml:"dataflow,omitempty"`
	AntiPatterns *xmlAntiPatternSignals `xml:"antiPatterns,omitempty"`
}

type xmlFreshnessSignal struct {
	FreshRatio float64 `xml:"freshRatio,attr"`
	VulnCount  int     `xml:"vulnCount,attr"`
	TotalDeps  int     `xml:"totalDeps,attr"`
}

type xmlDataflowSignal struct {
	DeadStores int `xml:"deadStores,attr"`
	UnusedVars int `xml:"unusedVars,attr"`
}

// qualityProbeTimeout bounds each quality probe (freshness, dataflow) so a
// slow OSV lookup or ox-codes scan cannot stall a delta review.
const qualityProbeTimeout = 10 * time.Second

// collectQualitySignals gathers optional quality probes for a delta review.
// Returns nil when no probe produced data so xml:"omitempty" suppresses the
// whole <quality> element instead of emitting an empty one.
func collectQualitySignals(ctx context.Context, root, language string, oxCodes *oxcodes.Client) *xmlQualitySignals {
	out := &xmlQualitySignals{}

	// Freshness — non-fatal. Cancel explicitly before the dataflow call so
	// the two timeout timers never overlap.
	fctx, fcancel := context.WithTimeout(ctx, qualityProbeTimeout)
	if fresh, _, _ := compare.CollectFreshness(fctx, root); fresh != nil {
		out.Freshness = &xmlFreshnessSignal{
			FreshRatio: fresh.DepFreshnessRatio,
			VulnCount:  fresh.VulnDeps,
			TotalDeps:  fresh.TotalDeps,
		}
	}
	fcancel()

	// Dataflow — requires ox-codes + language.
	if oxCodes != nil && language != "" {
		dctx, dcancel := context.WithTimeout(ctx, qualityProbeTimeout)
		if df := compare.GatherDataflow(dctx, oxCodes, root, language); df != nil {
			out.Dataflow = &xmlDataflowSignal{
				DeadStores: df.DeadStores,
				UnusedVars: df.UnusedVars,
			}
		}
		dcancel()
	}

	// Structural anti-patterns — requires ox-codes + language.
	if oxCodes != nil && language != "" {
		apCtx, apCancel := context.WithTimeout(ctx, antiPatternProbeTimeout)
		if ap := collectAntiPatterns(apCtx, root, language, oxCodes); ap != nil {
			out.AntiPatterns = ap
		}
		apCancel()
	}

	if out.Freshness == nil && out.Dataflow == nil && out.AntiPatterns == nil {
		return nil
	}
	return out
}
