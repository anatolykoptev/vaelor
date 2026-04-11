package compare

import (
	"fmt"
	"strings"
)

func writeFreshness(sb *strings.Builder, a, b *FreshnessStats) {
	if a == nil && b == nil {
		return
	}
	sb.WriteString("## Dependency Health\n\n")
	if a != nil {
		fmt.Fprintf(sb, "**Repo A**: %.0f%% deps up-to-date (%d/%d outdated), %d CVE vulnerabilities\n",
			a.DepFreshnessRatio*100, a.OutdatedDeps, a.TotalDeps, a.VulnDeps)
	}
	if b != nil {
		fmt.Fprintf(sb, "**Repo B**: %.0f%% deps up-to-date (%d/%d outdated), %d CVE vulnerabilities\n",
			b.DepFreshnessRatio*100, b.OutdatedDeps, b.TotalDeps, b.VulnDeps)
	}
	sb.WriteString("\n")
}

func writeDataflow(sb *strings.Builder, a, b *DataflowStats) {
	if a == nil && b == nil {
		return
	}
	sb.WriteString("## Code Quality (Static Analysis)\n\n")
	if a != nil {
		fmt.Fprintf(sb, "**Repo A**: %d dead stores, %d unused variables (%d total findings, %d files)\n",
			a.DeadStores, a.UnusedVars, a.TotalFindings, a.FilesAnalyzed)
	}
	if b != nil {
		fmt.Fprintf(sb, "**Repo B**: %d dead stores, %d unused variables (%d total findings, %d files)\n",
			b.DeadStores, b.UnusedVars, b.TotalFindings, b.FilesAnalyzed)
	}
	sb.WriteString("\n")
}

func writeAPISurface(sb *strings.Builder, diff *APIDiff) {
	if diff == nil {
		return
	}
	sb.WriteString("## API Compatibility\n\n")
	fmt.Fprintf(sb, "Exported symbols: %d common, %d only in repo A, %d only in repo B, %d with changed signatures\n\n",
		diff.Common, diff.OnlyACount, diff.OnlyBCount, diff.ChangedSig)
	limit := min(len(diff.Changed), 10)
	if limit > 0 {
		sb.WriteString("**Signature changes:**\n")
		for _, c := range diff.Changed[:limit] {
			fmt.Fprintf(sb, "- `%s` (%s): `%s` → `%s`\n", c.Name, c.Kind, c.SigA, c.SigB)
		}
		sb.WriteString("\n")
	}
}

func writeRoutesDiff(sb *strings.Builder, diff *RouteDiff) {
	if diff == nil {
		return
	}
	sb.WriteString("## HTTP Routes\n\n")
	fmt.Fprintf(sb, "%d common, %d only in repo A, %d only in repo B\n\n", diff.Common, diff.OnlyACount, diff.OnlyBCount)
	if len(diff.OnlyA) > 0 {
		sb.WriteString("**Only in repo A:**\n")
		for _, r := range diff.OnlyA {
			fmt.Fprintf(sb, "- %s %s (%s)\n", r.Method, r.Path, r.Handler)
		}
		sb.WriteString("\n")
	}
	if len(diff.OnlyB) > 0 {
		sb.WriteString("**Only in repo B:**\n")
		for _, r := range diff.OnlyB {
			fmt.Fprintf(sb, "- %s %s (%s)\n", r.Method, r.Path, r.Handler)
		}
		sb.WriteString("\n")
	}
}
