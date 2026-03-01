package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"math"
	"sort"
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
	SuggestedNext []suggestedCall `json:"suggestedNextCalls,omitempty"`
}

type envelopeData struct {
	RepoName  string   `json:"repoName"`
	Language  string   `json:"language"`
	FileCount int      `json:"fileCount"`
	Packages  []string `json:"packages,omitempty"`
}

type envelopeMeta struct {
	FilesAnalyzed int  `json:"filesAnalyzed"`
	Truncated     bool `json:"truncated"`
}

type suggestedCall struct {
	Tool   string            `json:"tool"`
	Params map[string]string `json:"params"`
	Reason string            `json:"reason"`
}

// ---- V2 XML types ----

// xmlResponse is the top-level XML envelope for repo_analyze output (schema v2.0).
type xmlResponse struct {
	XMLName       xml.Name    `xml:"response"`
	SchemaVersion string      `xml:"schemaVersion,attr"`
	Repo          xmlRepo     `xml:"repo"`
	Packages      xmlPackages `xml:"packages"`
	Imports       *xmlImports `xml:"imports,omitempty"`
	Files         *xmlFiles   `xml:"files,omitempty"`
	Tree          string      `xml:"tree,omitempty"`
	Symbols       xmlSymbols  `xml:"symbols"`
}

type xmlRepo struct {
	Name       string    `xml:"name,attr"`
	Language   string    `xml:"language,attr"`
	Files      int       `xml:"files,attr"`
	TotalBytes int64     `xml:"totalBytes,attr,omitempty"`
	Skipped    int       `xml:"skipped,attr,omitempty"`
	Languages  []xmlLang `xml:"languages>lang,omitempty"`
}

type xmlLang struct {
	Name  string `xml:"name,attr"`
	Files int    `xml:"files,attr"`
}

type xmlPackages struct {
	Items []string `xml:"package"`
}

type xmlImports struct {
	Packages []xmlImportPkg `xml:"pkg"`
}

type xmlImportPkg struct {
	Name string   `xml:"name,attr"`
	Deps []string `xml:"dep"`
}

type xmlFiles struct {
	Items []xmlFile `xml:"file"`
}

type xmlFile struct {
	Path       string       `xml:"path,attr"`
	Lang       string       `xml:"lang,attr,omitempty"`
	Size       int64        `xml:"size,attr,omitempty"`
	Lines      int          `xml:"lines,attr,omitempty"`
	Relevance  float64      `xml:"relevance,attr,omitempty"`
	ImportedBy int          `xml:"importedBy,attr,omitempty"`
	Symbols    []xmlFileSym `xml:"sym,omitempty"`
	Imports    []string     `xml:"imp,omitempty"`
}

type xmlFileSym struct {
	Kind       string `xml:"kind,attr"`
	Name       string `xml:"name,attr"`
	Line       uint32 `xml:"line,attr"`
	End        uint32 `xml:"end,attr,omitempty"`
	Complexity int    `xml:"complexity,attr,omitempty"`
	Doc        string `xml:"doc,attr,omitempty"`
	Signature  string `xml:",chardata"`
}

type xmlSymbol struct {
	Kind      string `xml:"kind,attr"`
	Name      string `xml:"name,attr"`
	File      string `xml:"file,attr"`
	Line      uint32 `xml:"line,attr"`
	Signature string `xml:"signature,omitempty"`
}

type xmlSymbols struct {
	Items []xmlSymbol `xml:"symbol"`
}

// ---- Depth limits ----

// depthLimits controls how much data each depth level includes in XML output.
type depthLimits struct {
	maxFiles       int  // 0 = no files section
	maxSymsPerFile int  // 0 = no per-file symbols
	includeDoc     bool // include doc comments in per-file symbols
	includeImports bool // include <imports> section
	treeLines      int  // max tree output lines
	topSymbols     int  // top-level <symbols> count
}

func limitsForDepth(depth string) depthLimits {
	switch depth {
	case analyze.DepthOverview:
		return depthLimits{
			maxFiles:       0,
			maxSymsPerFile: 0,
			includeDoc:     false,
			includeImports: false,
			treeLines:      50, //nolint:mnd
			topSymbols:     50, //nolint:mnd
		}
	case analyze.DepthDeep:
		return depthLimits{
			maxFiles:       0, // 0 = all
			maxSymsPerFile: 0, // 0 = all
			includeDoc:     true,
			includeImports: true,
			treeLines:      200, //nolint:mnd
			topSymbols:     200, //nolint:mnd
		}
	default: // "" or "module"
		return depthLimits{
			maxFiles:       30,  //nolint:mnd
			maxSymsPerFile: 20,  //nolint:mnd
			includeDoc:     false,
			includeImports: true,
			treeLines:      100, //nolint:mnd
			topSymbols:     100, //nolint:mnd
		}
	}
}

// ---- Format dispatching ----

// formatAnalysisResult dispatches to xml, text, or JSON formatting based on format.
// Defaults to XML when format is empty.
func formatAnalysisResult(r *analyze.RepoAnalysisResult, format, depth string) string {
	switch format {
	case formatJSON:
		return formatAnalysisJSON(r)
	case formatText:
		return formatAnalysisText(r)
	default:
		return formatAnalysisXML(r, depth)
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
	b, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return fmt.Sprintf(`{"error": %q}`, err.Error())
	}
	return string(b)
}

