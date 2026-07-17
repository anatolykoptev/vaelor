package callgraph

import (
	"context"
	"log/slog"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// injectCrossLangNodes performs a single post-trace pass over all nodes.
// For each node at depth <= MaxDepth-1 it queries HandlesRoute; when a route is
// found it calls FetchedBy and appends synthetic cross-language children.
// This function is only called when opts.CrossRefs != nil && opts.Repo != "".
// All CrossRefs errors are swallowed via slog.Debug — the feature is opportunistic.
func injectCrossLangNodes(ctx context.Context, result *TraceResult, opts TraceOpts) {
	type entry struct {
		node  *CallChainNode
		depth int
	}

	var queue []entry
	for i := range result.Tree {
		queue = append(queue, entry{node: &result.Tree[i], depth: 0})
	}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		// Enqueue children before potentially appending new synthetic ones.
		for i := range cur.node.Children {
			queue = append(queue, entry{node: &cur.node.Children[i], depth: cur.depth + 1})
		}

		// Depth cap: only enrich nodes within the main depth limit.
		if cur.depth > opts.MaxDepth-1 {
			continue
		}
		// Never recurse into already-synthetic nodes.
		if cur.node.Kind == CrossLanguageFetchKind {
			continue
		}

		sym := cur.node.Symbol
		if sym == nil {
			continue
		}

		route, found, err := opts.CrossRefs.HandlesRoute(ctx, opts.Repo, sym.Name, sym.File)
		if err != nil {
			slog.Debug("CrossRefs.HandlesRoute error", "symbol", sym.Name, "err", err)
			continue
		}
		if !found {
			continue
		}

		refs, err := opts.CrossRefs.FetchedBy(ctx, opts.Repo, route)
		if err != nil {
			slog.Debug("CrossRefs.FetchedBy error", "route", route, "err", err)
			continue
		}

		for _, ref := range refs {
			sym := &parser.Symbol{Name: ref.Name, File: ref.File, Kind: parser.KindFunction}
			synth := CallChainNode{
				Symbol:     sym,
				Kind:       CrossLanguageFetchKind,
				Depth:      cur.depth + 1,
				CallerKind: nodeCallerKind(sym),
			}
			cur.node.Children = append(cur.node.Children, synth)
			result.TotalNodes++
		}
	}
}
