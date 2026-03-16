// Package tier defines the 3-level analysis capability tiers and detects
// which tier is available based on the active backends.
//
// Tiers:
//   - Basic (1): tree-sitter only — name-based symbol resolution
//   - Enhanced (2): tree-sitter + go/types — precise type-aware analysis
//   - Full (3): tree-sitter + go/types + VTA call graph — full call precision
package tier

import "fmt"

// Tier represents the analysis capability level.
type Tier int

const (
	// Basic uses tree-sitter only (name-based resolution).
	Basic Tier = 1
	// Enhanced adds go/types for precise type analysis.
	Enhanced Tier = 2
	// Full adds VTA call graph on top of Enhanced.
	Full Tier = 3
)

// String returns a human-readable tier name.
func (t Tier) String() string {
	switch t {
	case Basic:
		return "basic"
	case Enhanced:
		return "enhanced"
	case Full:
		return "full"
	default:
		return fmt.Sprintf("tier(%d)", int(t))
	}
}

// Backends describes which analysis backends are available.
type Backends struct {
	GoTypes bool
	VTA     bool
	Graph   bool
	LLM     bool
}

// DegradationWarning describes a capability reduction relative to Full tier.
type DegradationWarning struct {
	Code          string `json:"code"          xml:"code"`
	Message       string `json:"message"       xml:"message"`
	CapabilityPct int    `json:"capability_pct" xml:"capability_pct"`
}

// Provenance records which tier and backends were used for a given analysis.
type Provenance struct {
	Tier     string   `json:"tier"     xml:"tier"`
	Backends []string `json:"backends" xml:"backends"`
}

// Detector determines the current analysis tier and associated warnings
// from a set of available backends.
type Detector struct {
	backends Backends
	tier     Tier
	warnings []DegradationWarning
}

// NewDetector creates a Detector for the given backend availability and
// pre-computes the tier and any degradation warnings.
func NewDetector(b Backends) *Detector {
	d := &Detector{backends: b}
	d.detect()
	return d
}

func (d *Detector) detect() {
	switch {
	case !d.backends.GoTypes:
		d.tier = Basic
		d.warnings = []DegradationWarning{
			{
				Code: "go_types_missing",
				Message: "Go type analysis unavailable — using name-based resolution " +
					"(less precise for interfaces). Ensure repo has go.mod and is buildable.",
				CapabilityPct: 40,
			},
		}
	case !d.backends.VTA:
		d.tier = Enhanced
		d.warnings = []DegradationWarning{
			{
				Code: "vta_missing",
				Message: "VTA call graph unavailable — using go/types resolution " +
					"(precise for direct calls, approximate for interfaces).",
				CapabilityPct: 70,
			},
		}
	default:
		d.tier = Full
		d.warnings = nil
	}
}

// Current returns the detected analysis tier.
func (d *Detector) Current() Tier {
	return d.tier
}

// Warnings returns any capability degradation warnings for the current tier.
// Returns nil (not an empty slice) when the tier is Full.
func (d *Detector) Warnings() []DegradationWarning {
	return d.warnings
}

// ProvenanceFor builds a Provenance record listing the active backends
// alongside any caller-supplied names (e.g. tool names, pass names).
func (d *Detector) ProvenanceFor(used ...string) Provenance {
	active := make([]string, 0, 4+len(used))
	active = append(active, "tree-sitter")
	if d.backends.GoTypes {
		active = append(active, "go/types")
	}
	if d.backends.VTA {
		active = append(active, "vta")
	}
	if d.backends.Graph {
		active = append(active, "graph")
	}
	if d.backends.LLM {
		active = append(active, "llm")
	}
	active = append(active, used...)
	return Provenance{
		Tier:     d.tier.String(),
		Backends: active,
	}
}
