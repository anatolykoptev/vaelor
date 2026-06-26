// archgraph_fallback.go — in-memory fallback for FallbackArchMetrics

package compare

import (
	"context"
	"path/filepath"
	"time"

	"github.com/anatolykoptev/go-code/internal/callgraph"
)

const fallbackArchTimeout = 10 * time.Second

// FallbackArchMetrics builds an in-memory call graph with a 10-second timeout
// and derives PackageCount and CrossPkgCallRatio from it.  It is called when
// CollectArchMetrics returns NotIndexed (AGE graph absent or repo not yet indexed).
//
// MaxCallDepth, InterfaceRatio, and CommunityCount are not computed here — they
// are too expensive without the persistent graph store. Callers must set
// HintApproxArchMetrics on the returned struct to indicate the metrics are approximate.
//
// Returns nil when root is empty or when BuildFromRepo fails (caller should
// fall back to NotIndexed=true in that case).
func FallbackArchMetrics(ctx context.Context, root string) *ArchMetrics {
	if root == "" {
		return nil
	}

	tCtx, cancel := context.WithTimeout(ctx, fallbackArchTimeout)
	defer cancel()

	cg, err := callgraph.BuildFromRepo(tCtx, callgraph.TraceRepoInput{Root: root})
	if err != nil || cg == nil {
		// BuildFromRepo failed (timeout, parse error, empty repo) — no data at all.
		// Return nil so callers can set NotIndexed=true instead of returning
		// an all-zero struct that looks like real metrics.
		return nil
	}

	// Collect unique packages from caller symbols.
	pkgs := make(map[string]struct{})
	for i := range cg.Symbols {
		sym := cg.Symbols[i]
		if sym != nil && sym.File != "" {
			pkg := filepath.Dir(sym.File)
			pkgs[pkg] = struct{}{}
		}
	}

	// Count cross-package edges: an edge is cross-pkg when caller and callee
	// reside in different directories (packages).
	var total, crossPkg int
	for i := range cg.Edges {
		e := cg.Edges[i]
		if e.Caller == nil || e.Callee == nil {
			continue
		}
		total++
		callerPkg := filepath.Dir(e.Caller.File)
		calleePkg := filepath.Dir(e.Callee.File)
		if callerPkg != calleePkg {
			crossPkg++
		}
	}

	var crossPkgRatio float64
	if total > 0 {
		crossPkgRatio = float64(crossPkg) / float64(total)
	}

	return &ArchMetrics{
		PackageCount:      len(pkgs),
		CrossPkgCallRatio: crossPkgRatio,
	}
}
