package fleet

import (
	"fmt"
	"sort"

	"github.com/anatolykoptev/go-code/internal/polyglot/pinned"
)

// DiffStatus enumerates the possible diff outcomes for one image.
type DiffStatus string

const (
	DiffMatch       DiffStatus = "Match"
	DiffTagDrift    DiffStatus = "TagDrift"
	DiffDigestDrift DiffStatus = "DigestDrift"
	DiffOnlySource  DiffStatus = "OnlySource"  // pinned in repo, not running anywhere
	DiffOnlyRuntime DiffStatus = "OnlyRuntime" // running but not pinned in repo
	DiffUnresolved  DiffStatus = "Unresolved"  // pinned image had Unresolved set; cannot compare
)

// diffStatusPriority maps DiffStatus to a sort-order integer.
// Lower = higher priority (appears first in sorted output).
var diffStatusPriority = map[DiffStatus]int{
	DiffTagDrift:    0,
	DiffDigestDrift: 1,
	DiffUnresolved:  2,
	DiffOnlySource:  3,
	DiffOnlyRuntime: 4,
	DiffMatch:       5,
}

// ImageDiff is one row of the per-target report. Either Pinned or Runtime (or both)
// is non-nil — never both nil.
type ImageDiff struct {
	Image       string // the registry+repo identifier this row is about
	Pinned      *pinned.PinnedImage
	Runtime     *RuntimeImage
	Status      DiffStatus
	Explanation string // one-line human-readable summary
}

