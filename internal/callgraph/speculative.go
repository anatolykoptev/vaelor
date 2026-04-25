package callgraph

import (
	"context"
	"log"
	"regexp"
	"strings"

	"github.com/anatolykoptev/go-code/internal/oxcodes"
	"github.com/anatolykoptev/go-code/internal/parser"
)

const (
	maxSpeculativePerTrace   = 20
	speculativeMaxResults    = 5
	speculativeMaxCandidates = 3
	confidenceExact          = 0.8
	confidencePartial        = 0.5
)

// speculativeCounter is a shared counter passed during tree walk.
type speculativeCounter struct {
	count int
}

// buildSearchPattern returns a regex pattern for the given callee name and language.
func buildSearchPattern(callName, language string) string {
	escaped := regexp.QuoteMeta(callName)
	switch strings.ToLower(language) {
	case "go", "golang":
		return `\bfunc\s+` + escaped + `\s*\(`
	case "typescript", "ts", "javascript", "js":
		return `\bfunction\s+` + escaped + `\s*\(|\b` + escaped + `\s*[=:]\s*(?:async\s*)?\(`
	case "python", "py":
		return `\bdef\s+` + escaped + `\s*\(`
	case "rust", "rs":
		return `\bfn\s+` + escaped + `\s*\(`
	default:
		return `\b` + escaped + `\(`
	}
}

// ResolveSpeculative walks the call tree and fills Speculative candidates for
// unresolved nodes (Symbol.Kind == "external") using ox-codes text search.
// At most maxSpeculativePerTrace resolutions are attempted for performance.
func ResolveSpeculative(ctx context.Context, client *oxcodes.Client, root, language string, nodes []CallChainNode) {
	counter := &speculativeCounter{}
	for i := range nodes {
		resolveSpeculativeNode(ctx, client, root, language, &nodes[i], counter)
	}
}

func resolveSpeculativeNode(ctx context.Context, client *oxcodes.Client, root, language string, node *CallChainNode, counter *speculativeCounter) {
	if counter.count >= maxSpeculativePerTrace {
		return
	}

	// This node is unresolved if its symbol has kind "external".
	if node.Symbol != nil && node.Symbol.Kind == parser.NodeKind("external") {
		candidates := searchCandidates(ctx, client, root, language, node.Symbol.Name, counter)
		if len(candidates) > 0 {
			node.Speculative = candidates
		}
	}

	// Recurse into children.
	for i := range node.Children {
		if counter.count >= maxSpeculativePerTrace {
			return
		}
		resolveSpeculativeNode(ctx, client, root, language, &node.Children[i], counter)
	}
}

func searchCandidates(ctx context.Context, client *oxcodes.Client, root, language, callName string, counter *speculativeCounter) []SpeculativeCall {
	counter.count++

	pattern := buildSearchPattern(callName, language)
	resp, err := client.Search(ctx, oxcodes.SearchInput{
		Root:        root,
		Pattern:     pattern,
		IsRegex:     true,
		MaxResults:  speculativeMaxResults,
		Language:    language,
		ExcludeGlob: "*_test.go,*_test.ts,*_test.py,*_spec.*",
	})
	if err != nil {
		log.Printf("speculative resolution: ox-codes search %q: %v", pattern, err)
		return nil
	}

	var candidates []SpeculativeCall
	for _, m := range resp.Matches {
		if len(candidates) >= speculativeMaxCandidates {
			break
		}
		// All regex matches get confidenceExact — the pattern ensures function definition context.
		candidates = append(candidates, SpeculativeCall{
			Name:       callName,
			File:       m.File,
			Line:       m.Line,
			Confidence: confidenceExact,
		})
	}
	return candidates
}
