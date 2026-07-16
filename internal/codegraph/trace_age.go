package codegraph

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// TraceFromAGE builds a call trace by querying CALLS edges directly from
// the AGE property graph, avoiding the need to re-parse the entire repo
// (which callgraph.TraceRepo does on cache miss, taking 2-60s depending
// on repo size).
//
// The function performs a BFS traversal using Cypher:
//   - callers: MATCH (s:Symbol {name: $name})<-[:CALLS]-(caller:Symbol) ...
//   - callees: MATCH (s:Symbol {name: $name})-[:CALLS]->(callee:Symbol) ...
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

	// Find the root symbol first. We need its properties to construct
	// the parser.Symbol for the trace result.
	rootCypher := fmt.Sprintf(
		`MATCH (s:Symbol {name: '%s'}) RETURN s.name, s.kind, s.file, s.start_line, s.end_line, s.signature LIMIT 1`,
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

	// BFS traversal using variable-length path Cypher.
	// CALLS*1..N matches paths of length 1 to N.
	// We query all paths from root, then reconstruct the tree client-side.
	var pathCypher string
	if direction == "callers" {
		pathCypher = fmt.Sprintf(
			`MATCH path = (target:Symbol {name: '%s'})<-[:CALLS*1..%d]-(caller:Symbol)
			 RETURN [node IN nodes(path) | node.name] AS names,
			        [node IN nodes(path) | node.kind] AS kinds,
			        [node IN nodes(path) | node.file] AS files,
			        [node IN nodes(path) | node.start_line] AS start_lines,
			        [node IN nodes(path) | node.end_line] AS end_lines,
			        [rel IN relationships(path) | rel.line] AS call_lines`,
			escapeCypherString(symbolName), maxDepth,
		)
	} else {
		pathCypher = fmt.Sprintf(
			`MATCH path = (source:Symbol {name: '%s'})-[:CALLS*1..%d]->(callee:Symbol)
			 RETURN [node IN nodes(path) | node.name] AS names,
			        [node IN nodes(path) | node.kind] AS kinds,
			        [node IN nodes(path) | node.file] AS files,
			        [node IN nodes(path) | node.start_line] AS start_lines,
			        [node IN nodes(path) | node.end_line] AS end_lines,
			        [rel IN relationships(path) | rel.line] AS call_lines`,
			escapeCypherString(symbolName), maxDepth,
		)
	}

	pathRows, err := store.ExecCypher(ctx, graphName, pathCypher, 6)
	if err != nil {
		slog.Warn("trace from AGE: path query failed, falling back",
			slog.String("symbol", symbolName),
			slog.Any("error", err))
		return nil, fmt.Errorf("trace from AGE: path query: %w", err)
	}

	// Reconstruct tree from paths.
	// Each row is a path from root to a leaf. We build a tree by
	// splitting on the path nodes.
	tree := buildTreeFromPaths(rootSym, pathRows, direction)
	result.Tree = []callgraph.CallChainNode{tree}
	result.TotalNodes = countNodes(tree)
	result.Resolved = result.TotalNodes - 1 // root doesn't count as resolved
	result.MaxDepth = maxDepthOf(tree, 0)

	return result, nil
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
	if v, err := strconv.ParseUint(stripAgtypeQuotes(row[3]), 10, 32); err == nil {
		sym.StartLine = uint32(v)
	}
	if v, err := strconv.ParseUint(stripAgtypeQuotes(row[4]), 10, 32); err == nil {
		sym.EndLine = uint32(v)
	}
	return sym
}

