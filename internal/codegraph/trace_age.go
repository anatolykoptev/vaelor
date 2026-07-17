package codegraph

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/langutil"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// TraceFromAGE builds a call trace by querying CALLS edges directly from
// the AGE property graph, avoiding the need to re-parse the entire repo
// (which callgraph.TraceRepo does on cache miss, taking 2-60s depending
// on repo size).
//
// Uses iterative BFS: for each depth level, queries direct CALLS
// neighbors of all nodes at that depth. This avoids AGE's lack of
// support for list comprehension in variable-length path queries
// (nodes(path) works but [node IN nodes(path) | node.name] does not).
//
// Returns a TraceResult compatible with callgraph.Trace. If the graph is
// not indexed, the symbol is not found, or any query error occurs, returns
// (nil, error) so the caller can fall back to BuildFromRepo.
func TraceFromAGE(ctx context.Context, store *Store, graphName, symbolName, direction string, maxDepth int) (*callgraph.TraceResult, error) {
	if err := store.EnsureGraphExistsForRead(ctx, graphName); err != nil {
		return nil, fmt.Errorf("trace from AGE: %w", err)
	}
	if maxDepth <= 0 {
		maxDepth = 5
	}
	if maxDepth > 10 {
		maxDepth = 10
	}
	if direction == "" {
		direction = "callees"
	}

	// Find the root symbol(s). There may be multiple symbols with the same
	// name in different files — we pick the first (highest pagerank if available).
	rootCypher := fmt.Sprintf(
		`MATCH (s:Symbol {name: '%s'}) RETURN s.name, s.kind, s.file, s.start_line, s.end_line, s.signature ORDER BY s.pagerank DESC LIMIT 1`,
		escapeCypherString(symbolName),
	)
	rootRows, err := store.ExecCypher(ctx, graphName, rootCypher, 6)
	if err != nil {
		return nil, fmt.Errorf("trace from AGE: root query: %w", err)
	}
	if len(rootRows) == 0 {
		return nil, fmt.Errorf("trace from AGE: symbol %q not found", symbolName)
	}

	rootSym := rowToSymbol(rootRows[0])
	result := &callgraph.TraceResult{
		Root: rootSym,
		Tier: "age-graph",
	}

	rootNode := callgraph.CallChainNode{Symbol: rootSym, CallerKind: ageCallerKind(rootSym.Name, rootSym.File, rootSym.Kind)}

	// Iterative BFS: expand each level by querying direct CALLS neighbors.
	// frontier = nodes at the current depth that need expansion.
	type frontierNode struct {
		node    *callgraph.CallChainNode
		symName string
		symFile string
	}
	frontier := []frontierNode{{node: &rootNode, symName: rootSym.Name, symFile: rootSym.File}}
	visited := map[string]bool{rootSym.Name + "\x00" + rootSym.File: true}

	totalNodes := 1

	for depth := 1; depth <= maxDepth && len(frontier) > 0; depth++ {
		var nextFrontier []frontierNode

		for _, fn := range frontier {
			children, err := queryDirectNeighbors(ctx, store, graphName, fn.symName, fn.symFile, direction)
			if err != nil {
				slog.Warn("trace from AGE: neighbor query failed, continuing with partial results",
					slog.String("symbol", fn.symName),
					slog.Int("depth", depth),
					slog.Any("error", err))
				continue
			}

			for _, child := range children {
				key := child.name + "\x00" + child.file
				if visited[key] {
					// Cycle: mark the existing node but don't re-expand.
					continue
				}
				visited[key] = true

				childSym := &parser.Symbol{
					Name:      child.name,
					Kind:      parser.NodeKind(child.kind),
					File:      child.file,
					StartLine: child.startLine,
					EndLine:   child.endLine,
					Signature: child.signature,
				}
				newChild := callgraph.CallChainNode{
					Symbol:     childSym,
					CallLine:   child.callLine,
					CallerKind: ageCallerKind(child.name, child.file, childSym.Kind),
				}
				fn.node.Children = append(fn.node.Children, newChild)
				totalNodes++

				nextFrontier = append(nextFrontier, frontierNode{
					node:    &fn.node.Children[len(fn.node.Children)-1],
					symName: child.name,
					symFile: child.file,
				})
			}
		}

		frontier = nextFrontier
	}

	result.Tree = []callgraph.CallChainNode{rootNode}
	result.TotalNodes = totalNodes
	result.Resolved = totalNodes - 1 // root doesn't count as resolved
	result.MaxDepth = maxDepthOf(rootNode, 0)

	return result, nil
}

