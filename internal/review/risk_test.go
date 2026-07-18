package review

import (
	"testing"

	"github.com/anatolykoptev/vaelor/internal/impact"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

func TestGenerateRiskGuidance(t *testing.T) {
	t.Parallel()
	input := RiskInput{
		ChangedSymbols: []ChangedSymbol{
			{Symbol: &parser.Symbol{Name: "Auth"}, ChangeType: ChangeModified},
		},
		ImpactResults: map[string]*impact.Result{
			"Auth": {Symbol: "Auth", Found: true, TotalAffected: 25, BlastRadius: "high",
				AffectedPackages: []string{"pkg/api", "pkg/middleware", "pkg/handler"}},
		},
		UntestedSymbols: []string{"Auth"},
	}

	guidance := GenerateRiskGuidance(input)
	if guidance.RiskLevel != "high" {
		t.Errorf("expected high risk, got %s", guidance.RiskLevel)
	}
	if len(guidance.Flags) == 0 {
		t.Error("expected at least one flag")
	}
}

func TestGenerateRiskGuidanceLow(t *testing.T) {
	t.Parallel()
	input := RiskInput{
		ChangedSymbols: []ChangedSymbol{
			{Symbol: &parser.Symbol{Name: "helper"}, ChangeType: ChangeModified},
		},
		ImpactResults: map[string]*impact.Result{
			"helper": {Symbol: "helper", Found: true, TotalAffected: 1, BlastRadius: "low"},
		},
	}

	guidance := GenerateRiskGuidance(input)
	if guidance.RiskLevel == "high" {
		t.Error("should not be high risk")
	}
}
