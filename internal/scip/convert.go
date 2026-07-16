package scip

import (
	"sort"

	"github.com/anatolykoptev/go-code/internal/goanalysis"
	sciplib "github.com/sourcegraph/scip/bindings/go/scip"
)

// scipLineToOneBased converts a SCIP 0-indexed line to 1-indexed.
const scipLineOffset uint32 = 1

// minEnclosingRangeLen is the minimum length of an EnclosingRange to extract end line.
// SCIP enclosing ranges: [startLine, startChar, endLine, endChar].
const minEnclosingRangeLen = 4

// ConvertToEdges converts a SCIP Index into a slice of TypedEdge call edges.
// It performs two passes:
//  1. Build a definition map (symbol → defInfo) and per-file function ranges.
//  2. For each non-definition reference, find the enclosing function and emit an edge.
func ConvertToEdges(idx *Index) []goanalysis.TypedEdge {
	if idx == nil || len(idx.Documents) == 0 {
		return nil
	}

	// Pre-build symbol tables once per document to avoid repeated O(n) construction.
	symTables := make([]map[string]*sciplib.SymbolInformation, len(idx.Documents))
	for i, doc := range idx.Documents {
		symTables[i] = doc.SymbolTable()
	}

	defMap := buildDefMap(idx.Documents, symTables)
	funcRanges := buildFuncRanges(idx.Documents, symTables, defMap)

	var edges []goanalysis.TypedEdge
	for _, doc := range idx.Documents {
		edges = append(edges, extractDocEdges(doc, defMap, funcRanges)...)
	}

	// Extract IMPLEMENTS edges from trait references (Rust trait dispatch).
	// rust-analyzer does not populate Relationship.IsImplementation
	// (https://github.com/sourcegraph/scip-rust/issues/16), so we use a
	// proximity heuristic: for each reference to a Trait-kind symbol, find
	// the nearest struct/type definition in the same file and emit an
	// implements edge with IsInterface=true.
	implEdges := extractImplEdges(idx.Documents, symTables, defMap)
	edges = append(edges, implEdges...)

	return edges
}

// buildDefMap constructs a symbol→defInfo map from all definition occurrences.
func buildDefMap(docs []*sciplib.Document, symTables []map[string]*sciplib.SymbolInformation) map[string]defInfo {
	dm := make(map[string]defInfo)
	for i, doc := range docs {
		symLookup := symTables[i]
		for _, occ := range doc.Occurrences {
			if !isDefinition(occ) {
				continue
			}
			if sciplib.IsLocalSymbol(occ.Symbol) {
				continue
			}
			if len(occ.Range) == 0 {
				continue
			}
			line := uint32(occ.Range[0]) + scipLineOffset //nolint:gosec // SCIP lines ≥ 0
			name := parseSymbolName(occ.Symbol)
			if si, ok := symLookup[occ.Symbol]; ok && si.DisplayName != "" {
				name = si.DisplayName
			}
			dm[occ.Symbol] = defInfo{
				Name: name,
				File: doc.RelativePath,
				Line: line,
				Pkg:  pkgFromSymbol(occ.Symbol),
			}
		}
	}
	return dm
}

// buildFuncRanges builds a map of file → sorted []funcRange using definition occurrences.
// It prefers EnclosingRange when present; otherwise uses sorted start lines with
// a fallback end-line = next function's start - 1.
func buildFuncRanges(docs []*sciplib.Document, symTables []map[string]*sciplib.SymbolInformation, defMap map[string]defInfo) map[string][]funcRange {
	result := make(map[string][]funcRange)
	for i, doc := range docs {
		symLookup := symTables[i]
		var funcs []funcRange
		for _, occ := range doc.Occurrences {
			if !isDefinition(occ) {
				continue
			}
			if sciplib.IsLocalSymbol(occ.Symbol) {
				continue
			}
			if !isFuncOcc(occ, symLookup) {
				continue
			}
			if len(occ.Range) == 0 {
				continue
			}
			fr := funcRange{
				Name:      defMap[occ.Symbol].Name,
				StartLine: uint32(occ.Range[0]) + 1, //nolint:gosec
			}
			if len(occ.EnclosingRange) >= minEnclosingRangeLen {
				fr.EndLine = uint32(occ.EnclosingRange[2]) + scipLineOffset //nolint:gosec
			}
			funcs = append(funcs, fr)
		}
		// Sort by StartLine ascending.
		sort.Slice(funcs, func(i, j int) bool {
			return funcs[i].StartLine < funcs[j].StartLine
		})
		// Fill in EndLine for ranges without EnclosingRange.
		for i := range funcs {
			if funcs[i].EndLine == 0 {
				if i+1 < len(funcs) {
					funcs[i].EndLine = funcs[i+1].StartLine - 1
				}
				// last function: EndLine stays 0 (open-ended, handled in lookup)
			}
		}
		result[doc.RelativePath] = funcs
	}
	return result
}

