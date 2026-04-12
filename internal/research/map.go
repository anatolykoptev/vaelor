package research

import (
	"fmt"
	"strings"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// RenderMap produces an Aider-style compact text map from the pruned file set.
// Format per file:
//
//	internal/parser/parser.go  [distance=0, seed]
//	    ParseFile(path string, opts ParseOpts) (*ParseResult, error)
//	    ParseResult
//
// If includeBody is true, function bodies are included (higher token cost).
// rootPrefix is stripped from file paths (e.g. "/tmp/go-code-workspace/owner_repo/").
func RenderMap(files []scoredFile, includeBody bool, rootPrefix string) string {
	if len(files) == 0 {
		return ""
	}
	strip := ""
	if rootPrefix != "" {
		strip = strings.TrimSuffix(rootPrefix, "/") + "/"
	}
	var sb strings.Builder
	for _, sf := range files {
		// Skip files with no symbols — they're just path noise.
		if len(sf.symbols) == 0 {
			continue
		}
		renderFileEntry(&sb, sf, includeBody, strip)
	}
	return sb.String()
}

func renderFileEntry(sb *strings.Builder, sf scoredFile, includeBody bool, strip string) {
	relPath := sf.expand.relPath
	if strip != "" {
		relPath = strings.TrimPrefix(relPath, strip)
	}

	// Header line: path + link annotation.
	annotation := buildAnnotation(sf)
	fmt.Fprintf(sb, "%s  %s\n", relPath, annotation)

	// Symbols — grouped by kind for readability.
	types := filterByKinds(sf.symbols, parser.KindClass, parser.KindInterface, parser.KindStruct)
	funcs := filterByKinds(sf.symbols, parser.KindFunction, parser.KindMethod)
	others := filterByKinds(sf.symbols, parser.KindVar, parser.KindConst)

	for _, sym := range types {
		fmt.Fprintf(sb, "    %s\n", sym.Name)
	}
	for _, sym := range funcs {
		sig := buildSignature(sym)
		fmt.Fprintf(sb, "    %s\n", sig)
		if includeBody && sym.Body != "" {
			indented := indentBody(sym.Body, "        ")
			fmt.Fprintf(sb, "%s\n", indented)
		}
	}
	for _, sym := range others {
		fmt.Fprintf(sb, "    %s\n", sym.Name)
	}
}

func buildAnnotation(sf scoredFile) string {
	if sf.expand.distance == 0 {
		return "[seed]"
	}
	return fmt.Sprintf("[distance=%d, %s]", sf.expand.distance, sf.expand.whyLinked)
}

func buildSignature(sym *parser.Symbol) string {
	if sym.Signature != "" {
		return sym.Signature
	}
	return sym.Name
}

func filterByKinds(symbols []*parser.Symbol, kinds ...parser.NodeKind) []*parser.Symbol {
	var out []*parser.Symbol
	for _, sym := range symbols {
		for _, k := range kinds {
			if sym.Kind == k {
				out = append(out, sym)
				break
			}
		}
	}
	return out
}

func indentBody(body, prefix string) string {
	lines := strings.Split(body, "\n")
	var sb strings.Builder
	for _, line := range lines {
		if line == "" {
			sb.WriteByte('\n')
		} else {
			sb.WriteString(prefix)
			sb.WriteString(line)
			sb.WriteByte('\n')
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}
