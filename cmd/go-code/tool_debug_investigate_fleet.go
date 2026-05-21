// cmd/go-code/tool_debug_investigate_fleet.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/fleet"
	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/polyglot/pinned"
)

// summaryStatusPriorityOf returns the sort priority for status s using the
// fleet.SummaryStatusPriority table. Unknown statuses sort last (99).
func summaryStatusPriorityOf(s string) int {
	if p, ok := fleet.SummaryStatusPriority[fleet.DiffStatus(s)]; ok {
		return p
	}
	return 99
}

// summarizeFleetForLLM returns a one-paragraph human-readable summary suitable
// for inclusion in the LLM prompt. Rules (per plan):
//
//  1. Drop Match diffs entirely.
//  2. Sort remaining by status priority:
//     TagDrift > DigestDrift > Unresolved > OnlyRuntime > OnlySource
//  3. Cap to top 20 diffs in the summary.
//  4. Trailing summary line for the remainder: "and N more diffs of type X, Y, Z"
//     where X, Y, Z are the distinct status names among the dropped tail
//     (sorted alphabetically for determinism).
//
// Returns "" if no non-Match diffs.
func summarizeFleetForLLM(target string, rows []investigate.FleetDiffRow) string {
	// Filter out Match rows.
	var nonMatch []investigate.FleetDiffRow
	for _, r := range rows {
		if r.Status != "Match" {
			nonMatch = append(nonMatch, r)
		}
	}
	if len(nonMatch) == 0 {
		return ""
	}

	// Sort by summary priority, then by image name for determinism.
	sorted := make([]investigate.FleetDiffRow, len(nonMatch))
	copy(sorted, nonMatch)
	sort.SliceStable(sorted, func(i, j int) bool {
		pi, pj := summaryStatusPriorityOf(sorted[i].Status), summaryStatusPriorityOf(sorted[j].Status)
		if pi != pj {
			return pi < pj
		}
		return sorted[i].Image < sorted[j].Image
	})

	const cap20 = 20
	top := sorted
	tail := []investigate.FleetDiffRow{}
	if len(sorted) > cap20 {
		top = sorted[:cap20]
		tail = sorted[cap20:]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Runtime drift detected on %s:\n", target)
	for _, row := range top {
		explanation := row.Explanation
		if explanation == "" {
			// Build minimal explanation from tags when Explanation is not set.
			switch row.Status {
			case "TagDrift":
				explanation = fmt.Sprintf("pinned %q → running %q", row.PinnedTag, row.RuntimeTag)
			case "DigestDrift":
				explanation = fmt.Sprintf("same tag, different digest %s/%s", row.PinnedDigest, row.RuntimeDigest)
			default:
				explanation = row.Image
			}
		}
		fmt.Fprintf(&b, " - %s: %s %s\n", row.Status, row.Image, explanation)
	}

	if len(tail) > 0 {
		// Collect distinct status names from the tail, sorted alphabetically for determinism.
		tailStatuses := make(map[string]struct{})
		for _, r := range tail {
			tailStatuses[r.Status] = struct{}{}
		}
		statusList := make([]string, 0, len(tailStatuses))
		for s := range tailStatuses {
			statusList = append(statusList, s)
		}
		sort.Strings(statusList)
		fmt.Fprintf(&b, "... and %d more diffs of type %s.", len(tail), strings.Join(statusList, ", "))
	}

	return strings.TrimRight(b.String(), "\n")
}

// runFleetVersionsPhase populates res.RuntimeVersions with a diff of repo-pinned
// images against runtime containers on input.Host. Soft failures append to
// res.Diagnostics.Warnings and leave res.RuntimeVersions nil.
//
// Phase 7 NEVER aborts the investigation.
func runFleetVersionsPhase(ctx context.Context, input DebugInvestigateInput,
	cfg Config, deps analyze.Deps, res *investigate.InvestigationResult,
) {
	host := input.Host
	if host == "" {
		host = cfg.FleetDefaultHost
	}

	// Collect pinned images from repo (best-effort).
	pinnedImgs := collectPinnedForInvestigation(ctx, input, deps)

	if host == "" {
		// No host — warn only if repo has compose/Dockerfile (has intent).
		if len(pinnedImgs) > 0 {
			res.Diagnostics.Warnings = append(res.Diagnostics.Warnings,
				"Phase 7 (fleet versions) skipped: pass `host` param or set GOCODE_FLEET_DEFAULT_HOST to enable")
		}
		return
	}

	t, err := fleet.ParseTarget(host)
	if err != nil {
		res.Diagnostics.Warnings = append(res.Diagnostics.Warnings,
			fmt.Sprintf("Phase 7 (fleet versions) skipped: invalid host %q: %v", host, err))
		return
	}

	// Remap "local" → "docker" for registry dispatch (same as tool_fleet_versions.go).
	if t.Scheme == "local" {
		t.Scheme = "docker"
	}

	reg := buildFleetRegistry(cfg)
	probe, err := reg.Get(t)
	if err != nil {
		report := &investigate.FleetReport{
			Target: host,
			Error:  err.Error(),
		}
		res.RuntimeVersions = report
		return
	}

	runtimeImgs, probeErr := probe.List(ctx, t, fleet.Filter{Service: input.Service})
	var probeErrStr string
	if probeErr != nil {
		probeErrStr = probeErr.Error()
		slog.WarnContext(ctx, "fleet phase: probe.List error", "err", probeErr)
	}

	diffs := fleet.Diff(pinnedImgs, runtimeImgs)

	report := &investigate.FleetReport{
		Target: host,
		Diffs:  toInvestigateDiffRows(diffs),
		Error:  probeErrStr,
	}
	report.Summary = summarizeFleetForLLM(host, report.Diffs)
	res.RuntimeVersions = report
}

// collectPinnedForInvestigation collects pinned images from the repo path.
// Returns empty slice on error or when repo is not set.
// Best-effort: errors are ignored per spec.
func collectPinnedForInvestigation(_ context.Context, input DebugInvestigateInput, _ analyze.Deps) []pinned.PinnedImage {
	if input.Repo == "" {
		return nil
	}
	imgs, _ := pinned.Collect(input.Repo)
	return imgs
}

// toInvestigateDiffRows translates []fleet.ImageDiff to []investigate.FleetDiffRow.
// This translation keeps internal/investigate import-free of internal/fleet.
func toInvestigateDiffRows(diffs []fleet.ImageDiff) []investigate.FleetDiffRow {
	rows := make([]investigate.FleetDiffRow, 0, len(diffs))
	for _, d := range diffs {
		row := investigate.FleetDiffRow{
			Image:       d.Image,
			Status:      string(d.Status),
			Explanation: d.Explanation,
		}
		if d.Pinned != nil {
			row.PinnedTag = d.Pinned.Tag
			row.PinnedDigest = d.Pinned.Digest
			row.Source = d.Pinned.Source
			row.Service = d.Pinned.Service
			row.Unresolved = d.Pinned.Unresolved
		}
		if d.Runtime != nil {
			row.RuntimeTag = d.Runtime.Tag
			row.RuntimeDigest = d.Runtime.Digest
			row.Container = d.Runtime.Container
			row.State = d.Runtime.State
			row.StartedAt = d.Runtime.StartedAt
			// Prefer runtime service label if pinned service is empty.
			if row.Service == "" {
				row.Service = d.Runtime.Service
			}
		}
		rows = append(rows, row)
	}
	return rows
}
