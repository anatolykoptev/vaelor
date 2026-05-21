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
type FleetReport struct {
	Target  string         `json:"target"`
	Diffs   []FleetDiffRow `json:"diffs"`
	Error   string         `json:"error,omitempty"`
	Summary string         `json:"summary,omitempty"` // top-20-ish line for LLM
}

type FleetDiffRow struct {
	Image         string    `json:"image"`
	Status        string    `json:"status"` // "Match" / "TagDrift" / ... (from fleet.DiffStatus)
	PinnedTag     string    `json:"pinned_tag,omitempty"`
	RuntimeTag    string    `json:"runtime_tag,omitempty"`
	PinnedDigest  string    `json:"pinned_digest,omitempty"`
	RuntimeDigest string    `json:"runtime_digest,omitempty"`
	Source        string    `json:"source,omitempty"`    // pinned source file
	Container     string    `json:"container,omitempty"` // runtime container name
	Service       string    `json:"service,omitempty"`   // compose service (either side)
	State         string    `json:"state,omitempty"`     // runtime state
	StartedAt     time.Time `json:"started_at,omitempty"`
	Explanation   string    `json:"explanation,omitempty"`
	Unresolved    string    `json:"unresolved,omitempty"` // pinned-side parse caveat
}
