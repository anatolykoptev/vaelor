package callgraph

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// ConvertToCallGraph converts typed edges to the existing CallGraph format by
// matching callers/callees against tree-sitter symbols by name and file.
func ConvertToCallGraph(typedEdges []goanalysis.TypedEdge, tsSymbols []*parser.Symbol) *CallGraph {
	idx := buildConvertIndexes(tsSymbols)

	edges := make([]CallEdge, 0, len(typedEdges))
	for _, te := range typedEdges {
		caller := idx.resolve(te.CallerName, te.CallerFile, te.CallerLine)
		callee := idx.resolve(te.CalleeName, te.CalleeFile, te.CalleeLine)
		edges = append(edges, CallEdge{
			Caller:      caller,
			Callee:      callee,
			CalleeName:  te.CalleeName,
			Receiver:    te.ReceiverType,
			Line:        te.Line,
			IsInterface: te.IsInterface,
		})
	}

	return &CallGraph{
		Edges:   edges,
		Symbols: tsSymbols,
	}
}

// MergeCallGraphs merges a tree-sitter call graph with a typed call graph.
// Typed edges take priority; unmatched tree-sitter edges are appended.
//
// Metadata fields (Warming, TypeRels, UsesIndex, HookCallbacks) are carried
// from tsGraph — the typed/SCIP graphs never set them. TypeRels is unioned
// with dedup in case either side carries them. Tier and Backend are left
// zero-valued; every caller re-assigns them immediately after the merge.
//
// The result is built by shallow-copying tsGraph and overriding the merged
// fields. This ensures a future field added to CallGraph is carried from
// tsGraph by default rather than silently dropped. A compile-time guard that
// fails loudly on new fields would require reflection; none exists cleanly
// in Go, so the copy-then-override shape is the next best thing.
func MergeCallGraphs(tsGraph, typedGraph *CallGraph) *CallGraph {
	if typedGraph == nil {
		return tsGraph
	}
	if tsGraph == nil {
		return typedGraph
	}

	// Build dedup key set from typed edges (typed takes priority).
	seen := make(map[string]struct{}, len(typedGraph.Edges))
	for _, e := range typedGraph.Edges {
		seen[edgeKey(e)] = struct{}{}
	}

	// Start with all typed edges; append unmatched tree-sitter edges.
	merged := make([]CallEdge, len(typedGraph.Edges), len(typedGraph.Edges)+len(tsGraph.Edges))
	copy(merged, typedGraph.Edges)
	for _, e := range tsGraph.Edges {
		if _, dup := seen[edgeKey(e)]; !dup {
			merged = append(merged, e)
		}
	}

	symbols := mergeSymbols(typedGraph.Symbols, tsGraph.Symbols)

	// Shallow-copy tsGraph to carry all metadata fields by default, then
	// override the merged fields. Tier/Backend are zeroed — callers re-assign.
	out := *tsGraph
	out.Edges = merged
	out.Symbols = symbols
	out.TypeRels = mergeTypeRels(tsGraph.TypeRels, typedGraph.TypeRels)
	out.Tier = ""
	out.Backend = ""
	return &out
}

// mergeTypeRels unions two TypeRels slices, deduplicating by
// Subject+Target+Kind+File+Line. Primary (tsGraph) entries are kept first.
func mergeTypeRels(primary, secondary []parser.TypeRelationship) []parser.TypeRelationship {
	if len(primary) == 0 {
		return secondary
	}
	if len(secondary) == 0 {
		return primary
	}
	seen := make(map[string]struct{}, len(primary)+len(secondary))
	result := make([]parser.TypeRelationship, 0, len(primary)+len(secondary))
	for _, rel := range primary {
		k := relKey(rel)
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			result = append(result, rel)
		}
	}
	for _, rel := range secondary {
		k := relKey(rel)
		if _, ok := seen[k]; !ok {
			seen[k] = struct{}{}
			result = append(result, rel)
		}
	}
	return result
}

func relKey(r parser.TypeRelationship) string {
	return r.Subject + ":" + r.Target + ":" + string(r.Kind) + ":" + r.File + ":" + strconv.FormatUint(uint64(r.Line), 10)
}

// convertIndex resolves typed-edge endpoints to tree-sitter symbols by their
// exact definition site, never by name alone. Name-only (or name+basename)
// resolution is the failure mode this replaces: `main` in cmd/eval/main.go and
// `main` in cmd/vaelor/main.go share both a name and a basename, and a typed
// callee that is not a repo symbol at all (flag.FlagSet.Parse) used to be bound
// to an arbitrary repo symbol also called Parse.
type convertIndex struct {
	byNameFile map[string][]*parser.Symbol // name + "\x00" + cleaned full path
	byName     map[string][]*parser.Symbol
}