type ageSymbol struct {
	name      string
	kind      string
	file      string
	startLine uint32
	endLine   uint32
	signature string
	callLine  uint32
}

// queryDirectNeighbors queries direct CALLS neighbors (depth=1) of a symbol.
// For direction="callees": MATCH (s)-[:CALLS]->(callee)
// For direction="callers": MATCH (caller)-[:CALLS]->(s)
func queryDirectNeighbors(ctx context.Context, store *Store, graphName, symName, symFile, direction string) ([]ageSymbol, error) {
	// Use composite key (name + file) to disambiguate symbols with the same name.
	relFile := symFile
	// The AGE graph stores file as relative path. If symFile is absolute,
	// we need to match by name only (less precise but works).
	_ = relFile

	var cypher string
	if direction == "callers" {
		cypher = fmt.Sprintf(
			`MATCH (caller:Symbol)-[r:CALLS]->(s:Symbol {name: '%s'})
			 RETURN caller.name, caller.kind, caller.file, caller.start_line, caller.end_line, caller.signature, r.line`,
			escapeCypherString(symName),
		)
	} else {
		cypher = fmt.Sprintf(
			`MATCH (s:Symbol {name: '%s'})-[r:CALLS]->(callee:Symbol)
			 RETURN callee.name, callee.kind, callee.file, callee.start_line, callee.end_line, callee.signature, r.line`,
			escapeCypherString(symName),
		)
	}

	rows, err := store.ExecCypher(ctx, graphName, cypher, 7)
	if err != nil {
		return nil, err
	}

	symbols := make([]ageSymbol, 0, len(rows))
	for _, row := range rows {
		if len(row) < 7 {
			continue
		}
		s := ageSymbol{
			name:      stripAgtypeQuotes(row[0]),
			kind:      stripAgtypeQuotes(row[1]),
			file:      stripAgtypeQuotes(row[2]),
			signature: stripAgtypeQuotes(row[5]),
			callLine:  parseUint32(row[6]),
		}
		s.startLine = parseUint32(row[3])
		s.endLine = parseUint32(row[4])
		symbols = append(symbols, s)
	}
	return symbols, nil
}

// rowToSymbol converts a Cypher row [name, kind, file, start_line, end_line, signature]
// to a parser.Symbol.
func rowToSymbol(row []string) *parser.Symbol {
	sym := &parser.Symbol{
		Name:      stripAgtypeQuotes(row[0]),
		Kind:      parser.NodeKind(stripAgtypeQuotes(row[1])),
		File:      stripAgtypeQuotes(row[2]),
		Signature: stripAgtypeQuotes(row[5]),
	}
	sym.StartLine = parseUint32(row[3])
	sym.EndLine = parseUint32(row[4])
	return sym
}

// ageCallerKind returns the caller kind for an AGE graph neighbor. Nodes with
// no source file or an explicit external kind are bucketed as unresolved so
// they do not inflate production_caller_count.
func ageCallerKind(name, file string, kind parser.NodeKind) string {
	if file == "" || kind == "external" {
		return langutil.CallerKindUnresolved
	}
	return langutil.CallerKind(name, file)
}

func maxDepthOf(node callgraph.CallChainNode, depth int) int {
	maxD := depth
	for _, child := range node.Children {
		if d := maxDepthOf(child, depth+1); d > maxD {
			maxD = d
		}
	}
	return maxD
}

// stripAgtypeQuotes removes the surrounding double quotes that AGE adds to
// agtype string values in text-format query results.
func stripAgtypeQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

func parseUint32(s string) uint32 {
	s = stripAgtypeQuotes(strings.TrimSpace(s))
	v, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0
	}
	return uint32(v)
}

// escapeCypherString escapes single quotes in a string for safe embedding
// in a Cypher string literal.
func escapeCypherString(s string) string {
	return strings.ReplaceAll(s, "'", "\\'")
}