// buildTreeFromPaths reconstructs a call tree from Cypher path rows.
// Each row contains arrays: names, kinds, files, start_lines, end_lines, call_lines.
// The first element of each array is the root (for callees) or the leaf (for callers).
func buildTreeFromPaths(root *parser.Symbol, rows [][]string, direction string) callgraph.CallChainNode {
	rootNode := callgraph.CallChainNode{Symbol: root}

	// For each path, walk from root outward and insert children.
	for _, row := range rows {
		names := parseAgtypeArray(row[0])
		kinds := parseAgtypeArray(row[1])
		files := parseAgtypeArray(row[2])
		startLines := parseAgtypeArray(row[3])
		endLines := parseAgtypeArray(row[4])
		callLines := parseAgtypeArray(row[5])

		if len(names) < 2 {
			continue // path of length 0 = just root, no edges
		}

		// For callees: names[0] = root, names[1..] = callees
		// For callers: names[last] = root, names[0..last-1] = callers
		// We normalize: always walk from root outward.
		var chain []symbolAtLine
		if direction == "callers" {
			// Reverse: root is at the end, callers are before it.
			// call_lines[i] is the line in the CALLER (names[i]) that calls names[i+1].
			// After reversal: root=names[last], then names[last-1], ..., names[0].
			for i := len(names) - 1; i >= 0; i-- {

				var callLine uint32
				if i > 0 && i-1 < len(callLines) {
					callLine = parseUint32(callLines[i-1])
				}
				chain = append(chain, symbolAtLine{
					name:      names[i],
					kind:      kinds[i],
					file:      files[i],
					startLine: startLines[i],
					endLine:   endLines[i],
					callLine:  callLine,
				})
			}
		} else {
			// callees: root=names[0], then names[1], ..., names[last].
			// call_lines[i] is the line in names[i] that calls names[i+1].
			for i := 0; i < len(names); i++ {
				var callLine uint32
				if i < len(callLines) {
					callLine = parseUint32(callLines[i])
				}
				chain = append(chain, symbolAtLine{
					name:      names[i],
					kind:      kinds[i],
					file:      files[i],
					startLine: startLines[i],
					endLine:   endLines[i],
					callLine:  callLine,
				})
			}
		}

		// Walk the chain from root (chain[0]) and insert into tree.
		insertChain(&rootNode, chain)
	}

	return rootNode
}

type symbolAtLine struct {
	name      string
	kind      string
	file      string
	startLine string
	endLine   string
	callLine  uint32
}

// insertChain walks the chain (starting from chain[1], since chain[0] = root)
// and inserts each node into the tree, creating children as needed.
func insertChain(root *callgraph.CallChainNode, chain []symbolAtLine) {
	if len(chain) < 2 {
		return
	}
	current := root
	for i := 1; i < len(chain); i++ {
		sym := chain[i]
		// Check if this child already exists (dedup by name+file).
		var found *callgraph.CallChainNode
		for j := range current.Children {
			child := &current.Children[j]
			if child.Symbol != nil && child.Symbol.Name == sym.name && child.Symbol.File == sym.file {
				found = child
				break
			}
		}
		if found != nil {
			current = found
			continue
		}
		// Create new child.
		childSym := &parser.Symbol{
			Name: sym.name,
			Kind: parser.NodeKind(sym.kind),
			File: sym.file,
		}
		if v, err := strconv.ParseUint(sym.startLine, 10, 32); err == nil {
			childSym.StartLine = uint32(v)
		}
		if v, err := strconv.ParseUint(sym.endLine, 10, 32); err == nil {
			childSym.EndLine = uint32(v)
		}
		newChild := callgraph.CallChainNode{
			Symbol:   childSym,
			CallLine: sym.callLine,
		}
		current.Children = append(current.Children, newChild)
		current = &current.Children[len(current.Children)-1]
	}
}

func countNodes(node callgraph.CallChainNode) int {
	count := 1
	for _, child := range node.Children {
		count += countNodes(child)
	}
	return count
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

// parseAgtypeArray parses an AGE agtype array literal like:
//
//	["foo", "bar", "baz"]
//	["1", "2", "3"]
//
// into a string slice. Handles quoted strings and bare numbers.
func parseAgtypeArray(s string) []string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil
	}
	s = s[1 : len(s)-1]
	if s == "" {
		return nil
	}
	// Split by comma, then strip quotes.
	var parts []string
	// Simple split — AGE arrays don't have nested commas in our use case.
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		part = stripAgtypeQuotes(part)
		parts = append(parts, part)
	}
	return parts
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
