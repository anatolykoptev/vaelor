package explore

import (
	"path/filepath"
	"sort"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/ranking"
)

const maxCommunitySymbols = 5
const maxClusters = 10

// minCommunitySize filters out trivial Louvain singletons/pairs that pollute
// the cluster list — they are an artefact of disconnected nodes in the call
// graph rather than meaningful structural groups.
const minCommunitySize = 3

// CommunityOverview summarizes Louvain community detection results.
type CommunityOverview struct {
	Count    int                `json:"count"`
	Clusters []CommunityCluster `json:"clusters"`
}

// CommunityCluster describes one community by its top members.
type CommunityCluster struct {
	ID      int      `json:"id"`
	Size    int      `json:"size"`
	Symbols []string `json:"symbols"`
}

// buildCommunityOverview runs Louvain on the call graph edges and returns
// a summary of detected communities. Returns nil if fewer than 2 communities.
func buildCommunityOverview(cg *callgraph.CallGraph, root string) *CommunityOverview {
	graph := make(map[string]map[string]int)
	for _, sym := range cg.Symbols {
		key := symKey(sym, root)
		if graph[key] == nil {
			graph[key] = make(map[string]int)
		}
	}
	for _, edge := range cg.Edges {
		if edge.Caller == nil || edge.Callee == nil {
			continue
		}
		from := symKey(edge.Caller, root)
		to := symKey(edge.Callee, root)
		if from == to {
			continue
		}
		if graph[from] == nil {
			graph[from] = make(map[string]int)
		}
		graph[from][to]++
		if graph[to] == nil {
			graph[to] = make(map[string]int)
		}
		graph[to][from]++
	}

	if len(graph) < 3 {
		return nil
	}

	communities := ranking.LouvainWeighted(graph)
	if communities == nil {
		return nil
	}

	groups := make(map[int][]string)
	for key, comm := range communities {
		groups[comm] = append(groups[comm], key)
	}

	if len(groups) < 2 {
		return nil
	}

	type cEntry struct {
		id      int
		members []string
	}
	entries := make([]cEntry, 0, len(groups))
	for id, members := range groups {
		sort.Strings(members)
		entries = append(entries, cEntry{id: id, members: members})
	}
	sort.Slice(entries, func(i, j int) bool {
		return len(entries[i].members) > len(entries[j].members)
	})

	// Drop singletons/pairs — they pollute the cluster list without
	// signalling structure. Both Count and Clusters reflect the
	// non-trivial groups so the agent sees a consistent picture.
	nonTrivial := entries[:0]
	for _, e := range entries {
		if len(e.members) >= minCommunitySize {
			nonTrivial = append(nonTrivial, e)
		}
	}
	if len(nonTrivial) < 2 {
		return nil
	}

	limit := maxClusters
	if len(nonTrivial) < limit {
		limit = len(nonTrivial)
	}

	clusters := make([]CommunityCluster, limit)
	for i := range limit {
		e := nonTrivial[i]
		syms := e.members
		if len(syms) > maxCommunitySymbols {
			syms = syms[:maxCommunitySymbols]
		}
		clusters[i] = CommunityCluster{
			ID:      e.id,
			Size:    len(e.members),
			Symbols: syms,
		}
	}

	return &CommunityOverview{
		Count:    len(nonTrivial),
		Clusters: clusters,
	}
}

func symKey(sym *parser.Symbol, root string) string {
	file := sym.File
	if rel, err := filepath.Rel(root, file); err == nil {
		file = rel
	}
	return sym.Name + ":" + file
}