// ---- XML formatting ----

// formatAnalysisXML formats a RepoAnalysisResult as structured XML (schema v2.0).
func formatAnalysisXML(r *analyze.RepoAnalysisResult, depth string) string {
	limits := limitsForDepth(depth)

	resp := xmlResponse{
		SchemaVersion: "2.0",
		Repo: xmlRepo{
			Name:       r.RepoName,
			Language:   r.Language,
			Files:      r.FileCount,
			TotalBytes: r.TotalBytes,
			Skipped:    r.Skipped,
			Languages:  buildSortedLangs(r.Languages),
		},
		Packages: xmlPackages{Items: r.Packages},
	}

	if limits.includeImports && len(r.ImportGraph) > 0 {
		resp.Imports = buildXMLImports(r.ImportGraph)
	}

	if limits.maxFiles != 0 || depth == analyze.DepthDeep {
		resp.Files = buildXMLFiles(r.Files, limits)
	}

	if r.FileTree != "" {
		resp.Tree = truncateTree(r.FileTree, limits.treeLines)
	}

	resp.Symbols = buildTopSymbols(r.Symbols, limits.topSymbols)

	b, err := xml.MarshalIndent(resp, "", "  ")
	if err != nil {
		return fmt.Sprintf("<error>%s</error>", err.Error())
	}
	return xml.Header + string(b)
}

// buildTopSymbols builds the backward-compatible top-level <symbols> section.
func buildTopSymbols(syms []*parser.Symbol, limit int) xmlSymbols {
	symbols := make([]xmlSymbol, 0, min(len(syms), limit))
	for _, sym := range syms {
		if len(symbols) >= limit {
			break
		}
		xs := xmlSymbol{
			Kind: string(sym.Kind),
			Name: sym.Name,
			File: sym.File,
			Line: sym.StartLine,
		}
		if sym.Signature != "" {
			xs.Signature = truncateSignature(sym.Signature)
		}
		symbols = append(symbols, xs)
	}
	return xmlSymbols{Items: symbols}
}

// buildSortedLangs converts a language map to sorted xmlLang slice (descending by count).
func buildSortedLangs(languages map[string]int) []xmlLang {
	if len(languages) == 0 {
		return nil
	}
	langs := make([]xmlLang, 0, len(languages))
	for name, count := range languages {
		langs = append(langs, xmlLang{Name: name, Files: count})
	}
	sort.Slice(langs, func(i, j int) bool {
		return langs[i].Files > langs[j].Files
	})
	return langs
}

// buildXMLImports converts the import graph to XML structure.
func buildXMLImports(ig map[string][]string) *xmlImports {
	names := make([]string, 0, len(ig))
	for name := range ig {
		names = append(names, name)
	}
	sort.Strings(names)

	pkgs := make([]xmlImportPkg, 0, len(ig))
	for _, name := range names {
		deps := ig[name]
		if len(deps) == 0 {
			continue
		}
		pkgs = append(pkgs, xmlImportPkg{Name: name, Deps: deps})
	}
	if len(pkgs) == 0 {
		return nil
	}
	return &xmlImports{Packages: pkgs}
}

// buildXMLFiles converts AnalyzedFile slice to XML structure with depth limits.
func buildXMLFiles(files []analyze.AnalyzedFile, limits depthLimits) *xmlFiles {
	maxFiles := limits.maxFiles
	if maxFiles == 0 {
		maxFiles = len(files) // 0 means all for deep mode
	}

	items := make([]xmlFile, 0, min(len(files), maxFiles))
	for i, af := range files {
		if i >= maxFiles {
			break
		}
		xf := xmlFile{
			Path:       af.RelPath,
			Lang:       af.Language,
			Size:       af.Size,
			Lines:      af.Lines,
			Relevance:  roundScore(af.Relevance),
			ImportedBy: af.ImportedBy,
			Imports:    af.Imports,
		}

		if limits.maxSymsPerFile != 0 || limits.includeDoc {
			xf.Symbols = buildFileSymbols(af, limits)
		}

		items = append(items, xf)
	}
	if len(items) == 0 {
		return nil
	}
	return &xmlFiles{Items: items}
}

// buildFileSymbols converts a file's symbols to XML with depth-dependent limits.
func buildFileSymbols(af analyze.AnalyzedFile, limits depthLimits) []xmlFileSym {
	maxSyms := limits.maxSymsPerFile
	if maxSyms == 0 {
		maxSyms = len(af.Symbols) // 0 = all for deep
	}

	syms := make([]xmlFileSym, 0, min(len(af.Symbols), maxSyms))
	for j, sym := range af.Symbols {
		if j >= maxSyms {
			break
		}
		xs := xmlFileSym{
			Kind: string(sym.Kind),
			Name: sym.Name,
			Line: sym.StartLine,
			End:  sym.EndLine,
		}
		if sym.Complexity > 0 {
			xs.Complexity = sym.Complexity
		}
		if limits.includeDoc && sym.DocComment != "" {
			xs.Doc = truncateDoc(sym.DocComment)
		}
		if sym.Signature != "" {
			xs.Signature = truncateSignature(sym.Signature)
		}
		syms = append(syms, xs)
	}
	return syms
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

// roundScore rounds a float to 2 decimal places.
func roundScore(f float64) float64 {
	return math.Round(f*100) / 100 //nolint:mnd
}
