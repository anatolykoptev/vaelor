package review

import (
	"fmt"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/impact"
)

const (
	wideBlastThreshold   = 20
	crossPkgThreshold    = 3
	manyChangesThreshold = 10
)

type RiskInput struct {
	ChangedSymbols  []ChangedSymbol
	ImpactResults   map[string]*impact.Result
	UntestedSymbols []string
	HasInheritance  bool
}

type RiskGuidance struct {
	RiskLevel   string   `json:"risk_level"`
	RiskScore   float64  `json:"risk_score"`
	Flags       []string `json:"flags"`
	Suggestions []string `json:"suggestions"`
}

func GenerateRiskGuidance(input RiskInput) RiskGuidance {
	var flags []string
	var suggestions []string
	var maxRisk float64

	for _, ir := range input.ImpactResults {
		if ir.TotalAffected >= wideBlastThreshold {
			flags = append(flags, fmt.Sprintf("Wide blast radius: %s affects %d symbols across %d packages",
				ir.Symbol, ir.TotalAffected, len(ir.AffectedPackages)))
			suggestions = append(suggestions, "Scrutinize all callers and dependents for side effects")
		}
		if ir.RiskScore > maxRisk {
			maxRisk = ir.RiskScore
		}
	}

	if len(input.UntestedSymbols) > 0 {
		flags = append(flags, fmt.Sprintf("Untested changes: %s lack test coverage",
			strings.Join(input.UntestedSymbols, ", ")))
		suggestions = append(suggestions, "Add tests for untested symbols before merging")
	}

	totalPkgs := countUniquePkgs(input.ImpactResults)
	if totalPkgs >= crossPkgThreshold {
		flags = append(flags, fmt.Sprintf("Cross-package impact: changes affect %d packages", totalPkgs))
		suggestions = append(suggestions, "Consider splitting into smaller, focused PRs")
	}

	if input.HasInheritance {
		flags = append(flags, "Inheritance/interface changes detected")
		suggestions = append(suggestions, "Verify Liskov substitution principle compliance")
	}

	if len(input.ChangedSymbols) >= manyChangesThreshold {
		flags = append(flags, fmt.Sprintf("Large changeset: %d symbols modified", len(input.ChangedSymbols)))
	}

	level := classifyRisk(len(flags), maxRisk)

	return RiskGuidance{
		RiskLevel:   level,
		RiskScore:   maxRisk,
		Flags:       flags,
		Suggestions: suggestions,
	}
}

func classifyRisk(flagCount int, maxRiskScore float64) string {
	if flagCount >= 3 || maxRiskScore >= 20 {
		return "high"
	}
	if flagCount >= 1 || maxRiskScore >= 5 {
		return "medium"
	}
	return "low"
}

func countUniquePkgs(results map[string]*impact.Result) int {
	pkgs := make(map[string]bool)
	for _, r := range results {
		for _, p := range r.AffectedPackages {
			pkgs[p] = true
		}
	}
	return len(pkgs)
}