// Diff computes per-image diffs between pinned (source) and runtime (probe) sets.
//
// Matching: by Image string (registry+repo, no tag, no digest). Case-sensitive.
//
// Semantics per image group:
//   - Pinned only, no Runtime          → DiffOnlySource     (Pinned set, Runtime nil)
//   - Runtime only, no Pinned          → DiffOnlyRuntime    (Pinned nil, Runtime set)
//   - Pinned.Unresolved != ""          → DiffUnresolved     (Pinned set, Runtime set if available)
//   - Both present, same Tag, same Digest (or both Digest=="")  → DiffMatch
//   - Both present, same Tag, different Digest (both non-empty) → DiffDigestDrift
//   - Both present, different Tag                              → DiffTagDrift
//
// One-side-has-digest, other-does-not: treated as DiffMatch (no evidence of drift;
// runtime probes may not surface digests).
//
// When the same Image appears multiple times in either input (e.g. two compose
// services referencing nginx), items are sorted within each Image group before
// pairing:
//   - Pinned:  by Service, then Source, then Line
//   - Runtime: by Container, then StartedAt
//
// Surplus on either side gets OnlySource / OnlyRuntime rows.
//
// Output is sorted: by Status priority (TagDrift > DigestDrift > Unresolved >
// OnlySource > OnlyRuntime > Match), then by Image asc, then by
// Pinned.Service / Runtime.Container — deterministic.
func Diff(pinnedImgs []pinned.PinnedImage, runtimeImgs []RuntimeImage) []ImageDiff {
	// Group pinned by Image.
	pinnedByImage := make(map[string][]pinned.PinnedImage)
	for _, p := range pinnedImgs {
		pinnedByImage[p.Image] = append(pinnedByImage[p.Image], p)
	}

	// Group runtime by Image.
	runtimeByImage := make(map[string][]RuntimeImage)
	for _, r := range runtimeImgs {
		runtimeByImage[r.Image] = append(runtimeByImage[r.Image], r)
	}

	// Collect all image keys.
	keySet := make(map[string]struct{})
	for k := range pinnedByImage {
		keySet[k] = struct{}{}
	}
	for k := range runtimeByImage {
		keySet[k] = struct{}{}
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var rows []ImageDiff

	for _, image := range keys {
		ps := sortedPinned(pinnedByImage[image])
		rs := sortedRuntime(runtimeByImage[image])

		// Pair by index.
		i, j := 0, 0
		for i < len(ps) && j < len(rs) {
			p := ps[i]
			r := rs[j]

			// If pinned is unresolved, consume both but emit Unresolved.
			if p.Unresolved != "" {
				rows = append(rows, ImageDiff{
					Image:       image,
					Pinned:      ptrPinned(p),
					Runtime:     ptrRuntime(r),
					Status:      DiffUnresolved,
					Explanation: fmt.Sprintf("pinned image tag is unresolved: %s", p.Unresolved),
				})
				i++
				j++
				continue
			}

			rows = append(rows, classifyPair(image, p, r))
			i++
			j++
		}

		// Surplus pinned → OnlySource.
		for ; i < len(ps); i++ {
			p := ps[i]
			row := ImageDiff{
				Image:  image,
				Pinned: ptrPinned(p),
			}
			if p.Unresolved != "" {
				row.Status = DiffUnresolved
				row.Explanation = fmt.Sprintf("pinned image tag is unresolved: %s", p.Unresolved)
			} else {
				row.Status = DiffOnlySource
				row.Explanation = fmt.Sprintf("image %q is pinned (tag %q) but not found in runtime", image, p.Tag)
			}
			rows = append(rows, row)
		}

		// Surplus runtime → OnlyRuntime.
		for ; j < len(rs); j++ {
			r := rs[j]
			rows = append(rows, ImageDiff{
				Image:       image,
				Runtime:     ptrRuntime(r),
				Status:      DiffOnlyRuntime,
				Explanation: fmt.Sprintf("image %q (tag %q) is running but not pinned in repo", image, r.Tag),
			})
		}
	}

	sort.SliceStable(rows, func(a, b int) bool {
		ra, rb := rows[a], rows[b]
		pa, pb := diffStatusPriority[ra.Status], diffStatusPriority[rb.Status]
		if pa != pb {
			return pa < pb
		}
		if ra.Image != rb.Image {
			return ra.Image < rb.Image
		}
		sa := pinnedService(ra)
		sb := pinnedService(rb)
		if sa != sb {
			return sa < sb
		}
		return runtimeContainer(ra) < runtimeContainer(rb)
	})

	return rows
}

// classifyPair compares one paired pinned+runtime entry.
func classifyPair(image string, p pinned.PinnedImage, r RuntimeImage) ImageDiff {
	switch {
	case p.Tag != r.Tag:
		return ImageDiff{
			Image:       image,
			Pinned:      ptrPinned(p),
			Runtime:     ptrRuntime(r),
			Status:      DiffTagDrift,
			Explanation: fmt.Sprintf("tag drift: pinned %q vs runtime %q", p.Tag, r.Tag),
		}
	case p.Digest != "" && r.Digest != "" && p.Digest != r.Digest:
		return ImageDiff{
			Image:       image,
			Pinned:      ptrPinned(p),
			Runtime:     ptrRuntime(r),
			Status:      DiffDigestDrift,
			Explanation: fmt.Sprintf("digest drift: pinned %s vs runtime %s", p.Digest, r.Digest),
		}
	default:
		// Match (includes case where one or both digests are empty).
		return ImageDiff{
			Image:   image,
			Pinned:  ptrPinned(p),
			Runtime: ptrRuntime(r),
			Status:  DiffMatch,
		}
	}
}

// sortedPinned returns a copy of ps sorted by Service, Source, Line.
func sortedPinned(ps []pinned.PinnedImage) []pinned.PinnedImage {
	out := make([]pinned.PinnedImage, len(ps))
	copy(out, ps)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Service != out[j].Service {
			return out[i].Service < out[j].Service
		}
		if out[i].Source != out[j].Source {
			return out[i].Source < out[j].Source
		}
		return out[i].Line < out[j].Line
	})
	return out
}

// sortedRuntime returns a copy of rs sorted by Container, StartedAt.
func sortedRuntime(rs []RuntimeImage) []RuntimeImage {
	out := make([]RuntimeImage, len(rs))
	copy(out, rs)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Container != out[j].Container {
			return out[i].Container < out[j].Container
		}
		return out[i].StartedAt.Before(out[j].StartedAt)
	})
	return out
}

func ptrPinned(p pinned.PinnedImage) *pinned.PinnedImage {
	v := p
	return &v
}

func ptrRuntime(r RuntimeImage) *RuntimeImage {
	v := r
	return &v
}

func pinnedService(d ImageDiff) string {
	if d.Pinned != nil {
		return d.Pinned.Service
	}
	return ""
}

func runtimeContainer(d ImageDiff) string {
	if d.Runtime != nil {
		return d.Runtime.Container
	}
	return ""
}
