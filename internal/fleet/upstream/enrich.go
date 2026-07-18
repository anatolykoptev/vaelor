// internal/fleet/upstream/enrich.go
package upstream

import (
	"context"
	"log/slog"
	"sync"

	"github.com/anatolykoptev/vaelor/internal/fleet"
)

// enrichCacheKey is the dedup key for Compare calls.
type enrichCacheKey struct {
	slug string
	base string
	head string
}

// enrichCacheValue holds a completed Compare result.
type enrichCacheValue struct {
	cl *fleet.Changelog
}

// Enrich populates Changelog on each TagDrift diff in-place, using parallel
// goroutines. Skips non-TagDrift rows, rows where image doesn't map to a known
// upstream, and rows when ctx expires. All failures are soft (logged via
// slog.Debug, not propagated).
//
// maxEnrich caps the total number of diffs enriched (to bound API usage).
// Pass 30 for the default. Passing 0 is a no-op (returns diffs unchanged).
//
// Deduplication: if multiple TagDrift rows share the same (upstream slug, base
// tag, head tag), the Compare API is called only once and the result is shared
// across all matching rows.
func Enrich(ctx context.Context, client *Client, diffs []fleet.ImageDiff, maxEnrich int) []fleet.ImageDiff {
	if maxEnrich <= 0 || len(diffs) == 0 {
		return diffs
	}

	// Step 1: Walk diffs, collect eligible indices and build the dedup plan.
	// eligible[i] = index into diffs slice; enrichedCount tracks the cap.
	type enrichWork struct {
		diffIdx int    // index in diffs
		slug    string // GitHub slug
		base    string // pinned tag
		head    string // runtime tag
		key     enrichCacheKey
	}

	works := make([]enrichWork, 0, len(diffs))
	for i, d := range diffs {
		if d.Status != fleet.DiffTagDrift {
			continue
		}
		if d.Pinned == nil || d.Runtime == nil {
			continue
		}
		slug, ok := Resolve(d.Image)
		if !ok {
			slog.Debug("upstream enrich: image not mapped, skipping", "image", d.Image)
			continue
		}
		works = append(works, enrichWork{
			diffIdx: i,
			slug:    slug,
			base:    d.Pinned.Tag,
			head:    d.Runtime.Tag,
			key:     enrichCacheKey{slug: slug, base: d.Pinned.Tag, head: d.Runtime.Tag},
		})
		if len(works) >= maxEnrich {
			break
		}
	}

	if len(works) == 0 {
		return diffs
	}

	// Step 2: Deduplicate — group works by key, compute which keys need a fetch.
	// keyOrder: deterministic order of unique keys (first seen).
	type keyGroup struct {
		work     enrichWork // representative work item (for slug/base/head)
		diffIdxs []int      // all diff indices sharing this key
	}
	keyIndex := make(map[enrichCacheKey]int) // key → index in keyGroups
	keyGroups := make([]keyGroup, 0)

	for _, w := range works {
		if ki, exists := keyIndex[w.key]; exists {
			keyGroups[ki].diffIdxs = append(keyGroups[ki].diffIdxs, w.diffIdx)
		} else {
			keyIndex[w.key] = len(keyGroups)
			keyGroups = append(keyGroups, keyGroup{
				work:     w,
				diffIdxs: []int{w.diffIdx},
			})
		}
	}

	// Step 3: Fetch in parallel (one goroutine per unique key).
	results := make([]*enrichCacheValue, len(keyGroups))
	var wg sync.WaitGroup
	wg.Add(len(keyGroups))
	for gi, grp := range keyGroups {
		gi, grp := gi, grp
		go func() {
			defer wg.Done()
			cl, err := client.Compare(ctx, grp.work.slug, grp.work.base, grp.work.head)
			if err != nil {
				slog.Debug("upstream enrich: Compare error",
					"image", grp.work.slug, "err", err)
				results[gi] = &enrichCacheValue{cl: nil}
				return
			}
			results[gi] = &enrichCacheValue{cl: cl}
		}()
	}
	wg.Wait()

	// Step 4: Apply results to diffs (make a copy to avoid mutating input).
	out := make([]fleet.ImageDiff, len(diffs))
	copy(out, diffs)
	for gi, grp := range keyGroups {
		cl := results[gi].cl
		for _, idx := range grp.diffIdxs {
			out[idx].Changelog = cl
		}
	}

	return out
}
