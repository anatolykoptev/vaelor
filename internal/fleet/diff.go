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

// DiffStatusPriority maps DiffStatus to a sort-order integer used by Diff()
// for stable per-target output. Lower number = higher priority (appears first).
//
// External code wanting a DIFFERENT priority order (e.g. LLM-summary
// presentation) MUST declare its own table rather than mutating this one;
// see SummaryStatusPriority.
var DiffStatusPriority = map[DiffStatus]int{
	DiffTagDrift:    0,
	DiffDigestDrift: 1,
	DiffUnresolved:  2,
	DiffOnlySource:  3,
	DiffOnlyRuntime: 4,
	DiffMatch:       5,
}

// SummaryStatusPriority is the order used to rank entries when summarising
// fleet drift for an LLM prompt (cmd/go-code Phase 7). DELIBERATELY differs
// from DiffStatusPriority: OnlyRuntime ranks above OnlySource here because
// a running unpinned container is more actionable in incident triage than
// a pinned image with no running instance.
//
// Lower number = higher priority (appears first). The zero value for unknown
// statuses sorts last — callers should provide explicit fallback (e.g. 99)
// for statuses not in this map.
var SummaryStatusPriority = map[DiffStatus]int{
	DiffTagDrift:    0,
	DiffDigestDrift: 1,
	DiffUnresolved:  2,
	DiffOnlyRuntime: 3,
	DiffOnlySource:  4,
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
		pa, pb := DiffStatusPriority[ra.Status], DiffStatusPriority[rb.Status]
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

// ---------------------------------------------------------------------------
// Cross-host SiblingDrift types and logic
// ---------------------------------------------------------------------------

// TargetReportLike is the minimal view of a per-host probe report that
// SiblingDiff needs. cmd/go-code's TargetReport implements this interface via
// TargetStr() and DiffsList() methods so that internal/fleet stays import-free
// of cmd/.
type TargetReportLike interface {
	TargetStr() string
	DiffsList() []ImageDiff
}

// SiblingDriftRow groups same-Image runtime entries across targets whose
// tag or digest differs. Populated by SiblingDiff over a slice of
// TargetReports (one per probed host).
type SiblingDriftRow struct {
	Image    string           `json:"image"` // registry+repo, no tag/digest
	Variants []SiblingVariant `json:"variants"`
}

// SiblingVariant is one row inside a SiblingDriftRow — a single host's view
// of the image.
type SiblingVariant struct {
	Target    string `json:"target"` // the TargetReport.Target string (e.g. "ssh://piter")
	Tag       string `json:"tag"`
	Digest    string `json:"digest,omitempty"`
	Container string `json:"container,omitempty"` // container name on that target
	State     string `json:"state,omitempty"`
}

// SiblingDiff returns one SiblingDriftRow per Image that appears with a
// differing (Tag, Digest) tuple across two or more targets.
//
// Inputs: the per-target reports produced by per-host Diff().
// Output: deterministic order — by Image asc, then by Variant.Target asc.
// Empty result (nil slice) when fewer than 2 targets, or when no Image
// shows cross-host drift.
//
// Rules:
//   - For each Image observed in any target's Diffs[].Runtime (regardless of
//     Status — Match, OnlyRuntime, TagDrift, etc.), collect (Target, Tag, Digest, ...).
//   - If two or more variants for the same Image differ in (Tag, Digest), emit one row.
//   - Variants WITHOUT a Runtime (e.g. OnlySource rows) are skipped — only
//     actually-running containers count for sibling-drift purposes.
//   - When Digest is empty on one side and non-empty on the other, treat as
//     drift only if Tag also differs. (Same-tag/empty-digest-vs-real-digest
//     is the "we don't have full info" case, not actionable drift.)
//   - When the same Image appears multiple times in one target (multiple
//     containers), one representative variant is chosen (first by Container
//     name ascending).
func SiblingDiff(reports []TargetReportLike) []SiblingDriftRow {
	if len(reports) < 2 {
		return nil
	}

	// Collect per-image: map[image][target] = SiblingVariant (one per target).
	// When a target has multiple containers for the same image, we keep the
	// lexicographically first Container as the representative.
	type key struct {
		image  string
		target string
	}
	best := make(map[key]SiblingVariant)

	for _, r := range reports {
		target := r.TargetStr()
		for _, d := range r.DiffsList() {
			if d.Runtime == nil {
				// Skip rows with no running container (OnlySource etc.)
				continue
			}
			k := key{image: d.Image, target: target}
			v := SiblingVariant{
				Target:    target,
				Tag:       d.Runtime.Tag,
				Digest:    d.Runtime.Digest,
				Container: d.Runtime.Container,
				State:     d.Runtime.State,
			}
			if prev, ok := best[k]; !ok || v.Container < prev.Container {
				best[k] = v
			}
		}
	}

	// Group variants by image. Collect unique images.
	type imageData struct {
		variants []SiblingVariant
	}
	imgMap := make(map[string]*imageData)
	for k, v := range best {
		if _, ok := imgMap[k.image]; !ok {
			imgMap[k.image] = &imageData{}
		}
		imgMap[k.image].variants = append(imgMap[k.image].variants, v)
	}

	var rows []SiblingDriftRow
	for image, data := range imgMap {
		// Sort variants by Target for determinism.
		sort.Slice(data.variants, func(i, j int) bool {
			return data.variants[i].Target < data.variants[j].Target
		})

		// Check if there's actual drift among variants.
		if !hasSiblingDrift(data.variants) {
			continue
		}
		rows = append(rows, SiblingDriftRow{
			Image:    image,
			Variants: data.variants,
		})
	}

	// Sort rows by Image asc for determinism.
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Image < rows[j].Image
	})

	if len(rows) == 0 {
		return nil
	}
	return rows
}

// hasSiblingDrift returns true if any two variants differ in (Tag, Digest)
// per the spec rules:
//   - Different Tag → always drift.
//   - Same Tag, both Digest non-empty, Digest differs → drift.
//   - Same Tag, one Digest empty → NOT drift (incomplete info).
func hasSiblingDrift(variants []SiblingVariant) bool {
	for i := 0; i < len(variants); i++ {
		for j := i + 1; j < len(variants); j++ {
			a, b := variants[i], variants[j]
			if a.Tag != b.Tag {
				return true
			}
			// Same tag — only check digest if both are non-empty.
			if a.Digest != "" && b.Digest != "" && a.Digest != b.Digest {
				return true
			}
		}
	}
	return false
}
