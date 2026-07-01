package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// ---- JSON envelope types ----

// responseEnvelope wraps analysis results in a structured JSON envelope.
type responseEnvelope struct {
	SchemaVersion string          `json:"schemaVersion"`
	Data          envelopeData    `json:"data"`
	Meta          envelopeMeta    `json:"meta"`
	Suggested     []suggestedCall `json:"suggested,omitempty"`
}

type envelopeData struct {
	RepoName  string   `json:"repoName"`
	Language  string   `json:"language"`
	FileCount int      `json:"fileCount"`
	Packages  []string `json:"packages,omitempty"`
}

type envelopeMeta struct {
	FilesAnalyzed int `json:"filesAnalyzed"`
}

type suggestedCall struct {
	Tool   string         `json:"tool"`
	Reason string         `json:"reason"`
	Params map[string]any `json:"params,omitempty"`
}

// ---- Format dispatching ----

// formatAnalysisResult dispatches to xml, text, or JSON formatting based on format.
// Defaults to XML when format is empty. extras is optional deep-mode enrichment.
func formatAnalysisResult(r *analyze.RepoAnalysisResult, format, depth string, extras *repoAnalysisExtras) string {
	switch format {
	case formatJSON:
		return formatAnalysisJSON(r)
	case formatText:
		return formatAnalysisText(r)
	default:
		return formatAnalysisXML(r, depth, extras)
	}
}

// ---- JSON formatting ----

// formatAnalysisJSON formats a RepoAnalysisResult as a structured JSON envelope.
func formatAnalysisJSON(r *analyze.RepoAnalysisResult) string {
	env := responseEnvelope{
		SchemaVersion: "2.0",
		Data: envelopeData{
			RepoName:  r.RepoName,
			Language:  r.Language,
			FileCount: r.FileCount,
			Packages:  r.Packages,
		},
		Meta: envelopeMeta{
			FilesAnalyzed: r.FileCount,
		},
	}
	b, err := json.Marshal(env)
	if err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error())
	}
	return string(b)
}

// ---- Text formatting ----

// formatAnalysisText formats a RepoAnalysisResult as human-readable text.
func formatAnalysisText(r *analyze.RepoAnalysisResult) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "# Repository: %s\n", r.RepoName)
	fmt.Fprintf(&sb, "Language: %s | Files analyzed: %d\n\n", r.Language, r.FileCount)

	if len(r.Packages) > 0 {
		fmt.Fprintf(&sb, "## Packages (%d)\n", len(r.Packages))
		for _, pkg := range r.Packages {
			fmt.Fprintf(&sb, "  - %s\n", pkg)
		}
		sb.WriteString("\n")
	}

	if len(r.Symbols) > 0 {
		fmt.Fprintf(&sb, "## Key Symbols (%d)\n", len(r.Symbols))
		for _, sym := range r.Symbols {
			writeSymbolLine(&sb, sym)
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// writeSymbolLine writes a single symbol summary line into sb.
func writeSymbolLine(sb *strings.Builder, sym *parser.Symbol) {
	if sym.Signature != "" {
		fmt.Fprintf(sb, "  [%s] %s — %s (line %d)\n", sym.Kind, sym.Name, truncateSignature(sym.Signature), sym.StartLine)
	} else {
		fmt.Fprintf(sb, "  [%s] %s (line %d)\n", sym.Kind, sym.Name, sym.StartLine)
	}
}

// ---- Shared truncation helpers ----

// maxSignatureLen is the maximum length for symbol signatures before truncation.
const maxSignatureLen = 200

// truncateSignature truncates a signature to maxSignatureLen characters.
func truncateSignature(sig string) string {
	if len(sig) <= maxSignatureLen {
		return sig
	}
	return sig[:maxSignatureLen] + "..."
}

// maxDocLen is the maximum length for doc comments in per-file symbols.
const maxDocLen = 150

// truncateDoc truncates a doc comment, taking only the first line up to maxDocLen.
func truncateDoc(doc string) string {
	if idx := strings.IndexByte(doc, '\n'); idx >= 0 {
		doc = doc[:idx]
	}
	doc = strings.TrimSpace(doc)
	if len(doc) > maxDocLen {
		return doc[:maxDocLen] + "..."
	}
	return doc
}

// truncateTree limits tree output to maxLines.
func truncateTree(tree string, maxLines int) string {
	lines := strings.Split(tree, "\n")
	if len(lines) <= maxLines {
		return tree
	}
	remaining := len(lines) - maxLines
	truncated := lines[:maxLines]
	return strings.Join(truncated, "\n") + fmt.Sprintf("\n... (%d more)", remaining)
}