func buildConvertIndexes(symbols []*parser.Symbol) convertIndex {
	idx := convertIndex{
		byNameFile: make(map[string][]*parser.Symbol, len(symbols)),
		byName:     make(map[string][]*parser.Symbol, len(symbols)),
	}
	for _, sym := range symbols {
		k := sym.Name + "\x00" + filepath.Clean(sym.File)
		idx.byNameFile[k] = append(idx.byNameFile[k], sym)
		idx.byName[sym.Name] = append(idx.byName[sym.Name], sym)
	}
	return idx
}

// pathSuffixComponents is how many trailing path elements a RELATIVE typed
// path must agree on with a symbol path (SCIP emits repo-relative paths).
const pathSuffixComponents = 3

// resolve returns the symbol named name whose declaration is at file:line, or
// nil. It never guesses.
//
// An absolute file must match a symbol's path exactly: go/packages reports
// stdlib, module-cache and vendored callees by their real absolute paths, and
// those must stay unresolved rather than be matched to a repo file by suffix.
// A relative file (SCIP) falls back to a unique suffix match. line, when
// non-zero, selects among same-named symbols by declaration span (methods of
// different types in one file); when several share the file and none spans the
// line, there is no answer rather than the first one.
func (ix convertIndex) resolve(name, file string, line uint32) *parser.Symbol {
	if name == "" || file == "" {
		return nil
	}
	if syms := ix.byNameFile[name+"\x00"+filepath.Clean(file)]; len(syms) > 0 {
		return pickByLine(syms, line)
	}
	if filepath.IsAbs(file) {
		return nil
	}
	var match []*parser.Symbol
	for _, sym := range ix.byName[name] {
		if sharesPathSuffix(sym.File, file) {
			match = append(match, sym)
		}
	}
	if len(match) == 1 {
		return match[0]
	}
	return nil
}

// pickByLine returns the symbol whose span contains line. With no line, or a
// single candidate, the first/only symbol is returned; with several candidates
// and no span containing line, nil.
func pickByLine(syms []*parser.Symbol, line uint32) *parser.Symbol {
	if len(syms) == 1 || line == 0 {
		return syms[0]
	}
	for _, s := range syms {
		if line >= s.StartLine && line <= s.EndLine {
			return s
		}
	}
	return nil
}

// sharesPathSuffix reports whether the relative path rel is a path-element
// suffix of abs (at most pathSuffixComponents elements compared).
func sharesPathSuffix(abs, rel string) bool {
	as := strings.Split(filepath.ToSlash(filepath.Clean(abs)), "/")
	rs := strings.Split(filepath.ToSlash(filepath.Clean(rel)), "/")
	n := min(pathSuffixComponents, len(as), len(rs))
	for i := 1; i <= n; i++ {
		if as[len(as)-i] != rs[len(rs)-i] {
			return false
		}
	}
	return true
}

// edgeKey returns a deduplication key for a CallEdge.
//
// The caller is identified by file, name AND declaration line: several `main` /
// `init` / TestMain functions coexist in one repo, and same-named methods of
// different types share a file, so a coarser key let a typed edge from one of
// them suppress a different function's tree-sitter edge.
func edgeKey(e CallEdge) string {
	caller := ""
	if e.Caller != nil {
		caller = e.Caller.File + "\x00" + e.Caller.Name + "\x00" + strconv.FormatUint(uint64(e.Caller.StartLine), 10)
	}
	return caller + "->" + e.CalleeName
}

// mergeSymbols merges two symbol slices, deduplicating by "name:file".
func mergeSymbols(primary, secondary []*parser.Symbol) []*parser.Symbol {
	seen := make(map[string]struct{}, len(primary)+len(secondary))
	result := make([]*parser.Symbol, 0, len(primary)+len(secondary))

	for _, sym := range primary {
		key := sym.Name + ":" + sym.File
		if _, exists := seen[key]; !exists {
			seen[key] = struct{}{}
			result = append(result, sym)
		}
	}
	for _, sym := range secondary {
		key := sym.Name + ":" + sym.File
		if _, exists := seen[key]; !exists {
			seen[key] = struct{}{}
			result = append(result, sym)
		}
	}
	return result
}
