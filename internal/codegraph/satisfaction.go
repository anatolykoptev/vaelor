package codegraph

import (
	"context"
	"log/slog"
	"time"

	"github.com/anatolykoptev/go-code/internal/goanalysis"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// implementsLoadTimeout bounds the synchronous go/packages load used to compute
// interface satisfaction at index time.
//
// This is a DEDICATED typed load, not a free or incremental reuse of existing
// analysis: the IndexRepo path builds its call graph from the tree-sitter symbol
// table (callgraph.BuildCallGraph in index.go), NOT a go/packages typed load, so
// no warmed go/types state exists to piggyback on here. extractGoImplements runs a
// fresh full NeedDeps load via goanalysis.LoadPackages — cold (NeedDeps) loads can
// run minutes, so the indexer must not block on it. On timeout we emit ZERO
// IMPLEMENTS edges and the dependent find_duplicates filter degrades to its
// signature heuristic — never worse than before this pass existed.
//
// (The callgraph package's separate type-aware call resolution uses a 10s warm-path
// bound in callgraph/repo.go; that is a different code path with its own load.)
const implementsLoadTimeout = 30 * time.Second

// extractGoImplements computes structural interface-satisfaction relationships
// for a Go module via go/types and returns them as parser.TypeRelationship values
// of kind RelImplements, ready to flow through the existing buildRelationshipEdges
// path (which emits IMPLEMENTS Symbol→Symbol edges). One edge is produced per
// (concrete type T, interface I) where T or *T implements I.
//
// Go-only and best-effort: returns nil (not an error) when the repo has no
// go.mod, when go/packages fails or times out, or when no satisfaction exists.
// Failures are non-fatal to indexing — they bump a counter and log, mirroring the
// graceful-degradation contract of the rest of the index path. The returned
// relationships' File field is the concrete type's ABSOLUTE declaration path, so
// buildRelationshipEdges keys the IMPLEMENTS edge's subject endpoint onto the same
// Symbol vertex (name + repo-relative file) that buildSymbolGraph created.
func extractGoImplements(ctx context.Context, root string) []parser.TypeRelationship {
	if !goanalysis.HasGoModule(root) {
		return nil
	}

	t0 := time.Now()
	loadCtx, cancel := context.WithTimeout(ctx, implementsLoadTimeout)
	defer cancel()

	lr, err := goanalysis.LoadPackages(loadCtx, root, goanalysis.LoadOpts{Timeout: implementsLoadTimeout})
	if err != nil {
		// Cold cache, missing/unbuildable deps, or timeout: degrade silently to
		// the heuristic. Warn (not Debug) so operators can see when the enrichment
		// did not run for a repo they expected it to cover.
		slog.Warn("codegraph: IMPLEMENTS go/types load failed (non-fatal, filter falls back to heuristic)",
			slog.String("repo", root), slog.Any("error", err))
		implementsLoadTotal.WithLabelValues("error").Inc()
		return nil
	}

	sats := goanalysis.ComputeSatisfactions(lr.Packages)
	rels := make([]parser.TypeRelationship, 0, len(sats))
	for _, s := range sats {
		// A self-edge (type whose interface name resolves to itself) is impossible
		// here because ComputeSatisfactions only pairs non-interface types with
		// interfaces. Skip empty endpoints defensively.
		if s.Type == "" || s.Interface == "" || s.TypeFile == "" {
			continue
		}
		rels = append(rels, parser.TypeRelationship{
			Subject: s.Type,
			Target:  s.Interface,
			Kind:    parser.RelImplements,
			File:    s.TypeFile,
		})
	}

	implementsLoadTotal.WithLabelValues("ok").Inc()
	implementsEdgesTotal.WithLabelValues(graphName(root)).Add(float64(len(rels)))
	slog.Info("codegraph: IMPLEMENTS go/types satisfaction done",
		slog.String("repo", root), slog.Int("edges", len(rels)),
		slog.Duration("elapsed", time.Since(t0)))
	return rels
}
