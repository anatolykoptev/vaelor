package main

import (
	"encoding/xml"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/compare"
)

// ---- XML types ----

type xmlResponse struct {
	XMLName       xml.Name             `xml:"response"`
	SchemaVersion string               `xml:"schemaVersion,attr"`
	Repo          xmlRepo              `xml:"repo"`
	Packages      xmlPackages          `xml:"packages"`
	Imports       *xmlImports          `xml:"imports,omitempty"`
	Files         *xmlFiles            `xml:"files,omitempty"`
	Tree          xmlCDATA             `xml:"tree,omitempty"`
	Symbols       xmlSymbols           `xml:"symbols"`
	APISurface    *xmlAPISummary       `xml:"apiSurface,omitempty"`
	Freshness     *xmlFreshnessSummary `xml:"freshness,omitempty"`
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
	Kind       string   `xml:"kind,attr"`
	Name       string   `xml:"name,attr"`
	Line       uint32   `xml:"line,attr"`
	End        uint32   `xml:"end,attr,omitempty"`
	Complexity int      `xml:"complexity,attr,omitempty"`
	Doc        string   `xml:"doc,attr,omitempty"`
	Signature  xmlCDATA `xml:"signature,omitempty"`
}

type xmlSymbol struct {
	Kind      string   `xml:"kind,attr"`
	Name      string   `xml:"name,attr"`
	File      string   `xml:"file,attr"`
	Line      uint32   `xml:"line,attr"`
	Signature xmlCDATA `xml:"signature,omitempty"`
}

type xmlSymbols struct {
	Items []xmlSymbol `xml:"symbol"`
}

// xmlAPISummary summarises the public/exported API surface of the repo.
type xmlAPISummary struct {
	ExportedCount int `xml:"exported,attr"`
}

// xmlFreshnessSummary summarises dependency freshness and vulnerability data.
type xmlFreshnessSummary struct {
	FreshRatio float64 `xml:"freshRatio,attr"`
	VulnCount  int     `xml:"vulnCount,attr"`
	TotalDeps  int     `xml:"totalDeps,attr"`
}

// repoAnalysisExtras holds optional enrichment data computed only in deep mode.
type repoAnalysisExtras struct {
	APISurfaceSize int
	FreshnessStats *compare.FreshnessStats
}

// applyExtras populates APISurface and Freshness fields on resp from extras.
func applyExtras(resp *xmlResponse, extras *repoAnalysisExtras) {
	if extras == nil {
		return
	}
	if extras.APISurfaceSize > 0 {
		resp.APISurface = &xmlAPISummary{ExportedCount: extras.APISurfaceSize}
	}
	if extras.FreshnessStats != nil {
		fs := extras.FreshnessStats
		resp.Freshness = &xmlFreshnessSummary{
			FreshRatio: fs.DepFreshnessRatio,
			VulnCount:  fs.VulnDeps,
			TotalDeps:  fs.TotalDeps,
		}
	}
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

// Depth-level limits for XML output.
const (
	overviewTreeLines  = 50
	overviewTopSymbols = 30

	deepTreeLines  = 200
	deepTopSymbols = 200

	moduleMaxFiles       = 30
	moduleMaxSymsPerFile = 20
	moduleTreeLines      = 100
	moduleTopSymbols     = 100
)

func limitsForDepth(depth string) depthLimits {
	switch depth {
	case analyze.DepthOverview:
		return depthLimits{
			maxFiles:       0,
			maxSymsPerFile: 0,
			includeDoc:     false,
			includeImports: false,
			treeLines:      overviewTreeLines,
			topSymbols:     overviewTopSymbols,
		}
	case analyze.DepthDeep:
		return depthLimits{
			maxFiles:       0, // 0 = all
			maxSymsPerFile: 0, // 0 = all
			includeDoc:     true,
			includeImports: true,
			treeLines:      deepTreeLines,
			topSymbols:     deepTopSymbols,
		}
	default: // "" or "module"
		return depthLimits{
			maxFiles:       moduleMaxFiles,
			maxSymsPerFile: moduleMaxSymsPerFile,
			includeDoc:     false,
			includeImports: true,
			treeLines:      moduleTreeLines,
			topSymbols:     moduleTopSymbols,
		}
	}
}
