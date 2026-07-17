package compound

import (
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/langutil"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// collectCallees walks the call graph for edges where sym is the caller and
// returns up to max unique callee references.
func collectCallees(cg *callgraph.CallGraph, sym *parser.Symbol, max int) []CallRef {
	var out []CallRef
	seen := make(map[string]struct{})
	for _, edge := range cg.Edges {
		if len(out) >= max {
			break
		}
		if edge.Caller != sym {
			continue
		}
		file := ""
		if edge.Callee != nil {
			file = edge.Callee.File
		}
		key := edge.CalleeName + ":" + file
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, CallRef{
			Name:     edge.CalleeName,
			File:     file,
			Line:     edge.Line,
			Receiver: edge.Receiver,
		})
	}
	return out
}

// collectCallers walks the call graph for edges where sym is the callee and
// returns up to max unique caller references.
func collectCallers(cg *callgraph.CallGraph, sym *parser.Symbol, max int) []CallRef {
	var out []CallRef
	seen := make(map[string]struct{})
	for _, edge := range cg.Edges {
		if len(out) >= max {
			break
		}
		if edge.Callee != sym || edge.Caller == nil {
			continue
		}
		key := edge.Caller.Name + ":" + edge.Caller.File
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		var kind string
		if edge.Caller.File == "" || edge.Caller.Kind == "external" {
			kind = langutil.CallerKindUnresolved
		} else {
			kind = langutil.CallerKind(edge.Caller.Name, edge.Caller.File)
		}
		out = append(out, CallRef{
			Name:       edge.Caller.Name,
			File:       edge.Caller.File,
			Line:       edge.Line,
			Receiver:   edge.Caller.Receiver,
			CallerKind: kind,
		})
	}
	return out
}
