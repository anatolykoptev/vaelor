package compare

import (
	"context"
	"net/http"
	"time"

	"github.com/anatolykoptev/go-code/internal/freshness"
)

// FreshnessStats holds dependency freshness and vulnerability data.
type FreshnessStats struct {
	DepFreshnessRatio float64 `json:"depFreshnessRatio"`
	VulnSecurityRatio float64 `json:"vulnSecurityRatio"`
	TotalDeps         int     `json:"totalDeps"`
	OutdatedDeps      int     `json:"outdatedDeps"`
	VulnDeps          int     `json:"vulnDeps"`
}

// CollectFreshness checks dependency freshness and vulnerabilities for a repo.
// Returns nil if no manifests found. depRatio and vulnRatio contain the ratios.
func CollectFreshness(ctx context.Context, root string) (*FreshnessStats, float64, float64) {
	manifests := freshness.DiscoverManifests(root)
	if len(manifests) == 0 {
		return nil, 0, 0
	}

	allDeps := freshness.CollectDeps(manifests)
	if len(allDeps) == 0 {
		return nil, 0, 0
	}

	client := &http.Client{Timeout: 5 * time.Second}
	stats := &FreshnessStats{}

	reg := freshness.NewMultiRegistry(client)
	if fr := freshness.CheckFreshness(ctx, allDeps, reg); fr != nil {
		stats.DepFreshnessRatio = fr.Ratio
		stats.TotalDeps = fr.Total
		stats.OutdatedDeps = fr.Total - fr.UpToDate
	}

	if vr := freshness.CheckVulnerabilities(ctx, allDeps, client, freshness.DefaultOSVURL); vr != nil {
		stats.VulnSecurityRatio = vr.Ratio
		stats.VulnDeps = vr.Vulnerable
	}

	return stats, stats.DepFreshnessRatio, stats.VulnSecurityRatio
}
