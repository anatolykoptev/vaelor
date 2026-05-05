package main

import (
"encoding/xml"
"fmt"
"math"
"sort"
"strings"

"github.com/anatolykoptev/go-code/internal/analyze"
"github.com/anatolykoptev/go-code/internal/parser"
)

// formatAnalysisXML formats a RepoAnalysisResult as structured XML (schema v2.0).
// extras is optional deep-mode enrichment (API surface size, freshness stats).
func formatAnalysisXML(r *analyze.RepoAnalysisResult, depth string, extras *repoAnalysisExtras) string {
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

if treeStr := truncateTree(r.FileTree, limits.treeLines); treeStr != "" {
resp.Tree = &xmlCDATA{Inner: wrapCDATA(treeStr)}
}

if resp.Files == nil {
top := buildTopSymbols(r.Symbols, limits.topSymbols, depth)
resp.Symbols = &top
}

applyExtras(&resp, extras)

b, err := xml.MarshalIndent(resp, "", "  ")
if err != nil {
return fmt.Sprintf("<error>%s</error>", err.Error())
}
return xml.Header + string(b)
}

// buildTopSymbols builds the backward-compatible top-level <symbols> section.
func buildTopSymbols(syms []*parser.Symbol, limit int, depth string) xmlSymbols {
isOverview := depth == analyze.DepthOverview
symbols := make([]xmlSymbol, 0, min(len(syms), limit))
for _, sym := range syms {
if len(symbols) >= limit {
break
}
if isOverview {
if len(sym.Name) > 0 && sym.Name[0] >= 'a' && sym.Name[0] <= 'z' {
continue
}
if strings.HasSuffix(sym.File, "_test.go") {
continue
}
}
xs := xmlSymbol{
Kind: string(sym.Kind),
Name: sym.Name,
File: sym.File,
Line: sym.StartLine,
}
if !signatureIsTrivial(string(sym.Kind), sym.Name, sym.Signature) {
xs.Signature = &xmlCDATA{Inner: wrapCDATA(truncateSignature(sym.Signature))}
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
if !signatureIsTrivial(string(sym.Kind), sym.Name, sym.Signature) {
xs.Signature = &xmlCDATA{Inner: wrapCDATA(truncateSignature(sym.Signature))}
}
syms = append(syms, xs)
}
return syms
}

const roundPrecision = 100 // 10^N for N decimal places

// roundScore rounds a float to 2 decimal places.
func roundScore(f float64) float64 {
return math.Round(f*roundPrecision) / roundPrecision
}

// signatureIsTrivial returns true when the signature only restates
// kind+name (e.g. "type X struct", "type X interface") -- those facts
// are already in the kind/name attributes, so emitting the signature
// wastes bytes without adding agent-usable information. Function and
// method signatures, and consts/vars with values, are kept.
func signatureIsTrivial(kind, name, sig string) bool {
sig = strings.TrimSpace(sig)
if sig == "" {
return true
}
return sig == "type "+name+" "+kind
}
