// Package compound provides high-level compound analysis tools that aggregate
// multiple lower-level analysis primitives into a single result.
package compound

import (
	"context"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/oxcodes"
	"github.com/anatolykoptev/go-code/internal/parser"
)

const (
	defaultMaxCallees = 20
	defaultMaxCallers = 20
)

// UnderstandOpts configures the understand compound analysis.
type UnderstandOpts struct {
	IncludeCallers bool
	MaxCallees     int // default 20
	MaxCallers     int // default 20

	// OxCodes enables body analysis via ox-codes scoped search. Optional.
	OxCodes *oxcodes.Client

	// Root is the repository root path, required when OxCodes is set.
	Root string
}

// UnderstandResult is the output of the understand compound tool.
type UnderstandResult struct {
	Symbol   SymbolInfo    `json:"symbol"`
	Callees  []CallRef     `json:"callees,omitempty"`
	Callers  []CallRef     `json:"callers,omitempty"`
	Tier     string        `json:"tier"`
	Warnings []string      `json:"warnings,omitempty"`
	Body     *BodyAnalysis `json:"body_analysis,omitempty"`
}

// SymbolInfo is a summary of a symbol for compound tool output.
type SymbolInfo struct {
	Name       string `json:"name"`
	Kind       string `json:"kind"`
	File       string `json:"file"`
	StartLine  uint32 `json:"start_line"`
	EndLine    uint32 `json:"end_line"`
	Signature  string `json:"signature,omitempty"`
	Complexity int    `json:"complexity,omitempty"`
	Receiver   string `json:"receiver,omitempty"`
}

// CallRef is a reference to a called/calling function.
type CallRef struct {
	Name     string `json:"name"`
	File     string `json:"file"`
	Line     uint32 `json:"line"`
	Receiver string `json:"receiver,omitempty"`
}

// MatchRef is a lightweight symbol descriptor used in disambiguation responses.
type MatchRef struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	File     string `json:"file"`
	Line     uint32 `json:"line"`
	Receiver string `json:"receiver,omitempty"`
}

// FindSymbol returns all function/method symbols matching the given name.
func FindSymbol(symbols []*parser.Symbol, name string) []*parser.Symbol {
	var matches []*parser.Symbol
	for _, sym := range symbols {
		if sym.Name == name && (sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod) {
			matches = append(matches, sym)
		}
	}
	return matches
}

// Understand performs a deep-dive analysis of a single symbol.
func Understand(ctx context.Context, sym *parser.Symbol, cg *callgraph.CallGraph, opts UnderstandOpts) *UnderstandResult {
	maxCallees := opts.MaxCallees
	if maxCallees <= 0 {
		maxCallees = defaultMaxCallees
	}
	maxCallers := opts.MaxCallers
	if maxCallers <= 0 {
		maxCallers = defaultMaxCallers
	}

	result := &UnderstandResult{
		Symbol: SymbolInfo{
			Name:       sym.Name,
			Kind:       string(sym.Kind),
			File:       sym.File,
			StartLine:  sym.StartLine,
			EndLine:    sym.EndLine,
			Signature:  sym.Signature,
			Complexity: sym.Complexity,
			Receiver:   sym.Receiver,
		},
		Tier: cg.Tier,
	}

	// Collect callees: edges where Caller == sym.
	seen := make(map[string]struct{})
	for _, edge := range cg.Edges {
		if len(result.Callees) >= maxCallees {
			break
		}
		if edge.Caller != sym {
			continue
		}
		name := edge.CalleeName
		file := ""
		line := edge.Line
		receiver := edge.Receiver
		if edge.Callee != nil {
			file = edge.Callee.File
		}
		key := name + ":" + file
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		result.Callees = append(result.Callees, CallRef{
			Name:     name,
			File:     file,
			Line:     line,
			Receiver: receiver,
		})
	}

	if !opts.IncludeCallers {
		if opts.OxCodes != nil {
			result.Body = AnalyzeBody(ctx, opts.OxCodes, opts.Root, sym)
		}
		return result
	}

	// Collect callers: edges where Callee == sym.
	seenCallers := make(map[string]struct{})
	for _, edge := range cg.Edges {
		if len(result.Callers) >= maxCallers {
			break
		}
		if edge.Callee != sym {
			continue
		}
		if edge.Caller == nil {
			continue
		}
		name := edge.Caller.Name
		file := edge.Caller.File
		key := name + ":" + file
		if _, dup := seenCallers[key]; dup {
			continue
		}
		seenCallers[key] = struct{}{}
		result.Callers = append(result.Callers, CallRef{
			Name:     name,
			File:     file,
			Line:     edge.Line,
			Receiver: edge.Caller.Receiver,
		})
	}

	if opts.OxCodes != nil {
		result.Body = AnalyzeBody(ctx, opts.OxCodes, opts.Root, sym)
	}

	return result
}