// extractDocEdges emits TypedEdge entries for non-definition occurrences in doc.
func extractDocEdges(doc *sciplib.Document, defMap map[string]defInfo, funcRanges map[string][]funcRange) []goanalysis.TypedEdge {
	var edges []goanalysis.TypedEdge
	ranges := funcRanges[doc.RelativePath]

	for _, occ := range doc.Occurrences {
		if isDefinition(occ) {
			continue
		}
		if sciplib.IsLocalSymbol(occ.Symbol) {
			continue
		}
		if len(occ.Range) == 0 {
			continue
		}

		callLine := uint32(occ.Range[0]) + scipLineOffset //nolint:gosec
		caller := enclosingFuncRange(ranges, callLine)
		if caller == nil {
			continue
		}

		callee, inDef := defMap[occ.Symbol]
		calleeName := parseSymbolName(occ.Symbol)
		calleeFile := ""
		calleePkg := pkgFromSymbol(occ.Symbol)
		if inDef {
			calleeName = callee.Name
			calleeFile = callee.File
			calleePkg = callee.Pkg
		}

		// Filter stdlib method calls (clone, unwrap, poll, etc.) that
		// never resolve to project nodes and only pollute call traces
		// with unresolved "external" nodes. Only filter when the callee
		// is NOT in the definition map (i.e. truly external) — project
		// methods with the same name as a stdlib method are kept.
		if !inDef && IsStdlibMethod(calleeName) {
			continue
		}

		edges = append(edges, goanalysis.TypedEdge{
			CallerName: caller.Name,
			CallerFile: doc.RelativePath,
			CallerLine: caller.StartLine,
			CalleeName: calleeName,
			CalleeFile: calleeFile,
			CalleePkg:  calleePkg,
			Line:       callLine,
		})
	}
	return edges
}

