// cmd/go-code/tool_debug_investigate_fleet.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

	"time"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/fleet"
	"github.com/anatolykoptev/vaelor/internal/fleet/upstream"
	"github.com/anatolykoptev/vaelor/internal/investigate"
	"github.com/anatolykoptev/vaelor/internal/polyglot/pinned"
)

// summaryStatusPriorityOf returns the sort priority for status s using the
// fleet.SummaryStatusPriority table. Unknown statuses sort last (99).
func summaryStatusPriorityOf(s string) int {
	if p, ok := fleet.SummaryStatusPriority[fleet.DiffStatus(s)]; ok {
		return p
	}
	return 99
}

// summarizeFleetForLLM returns a human-readable summary suitable for inclusion
// in the LLM prompt. It prepends a changelog-notes section (top-5 image/count pairs)
// when any TagDrift row has upstream changelog data, then sibling-drift section
// (top-5 entries), then per-target drift sections.
//
// Rules for per-target section:
//  1. Drop Match diffs entirely.
//  2. Sort remaining by status priority: TagDrift > DigestDrift > Unresolved > OnlyRuntime > OnlySource
//  3. Cap to top 20 diffs in the summary.
//  4. Trailing "and N more diffs of type X, Y, Z" for the tail.
//
// Returns "" if no non-Match diffs anywhere.
func summarizeFleetForLLM(report *investigate.FleetReport) string {
	var b strings.Builder

	// Changelog notes prelude (top-5 TagDrift rows with upstream data).
	type changelogNote struct {
		image   string
		count   int
		subject string // first commit subject
	}
	var notes []changelogNote
	noteSet := make(map[string]bool)
	for _, t := range report.Targets {
		for _, d := range t.Diffs {
			if d.Status == "TagDrift" && d.ChangelogSummary != "" && !noteSet[d.Image] {
				noteSet[d.Image] = true
				notes = append(notes, changelogNote{image: d.Image, subject: d.ChangelogSummary})
				if len(notes) >= 5 {
					break
				}
			}
		}
	}
	// Also check single-host backward-compat diffs.
	for _, d := range report.Diffs {
		if d.Status == "TagDrift" && d.ChangelogSummary != "" && !noteSet[d.Image] {
			noteSet[d.Image] = true
			notes = append(notes, changelogNote{image: d.Image, subject: d.ChangelogSummary})
			if len(notes) >= 5 {
				break
			}
		}
	}
	if len(notes) > 0 {
		fmt.Fprintf(&b, "Changelog notes (top upstream commit per drifting image):\n")
		for _, n := range notes {
			fmt.Fprintf(&b, " - %s: %s\n", n.image, n.subject)
		}
	}

	// Sibling-drift prelude (top-5 cap).
	if len(report.SiblingDrifts) > 0 {
		fmt.Fprintf(&b, "Sibling drift across hosts:\n")
		cap5 := report.SiblingDrifts
		if len(cap5) > 5 {
			cap5 = cap5[:5]
		}
		for _, row := range cap5 {
			parts := make([]string, 0, len(row.Variants))
			for _, v := range row.Variants {
				parts = append(parts, fmt.Sprintf("%s=%s", v.Target, v.Tag))
			}
			fmt.Fprintf(&b, " - %s: %s\n", row.Image, strings.Join(parts, ", "))
		}
		if len(report.SiblingDrifts) > 5 {
			fmt.Fprintf(&b, "... and %d more sibling-drift images.\n", len(report.SiblingDrifts)-5)
		}
	}

	// Per-target sections (multi-host).
	if len(report.Targets) > 0 {
		for _, t := range report.Targets {
			section := summarizeTargetDiffRows(t.Target, t.Diffs)
			if section != "" {
				fmt.Fprintf(&b, "%s\n", section)
			}
		}
	} else if report.Target != "" {
		// Single-host backward-compat path.
		section := summarizeTargetDiffRows(report.Target, report.Diffs)
		if section != "" {
			fmt.Fprintf(&b, "%s\n", section)
		}
	}

	result := strings.TrimRight(b.String(), "\n")
	return result
}

