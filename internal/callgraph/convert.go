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
		callee := idx.resolve(te.CalleeName, te.CalleeFile, 0)
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

// pathSuffixComponents is how many trailing path elements a non-exact file
// match must agree on. Typed paths normally equal tree-sitter paths exactly;
// the suffix fallback only tolerates a differing root prefix (symlinked or
// relative roots), and three components keep a GOROOT/module-cache file from
// matching a repo file that merely shares a basename.
const pathSuffixComponents = 3

// resolve returns the symbol named name defined in file, or nil.
//
// An empty file means the callee is external (stdlib / dependency): nil. A
// file that matches no repo symbol is also nil, not a name-only guess: a
// missing edge is a recall gap, a wrong edge puts an unrelated function into
// every blast radius that crosses it. line, when non-zero, disambiguates
// several same-named symbols in one file (methods of different types).
func (ix convertIndex) resolve(name, file string, line uint32) *parser.Symbol {
	if name == "" || file == "" {
		return nil
	}
	if syms := ix.byNameFile[name+"\x00"+filepath.Clean(file)]; len(syms) > 0 {
		return pickByLine(syms, line)
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

// pickByLine prefers the symbol whose definition spans line; otherwise the
// first one (stable, matches the historical first-wins behaviour within a
// single file).
func pickByLine(syms []*parser.Symbol, line uint32) *parser.Symbol {
	if line != 0 {
		for _, s := range syms {
			if line >= s.StartLine && line <= s.EndLine {
				return s
			}
		}
	}
	return syms[0]
}

func sharesPathSuffix(a, b string) bool {
	as := strings.Split(filepath.ToSlash(filepath.Clean(a)), "/")
	bs := strings.Split(filepath.ToSlash(filepath.Clean(b)), "/")
	n := min(pathSuffixComponents, len(as), len(bs))
	// Two absolute paths that differ within the suffix window are different
	// files; only a relative path may legitimately be shorter than the window.
	if n < pathSuffixComponents && filepath.IsAbs(a) && filepath.IsAbs(b) {
		return false
	}
	for i := 1; i <= n; i++ {
		if as[len(as)-i] != bs[len(bs)-i] {
			return false
		}
	}
	return true
}

// edgeKey returns a deduplication key for a CallEdge.
//
// The caller is identified by file AND name: several `main` / `init` / TestMain
// functions coexist in one repo, and keying on the bare name let a typed edge
// from one of them suppress a different function's tree-sitter edge.
func edgeKey(e CallEdge) string {
	caller := ""
	if e.Caller != nil {
		caller = e.Caller.File + "\x00" + e.Caller.Name
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
