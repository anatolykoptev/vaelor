package tier_test

import (
	"encoding/json"
	"encoding/xml"
	"testing"

	"github.com/anatolykoptev/go-code/internal/tier"
)

// Hard red tests — boundary conditions, invariants, edge cases.

func TestTier_InvalidValue(t *testing.T) {
	// Unknown tier value should not panic, should return "tier(N)".
	unknown := tier.Tier(99)
	s := unknown.String()
	if s != "tier(99)" {
		t.Errorf("expected tier(99), got %q", s)
	}
}

func TestTier_ZeroValue(t *testing.T) {
	// Zero-value Tier is not Basic (Basic=1). Should be "tier(0)".
	var zero tier.Tier
	if zero == tier.Basic {
		t.Fatal("zero value should not equal Basic")
	}
	if s := zero.String(); s == "basic" {
		t.Fatal("zero value string should not be 'basic'")
	}
}

func TestDetector_VTAWithoutGoTypes(t *testing.T) {
	// VTA=true but GoTypes=false is an invalid combination.
	// Should still detect Basic (GoTypes is the gate).
	d := tier.NewDetector(tier.Backends{GoTypes: false, VTA: true})
	if d.Current() != tier.Basic {
		t.Errorf("VTA without GoTypes should be Basic, got %s", d.Current())
	}
	warns := d.Warnings()
	if len(warns) == 0 {
		t.Fatal("expected warnings for Basic tier even when VTA=true")
	}
	if warns[0].Code != "go_types_missing" {
		t.Errorf("expected go_types_missing warning, got %q", warns[0].Code)
	}
}

func TestDetector_AllBackends(t *testing.T) {
	// All backends enabled — should be Full with no warnings.
	d := tier.NewDetector(tier.Backends{GoTypes: true, VTA: true, Graph: true, LLM: true})
	if d.Current() != tier.Full {
		t.Errorf("all backends should be Full, got %s", d.Current())
	}
	if w := d.Warnings(); w != nil {
		t.Errorf("Full tier should have nil warnings, got %v", w)
	}
}

func TestDetector_WarningsNilNotEmpty(t *testing.T) {
	// Full tier: warnings must be nil, not []DegradationWarning{}.
	d := tier.NewDetector(tier.Backends{GoTypes: true, VTA: true})
	if w := d.Warnings(); w != nil {
		t.Fatalf("Full tier warnings must be nil, got len=%d", len(w))
	}
}

func TestDetector_EnhancedWarningCapability(t *testing.T) {
	// Enhanced tier (GoTypes=true, VTA=false) → 70% capability.
	d := tier.NewDetector(tier.Backends{GoTypes: true})
	warns := d.Warnings()
	if len(warns) != 1 {
		t.Fatalf("expected 1 warning for Enhanced tier, got %d", len(warns))
	}
	if warns[0].CapabilityPct != 70 {
		t.Errorf("Enhanced capability should be 70%%, got %d%%", warns[0].CapabilityPct)
	}
	if warns[0].Code != "vta_missing" {
		t.Errorf("expected vta_missing, got %q", warns[0].Code)
	}
}

func TestProvenance_BackendsOrder(t *testing.T) {
	// Provenance should always start with "tree-sitter".
	d := tier.NewDetector(tier.Backends{GoTypes: true, VTA: true, Graph: true, LLM: true})
	p := d.ProvenanceFor("custom")
	if len(p.Backends) == 0 {
		t.Fatal("expected backends")
	}
	if p.Backends[0] != "tree-sitter" {
		t.Errorf("first backend must be tree-sitter, got %q", p.Backends[0])
	}
	// Should contain all: tree-sitter, go/types, vta, graph, llm, custom.
	want := []string{"tree-sitter", "go/types", "vta", "graph", "llm", "custom"}
	if len(p.Backends) != len(want) {
		t.Fatalf("expected %d backends, got %d: %v", len(want), len(p.Backends), p.Backends)
	}
	for i, w := range want {
		if p.Backends[i] != w {
			t.Errorf("backend[%d] = %q, want %q", i, p.Backends[i], w)
		}
	}
}

func TestProvenance_BasicMinimal(t *testing.T) {
	// Basic tier provenance should only have tree-sitter.
	d := tier.NewDetector(tier.Backends{})
	p := d.ProvenanceFor()
	if len(p.Backends) != 1 || p.Backends[0] != "tree-sitter" {
		t.Errorf("Basic provenance should be [tree-sitter], got %v", p.Backends)
	}
	if p.Tier != "basic" {
		t.Errorf("expected tier=basic, got %q", p.Tier)
	}
}

func TestDegradationWarning_JSONRoundtrip(t *testing.T) {
	w := tier.DegradationWarning{
		Code:          "go_types_missing",
		Message:       "test message with special chars: <>&\"",
		CapabilityPct: 40,
	}
	data, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var w2 tier.DegradationWarning
	if err := json.Unmarshal(data, &w2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if w2 != w {
		t.Errorf("roundtrip mismatch: %+v vs %+v", w, w2)
	}
}

func TestDegradationWarning_XMLRoundtrip(t *testing.T) {
	w := tier.DegradationWarning{
		Code:          "vta_missing",
		Message:       "test <xml> & chars",
		CapabilityPct: 70,
	}
	data, err := xml.Marshal(w)
	if err != nil {
		t.Fatalf("xml.Marshal: %v", err)
	}
	var w2 tier.DegradationWarning
	if err := xml.Unmarshal(data, &w2); err != nil {
		t.Fatalf("xml.Unmarshal: %v", err)
	}
	if w2 != w {
		t.Errorf("roundtrip mismatch: %+v vs %+v", w, w2)
	}
}

func TestTier_Ordering(t *testing.T) {
	// Tiers should be ordered: Basic < Enhanced < Full.
	if tier.Basic >= tier.Enhanced {
		t.Error("Basic must be < Enhanced")
	}
	if tier.Enhanced >= tier.Full {
		t.Error("Enhanced must be < Full")
	}
}