// enclosingFuncRange returns the funcRange that contains the given line, or nil.
// Uses binary search since ranges are sorted by StartLine.
func enclosingFuncRange(ranges []funcRange, line uint32) *funcRange {
	n := len(ranges)
	if n == 0 {
		return nil
	}
	// Find the last function whose StartLine <= line.
	lo, hi := 0, n-1
	idx := -1
	for lo <= hi {
		mid := (lo + hi) / 2
		if ranges[mid].StartLine <= line {
			idx = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	if idx < 0 {
		return nil
	}
	fr := &ranges[idx]
	if fr.EndLine == 0 || line <= fr.EndLine {
		return fr
	}
	return nil
}

// isDefinition reports whether an occurrence is a definition.
func isDefinition(occ *sciplib.Occurrence) bool {
	return occ.SymbolRoles&int32(sciplib.SymbolRole_Definition) != 0
}

// isFuncOcc reports whether an occurrence refers to a callable symbol.
// First checks Kind from SymbolInformation; falls back to symbol string heuristic
// when Kind is UnspecifiedKind (scip-typescript doesn't emit Kind).
func isFuncOcc(occ *sciplib.Occurrence, symLookup map[string]*sciplib.SymbolInformation) bool {
	if si, ok := symLookup[occ.Symbol]; ok && si.Kind != sciplib.SymbolInformation_UnspecifiedKind {
		return isFuncKind(si.Kind)
	}
	return isFuncSymbol(occ.Symbol)
}

// maxImplProximityLines is the maximum line distance between a trait reference
// and a struct/type definition in the same file for them to be considered an
// implements relationship. Rust impl blocks are typically near the struct def,
// but in separate impl files the struct may be defined earlier in the same file.
const maxImplProximityLines = 200

// extractImplEdges finds trait→struct implementation relationships by scanning
// for references to Trait-kind symbols and matching them to the nearest
// struct/type definition in the same file. This is needed because rust-analyzer's
// SCIP output does not populate Relationship.IsImplementation.
func extractImplEdges(docs []*sciplib.Document, symTables []map[string]*sciplib.SymbolInformation, defMap map[string]defInfo) []goanalysis.TypedEdge {
	// Collect trait symbols (Kind == Trait).
	traits := make(map[string]defInfo)
	for i, doc := range docs {
		for _, sym := range doc.Symbols {
			if sym.Kind != sciplib.SymbolInformation_Trait {
				continue
			}
			name := sym.DisplayName
			if name == "" {
				name = parseSymbolName(sym.Symbol)
			}
			line := uint32(0)
			for _, occ := range doc.Occurrences {
				if occ.Symbol == sym.Symbol && isDefinition(occ) && len(occ.Range) > 0 {
					line = uint32(occ.Range[0]) + scipLineOffset
					break
				}
			}
			traits[sym.Symbol] = defInfo{
				Name: name,
				File: doc.RelativePath,
				Line: line,
				Pkg:  pkgFromSymbol(sym.Symbol),
			}
		}
		_ = i // symTables index unused here
	}
	if len(traits) == 0 {
		return nil
	}

	// Collect struct/type/class definitions per file with line numbers.
	type typeDef struct {
		Sym  string
		Name string
		Line uint32
	}
	typeDefsByFile := make(map[string][]typeDef)
	for i, doc := range docs {
		for _, sym := range doc.Symbols {
			if sym.Kind != sciplib.SymbolInformation_Struct &&
				sym.Kind != sciplib.SymbolInformation_Class &&
				sym.Kind != sciplib.SymbolInformation_Type {
				continue
			}
			if sciplib.IsLocalSymbol(sym.Symbol) {
				continue
			}
			name := sym.DisplayName
			if name == "" {
				name = parseSymbolName(sym.Symbol)
			}
			for _, occ := range doc.Occurrences {
				if occ.Symbol == sym.Symbol && isDefinition(occ) && len(occ.Range) > 0 {
					td := typeDef{
						Sym:  sym.Symbol,
						Name: name,
						Line: uint32(occ.Range[0]) + scipLineOffset,
					}
					typeDefsByFile[doc.RelativePath] = append(typeDefsByFile[doc.RelativePath], td)
					break
				}
			}
		}
		_ = i
	}
	// Sort each file's type defs by line for efficient lookup.
	for file := range typeDefsByFile {
		sort.Slice(typeDefsByFile[file], func(i, j int) bool {
			return typeDefsByFile[file][i].Line < typeDefsByFile[file][j].Line
		})
	}

	// For each reference to a trait symbol, find the nearest struct/type def
	// in the same file (within maxImplProximityLines) and emit an IMPLEMENTS edge.
	type implKey struct {
		traitSym string
		implSym  string
		file     string
	}
	seen := make(map[implKey]bool)
	var edges []goanalysis.TypedEdge

	for _, doc := range docs {
		typeDefs := typeDefsByFile[doc.RelativePath]
		if len(typeDefs) == 0 {
			continue
		}

		for _, occ := range doc.Occurrences {
			if isDefinition(occ) {
				continue
			}
			traitInfo, isTrait := traits[occ.Symbol]
			if !isTrait {
				continue
			}
			if len(occ.Range) == 0 {
				continue
			}
			traitLine := uint32(occ.Range[0]) + scipLineOffset

			// Find the nearest type def at or before the trait reference line.
			bestIdx := -1
			bestDist := uint32(maxImplProximityLines + 1)
			for i, td := range typeDefs {
				if td.Line <= traitLine {
					dist := traitLine - td.Line
					if dist < bestDist {
						bestDist = dist
						bestIdx = i
					}
				}
			}
			if bestIdx < 0 || bestDist > maxImplProximityLines {
				continue
			}

			implDef := typeDefs[bestIdx]
			key := implKey{traitSym: occ.Symbol, implSym: implDef.Sym, file: doc.RelativePath}
			if seen[key] {
				continue
			}
			seen[key] = true

			edges = append(edges, goanalysis.TypedEdge{
				CallerName:   implDef.Name,
				CallerFile:   doc.RelativePath,
				CallerLine:   implDef.Line,
				CalleeName:   traitInfo.Name,
				CalleeFile:   traitInfo.File,
				CalleePkg:    traitInfo.Pkg,
				Line:         traitLine,
				IsInterface:  true,
				ReceiverType: implDef.Name,
			})
		}
	}

	return edges
}
