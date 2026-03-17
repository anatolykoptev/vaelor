package scip

import (
	"sort"

	"github.com/anatolykoptev/go-code/internal/goanalysis"
	sciplib "github.com/sourcegraph/scip/bindings/go/scip"
)

// ConvertToEdges converts a SCIP Index into a slice of TypedEdge call edges.
// It performs two passes:
//  1. Build a definition map (symbol → defInfo) and per-file function ranges.
//  2. For each non-definition reference, find the enclosing function and emit an edge.
func ConvertToEdges(idx *Index) []goanalysis.TypedEdge {
	if idx == nil || len(idx.Documents) == 0 {
		return nil
	}

	defMap := buildDefMap(idx.Documents)
	funcRanges := buildFuncRanges(idx.Documents, defMap)

	var edges []goanalysis.TypedEdge
	for _, doc := range idx.Documents {
		edges = append(edges, extractDocEdges(doc, defMap, funcRanges)...)
	}
	return edges
}

// buildDefMap constructs a symbol→defInfo map from all definition occurrences.
func buildDefMap(docs []*sciplib.Document) map[string]defInfo {
	dm := make(map[string]defInfo)
	for _, doc := range docs {
		symLookup := doc.SymbolTable()
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
			line := uint32(occ.Range[0]) + 1 //nolint:gosec // SCIP lines ≥ 0
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
func buildFuncRanges(docs []*sciplib.Document, defMap map[string]defInfo) map[string][]funcRange {
	result := make(map[string][]funcRange)
	for _, doc := range docs {
		symLookup := doc.SymbolTable()
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
			if len(occ.EnclosingRange) >= 4 { //nolint:mnd
				fr.EndLine = uint32(occ.EnclosingRange[2]) + 1 //nolint:gosec
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

		callLine := uint32(occ.Range[0]) + 1 //nolint:gosec
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

		callerDef := defMap[callerSymbol(doc, caller.Name)]
		edges = append(edges, goanalysis.TypedEdge{
			CallerName: caller.Name,
			CallerFile: doc.RelativePath,
			CallerLine: callerDefLine(callerDef, caller.StartLine),
			CalleeName: calleeName,
			CalleeFile: calleeFile,
			CalleePkg:  calleePkg,
			Line:       callLine,
		})
	}
	return edges
}

// enclosingFuncRange returns the funcRange that contains the given line, or nil.
func enclosingFuncRange(ranges []funcRange, line uint32) *funcRange {
	for i := range ranges {
		fr := &ranges[i]
		if line < fr.StartLine {
			continue
		}
		if fr.EndLine == 0 || line <= fr.EndLine {
			return fr
		}
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

// callerSymbol is a no-op placeholder — we use Name directly since we don't
// store a reverse name→symbol map. CallerLine is read from the funcRange.
func callerSymbol(_ *sciplib.Document, _ string) string {
	return "" // we can't reverse-lookup caller symbol by name cheaply
}

// callerDefLine returns the definition line for the caller.
// Falls back to the funcRange StartLine when not found in defMap.
func callerDefLine(d defInfo, startLine uint32) uint32 {
	if d.Line != 0 {
		return d.Line
	}
	return startLine
}