// summarizeTargetDiffRows returns a per-target drift summary (empty if only Match rows).
func summarizeTargetDiffRows(target string, rows []investigate.FleetDiffRow) string {
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

// runFleetVersionsPhase populates res.RuntimeVersions with diffs of repo-pinned
// images against runtime containers on input.Host / input.Hosts. Soft failures
// append to res.Diagnostics.Warnings and leave res.RuntimeVersions nil.
//
// Multi-host: when Hosts is set (or both Host and Hosts), all hosts are probed
// in parallel. Per-host failures go into FleetTargetRow.Error. SiblingDrifts
// are populated when ≥2 hosts were probed.
//
// Phase 7 NEVER aborts the investigation.
func runFleetVersionsPhase(ctx context.Context, input DebugInvestigateInput,
	cfg Config, deps analyze.Deps, res *investigate.InvestigationResult,
) {
	// Resolve effective hosts (same back-compat rules as tool_fleet_versions.go).
	var effectiveHosts []string
	if len(input.Hosts) > 0 {
		effectiveHosts = input.Hosts
	} else {
		h := input.Host
		if h == "" {
			h = cfg.FleetDefaultHost
		}
		if h != "" {
			effectiveHosts = []string{h}
		}
	}

	// Collect pinned images from repo (best-effort).
	pinnedImgs := collectPinnedForInvestigation(ctx, input, deps)

	if len(effectiveHosts) == 0 {
		// No host — warn only if repo has compose/Dockerfile (has intent).
		if len(pinnedImgs) > 0 {
			res.Diagnostics.Warnings = append(res.Diagnostics.Warnings,
				"Phase 7 (fleet versions) skipped: pass `host`/`hosts` param or set GOCODE_FLEET_DEFAULT_HOST to enable")
		}
		return
	}

	// Validate all host strings up-front (avoids partial errors later).
	type parsedHost struct {
		raw    string
		target fleet.Target
	}
	parsedHosts := make([]parsedHost, 0, len(effectiveHosts))
	for _, h := range effectiveHosts {
		t, err := fleet.ParseTarget(h)
		if err != nil {
			res.Diagnostics.Warnings = append(res.Diagnostics.Warnings,
				fmt.Sprintf("Phase 7 (fleet versions) skipped: invalid host %q: %v", h, err))
			return
		}
		if t.Scheme == "local" {
			t.Scheme = "docker"
		}
		parsedHosts = append(parsedHosts, parsedHost{raw: h, target: t})
	}

	reg := buildFleetRegistry(cfg)

	// Probe all hosts in parallel: collect raw fleet.ImageDiff slices.
	type hostProbeResult struct {
		raw   string
		diffs []fleet.ImageDiff
		error string
	}
	probeResults := make([]hostProbeResult, len(parsedHosts))
	var wg sync.WaitGroup
	wg.Add(len(parsedHosts))
	for i, ph := range parsedHosts {
		i, ph := i, ph
		go func() {
			defer wg.Done()
			diffs, errStr := rawProbeForInvestigation(ctx, ph.raw, ph.target, pinnedImgs, input.Service, reg)
			probeResults[i] = hostProbeResult{raw: ph.raw, diffs: diffs, error: errStr}
		}()
	}
	wg.Wait()

	// Upstream changelog enrichment for TagDrift rows (before translation to FleetDiffRow).
	if !cfg.FleetUpstreamDisable && cfg.GithubToken != "" {
		upstreamClient := upstream.New(
			upstream.WithToken(cfg.GithubToken),
			upstream.WithTimeout(8*time.Second),
		)
		for i := range probeResults {
			probeResults[i].diffs = upstream.Enrich(ctx, upstreamClient, probeResults[i].diffs, 30)
		}
	}

	// Translate to investigate.FleetTargetRow.
	targetRows := make([]investigate.FleetTargetRow, len(probeResults))
	for i, pr := range probeResults {
		targetRows[i] = investigate.FleetTargetRow{
			Target: pr.raw,
			Diffs:  toInvestigateDiffRows(pr.diffs),
			Error:  pr.error,
		}
	}

	// Compute sibling drift.
	var siblingDrifts []investigate.FleetSiblingDriftRow
	if len(targetRows) >= 2 {
		// Build TargetReportLike slice from target rows.
		likes := make([]fleet.TargetReportLike, len(targetRows))
		for i := range targetRows {
			likes[i] = investigateTargetRowLike{targetRows[i]}
		}
		rawDrifts := fleet.SiblingDiff(likes)
		siblingDrifts = toInvestigateSiblingDriftRows(rawDrifts)
	}

	// Build report.
	report := &investigate.FleetReport{
		Targets:       targetRows,
		SiblingDrifts: siblingDrifts,
	}

	// For single-host backward-compat: also populate top-level Target/Diffs/Error.
	if len(targetRows) == 1 {
		report.Target = targetRows[0].Target
		report.Diffs = targetRows[0].Diffs
		report.Error = targetRows[0].Error
	} else if len(targetRows) > 1 {
		// Multi-host: log any probe errors as warnings.
		for _, row := range targetRows {
			if row.Error != "" {
				slog.WarnContext(ctx, "fleet phase: probe error on host",
					"host", row.Target, "err", row.Error)
			}
		}
	}

	report.Summary = summarizeFleetForLLM(report)
	res.RuntimeVersions = report
}

// investigateTargetRowLike adapts investigate.FleetTargetRow to fleet.TargetReportLike.
// This lets fleet.SiblingDiff work without importing cmd/ types.
type investigateTargetRowLike struct {
	row investigate.FleetTargetRow
}

func (r investigateTargetRowLike) TargetStr() string { return r.row.Target }
func (r investigateTargetRowLike) DiffsList() []fleet.ImageDiff {
	// Re-hydrate ImageDiff from FleetDiffRow for SiblingDiff (only Runtime matters).
	diffs := make([]fleet.ImageDiff, 0, len(r.row.Diffs))
	for _, d := range r.row.Diffs {
		diff := fleet.ImageDiff{
			Image:  d.Image,
			Status: fleet.DiffStatus(d.Status),
		}
		if d.RuntimeTag != "" || d.RuntimeDigest != "" || d.Container != "" || d.State != "" {
			diff.Runtime = &fleet.RuntimeImage{
				Image:     d.Image,
				Tag:       d.RuntimeTag,
				Digest:    d.RuntimeDigest,
				Container: d.Container,
				State:     d.State,
			}
		}
		diffs = append(diffs, diff)
	}
	return diffs
}

// rawProbeForInvestigation probes one host and returns the raw fleet.ImageDiff slice.
// The caller is responsible for upstream enrichment and translation to FleetTargetRow.
func rawProbeForInvestigation(
	ctx context.Context,
	hostRaw string,
	t fleet.Target,
	pinnedImgs []pinned.PinnedImage,
	service string,
	reg *fleet.Registry,
) ([]fleet.ImageDiff, string) {
	probe, probeErr := reg.Get(t)

	var runtimeImgs []fleet.RuntimeImage
	var targetErr string

	if probeErr != nil {
		targetErr = probeErr.Error()
	} else {
		var err error
		runtimeImgs, err = probe.List(ctx, t, fleet.Filter{Service: service})
		if err != nil {
			targetErr = err.Error()
			slog.WarnContext(ctx, "fleet phase: probe.List error", "host", hostRaw, "err", err)
		}
	}

	diffs := fleet.Diff(pinnedImgs, runtimeImgs)
	return diffs, targetErr
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
		// Carry through the first commit subject from upstream changelog, if available.
		if d.Changelog != nil && d.Changelog.Resolved && len(d.Changelog.Commits) > 0 {
			row.ChangelogSummary = d.Changelog.Commits[0].Subject
		}
		rows = append(rows, row)
	}
	return rows
}

// toInvestigateSiblingDriftRows converts []fleet.SiblingDriftRow to the
// flattened investigate type so internal/investigate stays import-free of internal/fleet.
func toInvestigateSiblingDriftRows(rows []fleet.SiblingDriftRow) []investigate.FleetSiblingDriftRow {
	if len(rows) == 0 {
		return nil
	}
	result := make([]investigate.FleetSiblingDriftRow, 0, len(rows))
	for _, r := range rows {
		variants := make([]investigate.FleetSiblingVariant, 0, len(r.Variants))
		for _, v := range r.Variants {
			variants = append(variants, investigate.FleetSiblingVariant{
				Target:    v.Target,
				Tag:       v.Tag,
				Digest:    v.Digest,
				Container: v.Container,
				State:     v.State,
			})
		}
		result = append(result, investigate.FleetSiblingDriftRow{
			Image:    r.Image,
			Variants: variants,
		})
	}
	return result
}
