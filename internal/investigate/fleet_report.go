// internal/investigate/fleet_report.go
package investigate

import "time"

// FleetReport carries the output of runFleetVersionsPhase for an investigation.
// Shape mirrors cmd/go-code's FleetVersionsOutput but lives in this package
// to keep internal/fleet from depending on cmd/.
//
// Note: FleetReport does NOT directly reference internal/fleet types — fields
// are flattened to plain strings/ints so this package stays import-free of
// /fleet. Translation happens in runFleetVersionsPhase.
//
// Multi-host layout: Targets holds one FleetTargetRow per probed host;
// SiblingDrifts carries cross-host drift rows (populated when ≥2 hosts).
// Single-host layout (backward compat): Target/Diffs/Error/Summary fields
// are also populated so existing callers continue to work.
type FleetReport struct {
	// Single-host fields (backward compat; also populated in multi-host).
	Target  string         `json:"target"`
	Diffs   []FleetDiffRow `json:"diffs"`
	Error   string         `json:"error,omitempty"`
	Summary string         `json:"summary,omitempty"` // top-20-ish line for LLM

	// Multi-host fields (populated when ≥2 hosts probed).
	Targets       []FleetTargetRow       `json:"targets,omitempty"`
	SiblingDrifts []FleetSiblingDriftRow `json:"sibling_drifts,omitempty"`
}

// FleetTargetRow is one host's result in a multi-host FleetReport.
type FleetTargetRow struct {
	Target string         `json:"target"`
	Diffs  []FleetDiffRow `json:"diffs"`
	Error  string         `json:"error,omitempty"`
}

// FleetSiblingDriftRow is the flattened mirror of fleet.SiblingDriftRow.
// Lives here so internal/investigate stays import-free of internal/fleet.
type FleetSiblingDriftRow struct {
	Image    string                `json:"image"`
	Variants []FleetSiblingVariant `json:"variants"`
}

// FleetSiblingVariant is one host's view of a drifting image.
type FleetSiblingVariant struct {
	Target    string `json:"target"`
	Tag       string `json:"tag"`
	Digest    string `json:"digest,omitempty"`
	Container string `json:"container,omitempty"`
	State     string `json:"state,omitempty"`
}

type FleetDiffRow struct {
	Image            string    `json:"image"`
	Status           string    `json:"status"` // "Match" / "TagDrift" / ... (from fleet.DiffStatus)
	PinnedTag        string    `json:"pinned_tag,omitempty"`
	RuntimeTag       string    `json:"runtime_tag,omitempty"`
	PinnedDigest     string    `json:"pinned_digest,omitempty"`
	RuntimeDigest    string    `json:"runtime_digest,omitempty"`
	Source           string    `json:"source,omitempty"`    // pinned source file
	Container        string    `json:"container,omitempty"` // runtime container name
	Service          string    `json:"service,omitempty"`   // compose service (either side)
	State            string    `json:"state,omitempty"`     // runtime state
	StartedAt        time.Time `json:"started_at,omitempty"`
	Explanation      string    `json:"explanation,omitempty"`
	Unresolved       string    `json:"unresolved,omitempty"`        // pinned-side parse caveat
	ChangelogSummary string    `json:"changelog_summary,omitempty"` // first commit subject from upstream changelog, if available
}
