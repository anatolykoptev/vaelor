package research

import (
	"fmt"
	"strings"
)

// importNode implements goutil.DAGNode over a package entry.
// Used internally for cycle detection; the actual BFS expansion is simpler.
type importNode struct {
	id   string
	deps []string
}

func (n importNode) NodeID() string     { return n.id }
func (n importNode) NodeDeps() []string { return n.deps }

// expandResult holds the outcome of DAG expansion for one file.
type expandResult struct {
	relPath   string
	distance  int
	whyLinked string
}

// expandFromSeeds performs BFS over the import graph starting from seed files,
// returning all reachable files within maxHops in both directions (importers and importees).
//
// importGraph maps relPath → []relPath (files this file imports, local only).
// seedFiles is the set of seed relPaths.
func expandFromSeeds(
	seedFiles map[string]bool,
	importGraph map[string][]string, // file → files it imports
	maxHops int,
) []expandResult {
	if maxHops <= 0 {
		maxHops = DefaultExpandHops
	}

	// Build reverse graph: file → files that import it.
	reverseGraph := make(map[string][]string, len(importGraph))
	for src, targets := range importGraph {
		for _, dst := range targets {
			reverseGraph[dst] = append(reverseGraph[dst], src)
		}
	}

	type qitem struct {
		file      string
		distance  int
		direction string // "down" (imports) or "up" (importers)
		via       string // seed or intermediate file that caused this
	}

	visited := make(map[string]expandResult)
	queue := make([]qitem, 0, len(seedFiles)*4)

	// Seed files are distance=0.
	for f := range seedFiles {
		visited[f] = expandResult{relPath: f, distance: 0, whyLinked: "seed"}
		// Expand both ways from each seed.
		queue = append(queue,
			qitem{file: f, distance: 0, direction: "down", via: f},
			qitem{file: f, distance: 0, direction: "up", via: f},
		)
	}

	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]

		if item.distance >= maxHops {
			continue
		}

		var neighbours []string
		if item.direction == "down" {
			neighbours = importGraph[item.file]
		} else {
			neighbours = reverseGraph[item.file]
		}

		for _, neighbour := range neighbours {
			if _, alreadySeen := visited[neighbour]; alreadySeen {
				continue
			}
			dist := item.distance + 1
			why := buildWhyLinked(item.direction, item.file, neighbour)
			visited[neighbour] = expandResult{
				relPath:   neighbour,
				distance:  dist,
				whyLinked: why,
			}
			queue = append(queue,
				qitem{file: neighbour, distance: dist, direction: item.direction, via: neighbour},
			)
		}
	}

	// Return all non-seed results sorted by distance then path.
	out := make([]expandResult, 0, len(visited))
	for _, r := range visited {
		out = append(out, r)
	}
	// Stable sort: distance asc, then path asc.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && (out[j].distance < out[j-1].distance ||
			(out[j].distance == out[j-1].distance && out[j].relPath < out[j-1].relPath)); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func buildWhyLinked(direction, via, neighbour string) string {
	if direction == "down" {
		return fmt.Sprintf("imported by %s", shortPath(via))
	}
	return fmt.Sprintf("imports %s", shortPath(via))
}

func shortPath(p string) string {
	// Return last two path segments for readability.
	parts := strings.Split(p, "/")
	if len(parts) <= 2 {
		return p
	}
	return strings.Join(parts[len(parts)-2:], "/")
}
