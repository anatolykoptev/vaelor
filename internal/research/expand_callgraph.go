package research

import (
	"github.com/anatolykoptev/vaelor/internal/callgraph"
)

// expandFromCallGraph performs BFS over call edges starting from seed files.
// For each seed file, walks both directions:
//   - forward (callees): files containing functions called BY the seed
//   - reverse (callers): files containing functions that INVOKE a seed symbol
//
// Returns expandResult entries tagged with "calls X" / "called by X".
// Seed files themselves are not returned.
func expandFromCallGraph(
	seedFiles map[string]bool,
	g *callgraph.CallGraph,
	maxHops int,
) []expandResult {
	if g == nil || len(g.Edges) == 0 || maxHops <= 0 {
		return nil
	}

	type fileEdge struct {
		dst       string
		viaSymbol string
	}
	forward := make(map[string][]fileEdge) // file → files it calls into
	reverse := make(map[string][]fileEdge) // file → files that call it
	for _, e := range g.Edges {
		if e.Caller == nil || e.Callee == nil || e.Caller.File == "" || e.Callee.File == "" {
			continue
		}
		if e.Caller.File == e.Callee.File {
			continue // intra-file edge
		}
		forward[e.Caller.File] = append(forward[e.Caller.File], fileEdge{e.Callee.File, e.CalleeName})
		reverse[e.Callee.File] = append(reverse[e.Callee.File], fileEdge{e.Caller.File, e.CalleeName})
	}

	type qitem struct {
		file string
		dist int
		dir  string // "callees" or "callers"
	}
	visited := make(map[string]expandResult)
	queue := make([]qitem, 0, len(seedFiles)*4)
	for f := range seedFiles {
		queue = append(queue,
			qitem{f, 0, "callers"},
			qitem{f, 0, "callees"},
		)
	}

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		if item.dist >= maxHops {
			continue
		}
		var edges []fileEdge
		if item.dir == "callees" {
			edges = forward[item.file]
		} else {
			edges = reverse[item.file]
		}
		for _, e := range edges {
			if seedFiles[e.dst] {
				continue
			}
			if _, ok := visited[e.dst]; ok {
				continue
			}
			why := "calls " + e.viaSymbol
			if item.dir == "callers" {
				why = "called by " + e.viaSymbol
			}
			visited[e.dst] = expandResult{
				relPath:   e.dst,
				distance:  item.dist + 1,
				whyLinked: why,
			}
			queue = append(queue, qitem{e.dst, item.dist + 1, item.dir})
		}
	}

	out := make([]expandResult, 0, len(visited))
	for _, r := range visited {
		out = append(out, r)
	}
	return out
}

// mergeExpandResults combines two expandResult slices. When the same file
// appears in both, the entry with the smaller distance wins (and its
// whyLinked is preserved — typically "calls X" beats "imports foo" at
// shorter distance).
func mergeExpandResults(a, b []expandResult) []expandResult {
	seen := make(map[string]int, len(a)+len(b))
	out := make([]expandResult, 0, len(a)+len(b))
	for _, r := range a {
		seen[r.relPath] = len(out)
		out = append(out, r)
	}
	for _, r := range b {
		if idx, ok := seen[r.relPath]; ok {
			if r.distance < out[idx].distance {
				out[idx] = r
			}
			continue
		}
		seen[r.relPath] = len(out)
		out = append(out, r)
	}
	return out
}
