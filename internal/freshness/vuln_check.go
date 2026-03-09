package freshness

import (
	"context"
	"net/http"
	"sync"
)

// DefaultOSVURL is the default endpoint for the OSV.dev vulnerability API.
const DefaultOSVURL = "https://api.osv.dev/v1/query"

// osvEcosystems maps internal language identifiers to OSV ecosystem names.
var osvEcosystems = map[string]string{
	"go":         "Go",
	"npm":        "npm",
	"typescript": "npm",
	"python":     "PyPI",
	"rust":       "crates.io",
	"java":       "Maven",
	"ruby":       "RubyGems",
	"csharp":     "NuGet",
}

// VulnResult summarizes vulnerability scan results for a set of dependencies.
type VulnResult struct {
	Total      int       `json:"total"`
	Vulnerable int       `json:"vulnerable"`
	Critical   int       `json:"critical"`
	High       int       `json:"high"`
	Medium     int       `json:"medium"`
	Low        int       `json:"low"`
	Ratio      float64   `json:"ratio"`
	Vulns      []VulnDep `json:"vulns,omitempty"`
}

// VulnDep describes a single vulnerability found in a dependency.
type VulnDep struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	ID       string `json:"id"`
	Severity string `json:"severity"`
	Summary  string `json:"summary"`
}

// vulnDepResult holds the outcome of a single dependency vulnerability check.
type vulnDepResult struct {
	checked bool
	vulns   []VulnDep
}

// CheckVulnerabilities checks all dependencies against OSV.dev concurrently.
func CheckVulnerabilities(
	ctx context.Context,
	deps []Dependency,
	client *http.Client,
	osvURL string,
) *VulnResult {
	if len(deps) == 0 {
		return &VulnResult{Ratio: 1.0}
	}

	results := make([]vulnDepResult, len(deps))
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrency)

	for i, dep := range deps {
		eco, ok := osvEcosystems[dep.Language]
		if !ok || dep.Version == "" {
			continue
		}

		wg.Add(1)
		go func(idx int, d Dependency, ecosystem string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			lookupCtx, cancel := context.WithTimeout(ctx, perLookupTimeout)
			defer cancel()

			version := stripVersionPrefix(d.Version)
			vulns, err := queryOSV(lookupCtx, client, osvURL, d.Name, version, ecosystem)
			if err != nil {
				results[idx] = vulnDepResult{checked: true}
				return
			}
			results[idx] = vulnDepResult{checked: true, vulns: vulns}
		}(i, dep, eco)
	}

	wg.Wait()
	return aggregateVulnResults(results)
}

// aggregateVulnResults computes totals from per-dependency vulnerability results.
func aggregateVulnResults(results []vulnDepResult) *VulnResult {
	var vr VulnResult
	for _, r := range results {
		if !r.checked {
			continue
		}
		vr.Total++
		if len(r.vulns) > 0 {
			vr.Vulnerable++
			for _, v := range r.vulns {
				vr.Vulns = append(vr.Vulns, v)
				switch v.Severity {
				case sevCritical:
					vr.Critical++
				case sevHigh:
					vr.High++
				case sevMedium:
					vr.Medium++
				case sevLow:
					vr.Low++
				}
			}
		}
	}

	if vr.Total > 0 {
		vr.Ratio = 1.0 - float64(vr.Vulnerable)/float64(vr.Total)
	} else {
		vr.Ratio = 1.0
	}
	return &vr
}
