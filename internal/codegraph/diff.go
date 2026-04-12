package codegraph

import (
	"fmt"
	"sort"
)

// GraphDiff holds the result of comparing two code graph snapshots.
type GraphDiff struct {
	AddedSymbols        []SnapshotSymbol   `json:"added_symbols"`
	RemovedSymbols      []SnapshotSymbol   `json:"removed_symbols"`
	AddedEdges          []SnapshotEdge     `json:"added_edges"`
	RemovedEdges        []SnapshotEdge     `json:"removed_edges"`
	CommunityMigrations []CommunityChange  `json:"community_migrations,omitempty"`
	ComplexityChanges   []ComplexityChange `json:"complexity_changes,omitempty"`
	Summary             string             `json:"summary"`
}

// CommunityChange records a symbol that moved between communities.
type CommunityChange struct {
	Name         string `json:"name"`
	File         string `json:"file"`
	OldCommunity int    `json:"old_community"`
	NewCommunity int    `json:"new_community"`
}

// ComplexityChange records a symbol whose complexity score changed.
type ComplexityChange struct {
	Name          string `json:"name"`
	File          string `json:"file"`
	OldComplexity int    `json:"old_complexity"`
	NewComplexity int    `json:"new_complexity"`
}

// symKey returns a stable identity key for a symbol.
func symKey(s SnapshotSymbol) string {
	return s.Name + ":" + s.File
}

// edgeKey returns a stable identity key for an edge.
func edgeKey(e SnapshotEdge) string {
	return e.From + "|" + e.Label + "|" + e.To
}

// DiffGraphs compares two snapshots and returns the diff.
func DiffGraphs(old, new_ Snapshot) GraphDiff {
	var d GraphDiff

	// --- symbol set diff ---
	oldSymMap := make(map[string]SnapshotSymbol, len(old.Symbols))
	for _, s := range old.Symbols {
		oldSymMap[symKey(s)] = s
	}
	newSymMap := make(map[string]SnapshotSymbol, len(new_.Symbols))
	for _, s := range new_.Symbols {
		newSymMap[symKey(s)] = s
	}

	for k, s := range newSymMap {
		if _, exists := oldSymMap[k]; !exists {
			d.AddedSymbols = append(d.AddedSymbols, s)
		}
	}
	for k, s := range oldSymMap {
		if _, exists := newSymMap[k]; !exists {
			d.RemovedSymbols = append(d.RemovedSymbols, s)
		}
	}

	// --- edge set diff ---
	oldEdgeMap := make(map[string]SnapshotEdge, len(old.Edges))
	for _, e := range old.Edges {
		oldEdgeMap[edgeKey(e)] = e
	}
	newEdgeMap := make(map[string]SnapshotEdge, len(new_.Edges))
	for _, e := range new_.Edges {
		newEdgeMap[edgeKey(e)] = e
	}

	for k, e := range newEdgeMap {
		if _, exists := oldEdgeMap[k]; !exists {
			d.AddedEdges = append(d.AddedEdges, e)
		}
	}
	for k, e := range oldEdgeMap {
		if _, exists := newEdgeMap[k]; !exists {
			d.RemovedEdges = append(d.RemovedEdges, e)
		}
	}

	// --- community migrations and complexity changes (symbols present in both) ---
	for k, oldSym := range oldSymMap {
		newSym, exists := newSymMap[k]
		if !exists {
			continue
		}
		if oldSym.Community != newSym.Community {
			d.CommunityMigrations = append(d.CommunityMigrations, CommunityChange{
				Name:         oldSym.Name,
				File:         oldSym.File,
				OldCommunity: oldSym.Community,
				NewCommunity: newSym.Community,
			})
		}
		if oldSym.Complexity != newSym.Complexity && !(oldSym.Complexity == 0 && newSym.Complexity == 0) {
			d.ComplexityChanges = append(d.ComplexityChanges, ComplexityChange{
				Name:          oldSym.Name,
				File:          oldSym.File,
				OldComplexity: oldSym.Complexity,
				NewComplexity: newSym.Complexity,
			})
		}
	}

	// --- sort all slices for deterministic output ---
	sort.Slice(d.AddedSymbols, func(i, j int) bool {
		return symKey(d.AddedSymbols[i]) < symKey(d.AddedSymbols[j])
	})
	sort.Slice(d.RemovedSymbols, func(i, j int) bool {
		return symKey(d.RemovedSymbols[i]) < symKey(d.RemovedSymbols[j])
	})
	sort.Slice(d.AddedEdges, func(i, j int) bool {
		return edgeKey(d.AddedEdges[i]) < edgeKey(d.AddedEdges[j])
	})
	sort.Slice(d.RemovedEdges, func(i, j int) bool {
		return edgeKey(d.RemovedEdges[i]) < edgeKey(d.RemovedEdges[j])
	})
	sort.Slice(d.CommunityMigrations, func(i, j int) bool {
		ki := d.CommunityMigrations[i].Name + ":" + d.CommunityMigrations[i].File
		kj := d.CommunityMigrations[j].Name + ":" + d.CommunityMigrations[j].File
		return ki < kj
	})
	sort.Slice(d.ComplexityChanges, func(i, j int) bool {
		ki := d.ComplexityChanges[i].Name + ":" + d.ComplexityChanges[i].File
		kj := d.ComplexityChanges[j].Name + ":" + d.ComplexityChanges[j].File
		return ki < kj
	})

	d.Summary = buildDiffSummary(d)
	return d
}

// buildDiffSummary produces a human-readable summary of the diff.
func buildDiffSummary(d GraphDiff) string {
	added := len(d.AddedSymbols)
	removed := len(d.RemovedSymbols)
	addedEdges := len(d.AddedEdges)
	removedEdges := len(d.RemovedEdges)
	migrations := len(d.CommunityMigrations)
	complexityChg := len(d.ComplexityChanges)

	if added == 0 && removed == 0 && addedEdges == 0 && removedEdges == 0 &&
		migrations == 0 && complexityChg == 0 {
		return "no changes"
	}

	parts := []string{}
	if added > 0 {
		parts = append(parts, fmt.Sprintf("%d new symbol%s", added, plural(added)))
	}
	if removed > 0 {
		parts = append(parts, fmt.Sprintf("%d symbol%s removed", removed, plural(removed)))
	}
	if addedEdges > 0 {
		parts = append(parts, fmt.Sprintf("%d new edge%s", addedEdges, plural(addedEdges)))
	}
	if removedEdges > 0 {
		parts = append(parts, fmt.Sprintf("%d edge%s removed", removedEdges, plural(removedEdges)))
	}
	if migrations > 0 {
		parts = append(parts, fmt.Sprintf("%d community migration%s", migrations, plural(migrations)))
	}
	if complexityChg > 0 {
		parts = append(parts, fmt.Sprintf("%d complexity change%s", complexityChg, plural(complexityChg)))
	}

	result := ""
	for i, p := range parts {
		if i == 0 {
			result = p
		} else {
			result += ", " + p
		}
	}
	return result
}

// plural returns "" for n==1 and "s" otherwise.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
