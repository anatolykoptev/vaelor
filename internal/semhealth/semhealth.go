// Package semhealth provides semantic health analysis bridging
// embeddings search with code quality metrics.
package semhealth

import (
	"context"
	"log/slog"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/embeddings"
)

// SimilarPairFinder abstracts the pgvector self-join query so Analyze can be
// tested without a real database. Production callers pass *embeddings.Store.
type SimilarPairFinder interface {
	FindSimilarPairs(ctx context.Context, opts embeddings.SimilarPairOpts) ([]embeddings.SimilarPair, error)
}

const (
	// semhealthMaxFuncs is the repo-size guard for the O(n²) similarity self-join.
	// Repos with more functions than this threshold skip FindSimilarPairs to avoid
	// pinning a CPU core on the 4-core ARM box.
	// At 5000 functions the self-join produces up to 12.5M candidate pairs, which
	// is near the tip-over point observed in the 2026-05-27 incident (24k embeddings
	// drove 287M pairs, CPU PSI some avg10 = 85 for 18+ min).
	semhealthMaxFuncs = 5000
)

// SemanticResult holds semantic analysis results for a repository.
type SemanticResult struct {
	SemanticDupRatio float64    // fraction of functions involved in semantic duplication
	DupGroups        []DupGroup // groups of semantically similar functions
}

// DupGroup is a cluster of semantically similar functions.
type DupGroup struct {
	Symbols       []DupSymbol
	AvgSimilarity float32
	// Tier classifies the group: "exact", "very-close", or "related".
	// Empty string for groups produced by Analyze (pre-triage path).
	Tier string
}

// DupSymbol identifies a function in a duplicate group.
type DupSymbol struct {
	Name string
	File string
	Line int
	// Kind is the symbol kind (e.g. "function", "method").
	// Empty string for symbols produced by Analyze (pre-triage path).
	Kind string
}

// ComputeSemanticDupRatio computes the fraction of functions involved in
// semantic duplication. Each unique symbol appearing in any pair counts once.
func ComputeSemanticDupRatio(pairs []embeddings.SimilarPair, totalFuncs int) float64 {
	if totalFuncs == 0 || len(pairs) == 0 {
		return 0
	}
	unique := make(map[string]struct{})
	for _, p := range pairs {
		unique[p.FileA+":"+p.SymbolA] = struct{}{}
		unique[p.FileB+":"+p.SymbolB] = struct{}{}
	}
	return float64(len(unique)) / float64(totalFuncs)
}

// CollectDupGroups clusters similar pairs into groups using union-find.
// Pairs sharing a symbol are merged into the same group.
func CollectDupGroups(pairs []embeddings.SimilarPair) []DupGroup {
	if len(pairs) == 0 {
		return nil
	}

	parent := make(map[string]string)
	symInfo := make(map[string]DupSymbol)

	find := func(x string) string {
		for parent[x] != x {
			parent[x] = parent[parent[x]]
			x = parent[x]
		}
		return x
	}
	union := func(a, b string) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}

	// Track similarity sums per root for averaging.
	type simAccum struct {
		sum   float32
		count int
	}
	accum := make(map[string]*simAccum)

	for _, p := range pairs {
		keyA := p.FileA + ":" + p.SymbolA
		keyB := p.FileB + ":" + p.SymbolB
		if _, ok := parent[keyA]; !ok {
			parent[keyA] = keyA
			symInfo[keyA] = DupSymbol{Name: p.SymbolA, File: p.FileA, Line: p.LineA}
		}
		if _, ok := parent[keyB]; !ok {
			parent[keyB] = keyB
			symInfo[keyB] = DupSymbol{Name: p.SymbolB, File: p.FileB, Line: p.LineB}
		}
		union(keyA, keyB)
	}

	// Accumulate similarities by final root.
	for _, p := range pairs {
		keyA := p.FileA + ":" + p.SymbolA
		root := find(keyA)
		if _, ok := accum[root]; !ok {
			accum[root] = &simAccum{}
		}
		accum[root].sum += p.Similarity
		accum[root].count++
	}

	// Collect groups by root.
	groups := make(map[string][]string)
	for key := range parent {
		root := find(key)
		groups[root] = append(groups[root], key)
	}

	result := make([]DupGroup, 0, len(groups))
	for root, members := range groups {
		g := DupGroup{
			Symbols: make([]DupSymbol, len(members)),
		}
		for i, m := range members {
			g.Symbols[i] = symInfo[m]
		}
		if a, ok := accum[root]; ok && a.count > 0 {
			g.AvgSimilarity = a.sum / float32(a.count)
		}
		result = append(result, g)
	}

	// Sort by group size descending.
	for i := 1; i < len(result); i++ {
		for j := i; j > 0 && len(result[j].Symbols) > len(result[j-1].Symbols); j-- {
			result[j], result[j-1] = result[j-1], result[j]
		}
	}

	return result
}

// Analyze runs semantic health analysis for a repo.
// Returns nil result (not error) if embeddings are unavailable.
// Returns an empty &SemanticResult{} if the repo exceeds semhealthMaxFuncs
// to avoid the O(n²) pgvector self-join pinning a CPU core on large repos.
func Analyze(ctx context.Context, store SimilarPairFinder, repoKey string, totalFuncs int) *SemanticResult {
	if store == nil || repoKey == "" || totalFuncs == 0 {
		return nil
	}

	// Repo-size guard: skip the O(n²) self-join for large repos.
	// At semhealthMaxFuncs=5000 functions the self-join produces up to 12.5M
	// candidate pairs — near the tip-over observed in the 2026-05-27 incident.
	if totalFuncs > semhealthMaxFuncs {
		slog.Info("semhealth: skipping similarity analysis, repo too large",
			slog.Int("totalFuncs", totalFuncs),
			slog.Int("threshold", semhealthMaxFuncs))
		return &SemanticResult{}
	}

	pairs, err := store.FindSimilarPairs(ctx, embeddings.SimilarPairOpts{
		RepoKey: repoKey,
	})
	if err != nil {
		slog.Debug("semhealth: find similar pairs failed",
			slog.String("repo", repoKey), slog.Any("error", err))
		return nil
	}

	if len(pairs) == 0 {
		return &SemanticResult{}
	}

	return &SemanticResult{
		SemanticDupRatio: ComputeSemanticDupRatio(pairs, totalFuncs),
		DupGroups:        CollectDupGroups(pairs),
	}
}

// FormatDupGroupMessage formats a duplicate group for recommendation output.
func FormatDupGroupMessage(g DupGroup) string {
	var sb strings.Builder
	for i, s := range g.Symbols {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(s.Name)
		sb.WriteString(" (")
		sb.WriteString(s.File)
		sb.WriteString(")")
	}
	return sb.String()
}
