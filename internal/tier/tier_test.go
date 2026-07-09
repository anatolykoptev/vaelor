package tier_test

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/tier"
)

func TestDetectBasic(t *testing.T) {
	t.Parallel()
	d := tier.NewDetector(tier.Backends{GoTypes: false, VTA: false})
	if got := d.Current(); got != tier.Basic {
		t.Fatalf("expected Basic tier, got %v", got)
	}
}

func TestDetectEnhanced(t *testing.T) {
	t.Parallel()
	d := tier.NewDetector(tier.Backends{GoTypes: true, VTA: false})
	if got := d.Current(); got != tier.Enhanced {
		t.Fatalf("expected Enhanced tier, got %v", got)
	}
}

func TestDetectFull(t *testing.T) {
	t.Parallel()
	d := tier.NewDetector(tier.Backends{GoTypes: true, VTA: true})
	if got := d.Current(); got != tier.Full {
		t.Fatalf("expected Full tier, got %v", got)
	}
}

func TestDegradationWarnings(t *testing.T) {
	t.Parallel()
	d := tier.NewDetector(tier.Backends{GoTypes: false, VTA: false})

	warns := d.Warnings()
	if len(warns) == 0 {
		t.Fatal("expected degradation warnings for Basic tier, got none")
	}

	w := warns[0]
	if w.Code != "go_types_missing" {
		t.Errorf("unexpected warning code: %q", w.Code)
	}
	if w.CapabilityPct != 40 {
		t.Errorf("expected CapabilityPct=40, got %d", w.CapabilityPct)
	}

	// Full tier must have no warnings.
	full := tier.NewDetector(tier.Backends{GoTypes: true, VTA: true})
	if w2 := full.Warnings(); len(w2) != 0 {
		t.Errorf("expected no warnings for Full tier, got %v", w2)
	}
}

func TestDetectSCIPEnhanced(t *testing.T) {
	t.Parallel()
	d := tier.NewDetector(tier.Backends{SCIP: true})
	if got := d.Current(); got != tier.Enhanced {
		t.Fatalf("expected Enhanced tier with SCIP only, got %v", got)
	}
}

func TestDetectSCIPAndGoTypesEnhanced(t *testing.T) {
	t.Parallel()
	d := tier.NewDetector(tier.Backends{GoTypes: true, SCIP: true})
	if got := d.Current(); got != tier.Enhanced {
		t.Fatalf("expected Enhanced tier with both backends, got %v", got)
	}
}

func TestProvenanceIncludesSCIP(t *testing.T) {
	t.Parallel()
	d := tier.NewDetector(tier.Backends{SCIP: true})
	p := d.ProvenanceFor()
	found := false
	for _, b := range p.Backends {
		if b == "scip" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'scip' in backends, got %v", p.Backends)
	}
}

func TestTierString(t *testing.T) {
	t.Parallel()
	cases := []struct {
		tier tier.Tier
		want string
	}{
		{tier.Basic, "basic"},
		{tier.Enhanced, "enhanced"},
		{tier.Full, "full"},
	}
	for _, tc := range cases {
		if got := tc.tier.String(); got != tc.want {
			t.Errorf("Tier(%d).String() = %q, want %q", int(tc.tier), got, tc.want)
		}
	}
}
