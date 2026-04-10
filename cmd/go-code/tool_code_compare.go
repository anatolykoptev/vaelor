package main

import (
	"context"
	"encoding/xml"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/compare"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type xmlCompareResponse struct {
	XMLName xml.Name   `xml:"response"`
	Compare xmlCompare `xml:"compare"`
}

type xmlCompare struct {
	RepoA          string                `xml:"repoA,attr"`
	RepoB          string                `xml:"repoB,attr"`
	Query          string                `xml:"query,attr"`
	MatchedSymbols int                   `xml:"matchedSymbols,attr"`
	UnmatchedA     int                   `xml:"unmatchedA,attr"`
	UnmatchedB     int                   `xml:"unmatchedB,attr"`
	MetricsA       xmlCompMetrics        `xml:"metricsA"`
	MetricsB       xmlCompMetrics        `xml:"metricsB"`
	MatchBreakdown xmlMatchBreak         `xml:"matchBreakdown"`
	ImportDiff     xmlImportDiff         `xml:"importDiff"`
	DiffStats      *xmlDiffStats         `xml:"diffStats,omitempty"`
	Analysis       xmlAnalysis           `xml:"analysis"`
	HotspotsA      *xmlHotspots          `xml:"hotspotsA,omitempty"`
	HotspotsB      *xmlHotspots          `xml:"hotspotsB,omitempty"`
	RelStatsA      *xmlRelStats          `xml:"relStatsA,omitempty"`
	RelStatsB      *xmlRelStats          `xml:"relStatsB,omitempty"`
	QualityA       *xmlQualityIndicators `xml:"qualityA,omitempty"`
	QualityB       *xmlQualityIndicators `xml:"qualityB,omitempty"`
	FreshnessA     *xmlFreshness         `xml:"freshnessA,omitempty"`
	FreshnessB     *xmlFreshness         `xml:"freshnessB,omitempty"`
	DataflowA      *xmlCompareDataflow   `xml:"dataflowA,omitempty"`
	DataflowB      *xmlCompareDataflow   `xml:"dataflowB,omitempty"`
	APIDiff        *xmlAPIDiff           `xml:"apiDiff,omitempty"`
	RouteDiff      *xmlRouteDiff         `xml:"routeDiff,omitempty"`
}

type xmlQualityIndicators struct {
	TodoCount     int `xml:"todoCount,attr"`
	ErrorPatterns int `xml:"errorPatterns,attr"`
	PanicCount    int `xml:"panicCount,attr"`
	MagicNumbers  int `xml:"magicNumbers,attr"`
}

type xmlCompMetrics struct {
	Files              int     `xml:"files,attr"`
	TotalLines         int     `xml:"totalLines,attr"`
	AvgFuncLines       float64 `xml:"avgFuncLines,attr"`
	MaxFuncLines       int     `xml:"maxFuncLines,attr"`
	AvgComplexity      float64 `xml:"avgComplexity,attr"`
	MaxComplexity      int     `xml:"maxComplexity,attr"`
	TestRatio          float64 `xml:"testRatio,attr"`
	DocRatio           float64 `xml:"docRatio,attr"`
	ErrorHandlingRatio float64 `xml:"errorHandlingRatio,attr"`
	Interfaces         int     `xml:"interfaces,attr"`
	ExternalDeps       int     `xml:"externalDeps,attr"`
	Grade              string  `xml:"grade,attr"`

	AvgCognitiveComplexity float64 `xml:"avgCognitiveComplexity,attr"`
	MaxCognitiveComplexity int     `xml:"maxCognitiveComplexity,attr"`
	AvgNestingDepth        float64 `xml:"avgNestingDepth,attr"`
	MaxNestingDepth        int     `xml:"maxNestingDepth,attr"`
	LargeFileRatio         float64 `xml:"largeFileRatio,attr"`
	DuplicationRatio       float64 `xml:"duplicationRatio,attr"`
	MagicNumberRatio       float64 `xml:"magicNumberRatio,attr"`
	AvgParamCount          float64 `xml:"avgParamCount,attr"`
	MaxParamCount          int     `xml:"maxParamCount,attr"`
	Score                  float64 `xml:"score,attr"`
	SemanticDupRatio       float64 `xml:"semanticDupRatio,attr,omitempty"`
}

type xmlMatchBreak struct {
	Exact    int `xml:"exact,attr"`
	Modified int `xml:"modified,attr"`
	Fuzzy    int `xml:"fuzzy,attr"`
	Renamed  int `xml:"renamed,attr"`
	Semantic int `xml:"semantic,attr"`
	Moved    int `xml:"moved,attr"`
}

type xmlFreshness struct {
	DepRatio   float64 `xml:"depRatio,attr"`
	VulnRatio  float64 `xml:"vulnRatio,attr"`
	TotalDeps  int     `xml:"totalDeps,attr"`
	Outdated   int     `xml:"outdated,attr"`
	Vulnerable int     `xml:"vulnerable,attr"`
}

type xmlCompareDataflow struct {
	DeadStores    int `xml:"deadStores,attr"`
	UnusedVars    int `xml:"unusedVars,attr"`
	TotalFindings int `xml:"totalFindings,attr"`
	FilesAnalyzed int `xml:"filesAnalyzed,attr"`
}

type xmlAPIDiff struct {
	Common     int            `xml:"common,attr"`
	OnlyACount int            `xml:"onlyACount,attr"`
	OnlyBCount int            `xml:"onlyBCount,attr"`
	ChangedSig int            `xml:"changedSig,attr"`
	OnlyA      []xmlAPISymbol `xml:"onlyA>sym,omitempty"`
	OnlyB      []xmlAPISymbol `xml:"onlyB>sym,omitempty"`
	Changed    []xmlAPIDelta  `xml:"changed>delta,omitempty"`
}

type xmlAPISymbol struct {
	Name      string `xml:"name,attr"`
	Kind      string `xml:"kind,attr"`
	Signature string `xml:"sig,attr"`
	Package   string `xml:"pkg,attr"`
}

type xmlAPIDelta struct {
	Name string `xml:"name,attr"`
	Kind string `xml:"kind,attr"`
	SigA string `xml:"sigA,attr"`
	SigB string `xml:"sigB,attr"`
}

type xmlRouteDiff struct {
	Common     int        `xml:"common,attr"`
	OnlyACount int        `xml:"onlyACount,attr"`
	OnlyBCount int        `xml:"onlyBCount,attr"`
	OnlyA      []xmlRoute `xml:"onlyA>route,omitempty"`
	OnlyB      []xmlRoute `xml:"onlyB>route,omitempty"`
}

type xmlRoute struct {
	Method  string `xml:"method,attr"`
	Path    string `xml:"path,attr"`
	Handler string `xml:"handler,attr"`
}

type xmlImportDiff struct {
	CommonCount int      `xml:"common,attr"`
	OnlyACount  int      `xml:"onlyACount,attr"`
	OnlyBCount  int      `xml:"onlyBCount,attr"`
	StdlibA     int      `xml:"stdlibA,attr"`
	StdlibB     int      `xml:"stdlibB,attr"`
	ExternalA   int      `xml:"externalA,attr"`
	ExternalB   int      `xml:"externalB,attr"`
	OnlyA       []string `xml:"onlyA>dep,omitempty"`
	OnlyB       []string `xml:"onlyB>dep,omitempty"`
	FrameworksA []string `xml:"frameworksA>fw,omitempty"`
	FrameworksB []string `xml:"frameworksB>fw,omitempty"`
}

type xmlDiffStats struct {
	ModifiedWithDiff int `xml:"modified,attr"`
	TotalInserts     int `xml:"inserts,attr"`
	TotalDeletes     int `xml:"deletes,attr"`
	TotalUpdates     int `xml:"updates,attr"`
	TotalMoves       int `xml:"moves,attr"`
}

type xmlAnalysis struct {
	Quality         []xmlQuality     `xml:"quality,omitempty"`
	Gaps            []xmlGap         `xml:"gap,omitempty"`
	Architecture    []xmlArchInsight `xml:"architecture,omitempty"`
	Recommendations []string         `xml:"recommendation,omitempty"`
}

type xmlQuality struct {
	Aspect   string `xml:"aspect,attr"`
	Winner   string `xml:"winner,attr"`
	Reason   string `xml:"reason,attr"`
	SnippetA string `xml:"snippetA,omitempty"`
	SnippetB string `xml:"snippetB,omitempty"`
}

type xmlGap struct {
	MissingIn  string `xml:"missingIn,attr"`
	Feature    string `xml:"feature,attr"`
	Importance string `xml:"importance,attr"`
	Location   string `xml:"location,attr,omitempty"`
}

type xmlArchInsight struct {
	Insight string `xml:"insight,attr"`
	Source  string `xml:"source,attr"`
	Example string `xml:"example,omitempty"`
	Benefit string `xml:"benefit,omitempty"`
}

type xmlHotspots struct {
	Items []xmlHotspot `xml:"hotspot"`
}

type xmlHotspot struct {
	File       string  `xml:"file,attr"`
	Score      float64 `xml:"score,attr"`
	Churn      int     `xml:"churn,attr"`
	Complexity float64 `xml:"complexity,attr"`
	Risk       string  `xml:"risk,attr"`
}

type xmlRelStats struct {
	Total          int `xml:"total,attr"`
	Extends        int `xml:"extends,attr"`
	Implements     int `xml:"implements,attr"`
	Embeds         int `xml:"embeds,attr"`
	UniqueSubjects int `xml:"uniqueSubjects,attr"`
}

// CodeCompareInput is the input schema for the code_compare tool.
type CodeCompareInput struct {
	RepoA    string `json:"repo_a" jsonschema_description:"First repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`
	RepoB    string `json:"repo_b" jsonschema_description:"Second repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`
	Query    string `json:"query,omitempty" jsonschema_description:"What to compare — quality aspects, architectural patterns, specific concerns (default: general comparison)"`
	Focus    string `json:"focus,omitempty" jsonschema_description:"Subdirectory path to limit comparison scope (e.g. internal/auth, pkg/api), or space-separated keywords (e.g. 'auth handler'). Use query for topic focus."`
	Language string `json:"language,omitempty" jsonschema_description:"Limit comparison to files of this language (e.g. go, python, rust)"`
}

// registerCodeCompare registers the code_compare MCP tool.
func registerCodeCompare(server *mcp.Server, cfg Config, deps analyze.Deps) {
	outputDir := cfg.OutputDir

	mcpserver.AddTool(server, &mcp.Tool{
		Name: "code_compare",
		Description: "Compare two code repositories to find the better implementation. " +
			"Analyzes architecture, code quality, patterns, and identifies missing features. " +
			"Returns XML with quality verdicts, coverage gaps, architecture insights, " +
			"metrics, and actionable recommendations. Works cross-language.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodeCompareInput) (*mcp.CallToolResult, error) {
		if input.RepoA == "" || input.RepoB == "" {
			return errResult("repo_a and repo_b are required"), nil
		}
		if input.Query == "" {
			input.Query = "Compare architecture, code quality, patterns, and identify missing features"
		}

		rootA, cleanupA, err := resolveRoot(ctx, input.RepoA, "", deps)
		if err != nil {
			return errResult(fmt.Sprintf("resolve repo_a: %s", err)), nil
		}
		defer cleanupA()

		rootB, cleanupB, err := resolveRoot(ctx, input.RepoB, "", deps)
		if err != nil {
			return errResult(fmt.Sprintf("resolve repo_b: %s", err)), nil
		}
		defer cleanupB()

		result, err := compare.CompareRepos(ctx, compare.CompareInput{
			RootA:   rootA,
			RootB:   rootB,
			Query:   input.Query,
			OxCodes: deps.OxCodes,
			Opts: compare.SnapshotOpts{
				Focus:    input.Focus,
				Language: input.Language,
			},
		}, deps.LLM)
		if err != nil {
			return errResult(fmt.Sprintf("compare: %s", err)), nil
		}

		return xmlMarshalFileResult(buildCompareXML(result), "code_compare", outputDir), nil
	})
}

func buildCompareXML(r *compare.CompareResult) xmlCompareResponse {
	resp := xmlCompareResponse{
		Compare: xmlCompare{
			RepoA:          r.RepoA,
			RepoB:          r.RepoB,
			Query:          r.Query,
			MatchedSymbols: r.MatchedSymbols,
			UnmatchedA:     r.UnmatchedA,
			UnmatchedB:     r.UnmatchedB,
			MetricsA:       convertMetrics(r.MetricsA),
			MetricsB:       convertMetrics(r.MetricsB),
			MatchBreakdown: xmlMatchBreak{
				Exact:    r.MatchBreakdown.Exact,
				Modified: r.MatchBreakdown.Modified,
				Fuzzy:    r.MatchBreakdown.Fuzzy,
				Renamed:  r.MatchBreakdown.Renamed,
				Semantic: r.MatchBreakdown.Semantic,
				Moved:    r.MatchBreakdown.Moved,
			},
			ImportDiff: xmlImportDiff{
				CommonCount: r.ImportDiff.CommonCount,
				OnlyACount:  r.ImportDiff.OnlyACount,
				OnlyBCount:  r.ImportDiff.OnlyBCount,
				StdlibA:     r.ImportDiff.StdlibA,
				StdlibB:     r.ImportDiff.StdlibB,
				ExternalA:   r.ImportDiff.ExternalA,
				ExternalB:   r.ImportDiff.ExternalB,
				OnlyA:       r.ImportDiff.OnlyA,
				OnlyB:       r.ImportDiff.OnlyB,
				FrameworksA: r.ImportDiff.FrameworksA,
				FrameworksB: r.ImportDiff.FrameworksB,
			},
			Analysis: convertAnalysis(r.Analysis),
		},
	}

	if r.DiffStats != nil {
		resp.Compare.DiffStats = &xmlDiffStats{
			ModifiedWithDiff: r.DiffStats.ModifiedWithDiff,
			TotalInserts:     r.DiffStats.TotalInserts,
			TotalDeletes:     r.DiffStats.TotalDeletes,
			TotalUpdates:     r.DiffStats.TotalUpdates,
			TotalMoves:       r.DiffStats.TotalMoves,
		}
	}

	if len(r.HotspotsA) > 0 {
		resp.Compare.HotspotsA = convertHotspots(r.HotspotsA)
	}
	if len(r.HotspotsB) > 0 {
		resp.Compare.HotspotsB = convertHotspots(r.HotspotsB)
	}
	if r.RelStatsA != nil {
		resp.Compare.RelStatsA = &xmlRelStats{
			Total: r.RelStatsA.Total, Extends: r.RelStatsA.Extends,
			Implements: r.RelStatsA.Implements, Embeds: r.RelStatsA.Embeds,
			UniqueSubjects: r.RelStatsA.UniqueSubjects,
		}
	}
	if r.RelStatsB != nil {
		resp.Compare.RelStatsB = &xmlRelStats{
			Total: r.RelStatsB.Total, Extends: r.RelStatsB.Extends,
			Implements: r.RelStatsB.Implements, Embeds: r.RelStatsB.Embeds,
			UniqueSubjects: r.RelStatsB.UniqueSubjects,
		}
	}
	if r.QualityA != nil {
		resp.Compare.QualityA = &xmlQualityIndicators{
			TodoCount: r.QualityA.TodoCount, ErrorPatterns: r.QualityA.ErrorPatterns,
			PanicCount: r.QualityA.PanicCount, MagicNumbers: r.QualityA.MagicNumbers,
		}
	}
	if r.QualityB != nil {
		resp.Compare.QualityB = &xmlQualityIndicators{
			TodoCount: r.QualityB.TodoCount, ErrorPatterns: r.QualityB.ErrorPatterns,
			PanicCount: r.QualityB.PanicCount, MagicNumbers: r.QualityB.MagicNumbers,
		}
	}
	if r.FreshnessA != nil {
		resp.Compare.FreshnessA = &xmlFreshness{
			DepRatio: r.FreshnessA.DepFreshnessRatio, VulnRatio: r.FreshnessA.VulnSecurityRatio,
			TotalDeps: r.FreshnessA.TotalDeps, Outdated: r.FreshnessA.OutdatedDeps, Vulnerable: r.FreshnessA.VulnDeps,
		}
	}
	if r.FreshnessB != nil {
		resp.Compare.FreshnessB = &xmlFreshness{
			DepRatio: r.FreshnessB.DepFreshnessRatio, VulnRatio: r.FreshnessB.VulnSecurityRatio,
			TotalDeps: r.FreshnessB.TotalDeps, Outdated: r.FreshnessB.OutdatedDeps, Vulnerable: r.FreshnessB.VulnDeps,
		}
	}
	if r.DataflowA != nil {
		resp.Compare.DataflowA = &xmlCompareDataflow{
			DeadStores: r.DataflowA.DeadStores, UnusedVars: r.DataflowA.UnusedVars,
			TotalFindings: r.DataflowA.TotalFindings, FilesAnalyzed: r.DataflowA.FilesAnalyzed,
		}
	}
	if r.DataflowB != nil {
		resp.Compare.DataflowB = &xmlCompareDataflow{
			DeadStores: r.DataflowB.DeadStores, UnusedVars: r.DataflowB.UnusedVars,
			TotalFindings: r.DataflowB.TotalFindings, FilesAnalyzed: r.DataflowB.FilesAnalyzed,
		}
	}
	if r.APIDiffResult != nil {
		resp.Compare.APIDiff = convertAPIDiff(r.APIDiffResult)
	}
	if r.RouteDiffResult != nil {
		resp.Compare.RouteDiff = convertRouteDiff(r.RouteDiffResult)
	}

	return resp
}

func convertAPIDiff(d *compare.APIDiff) *xmlAPIDiff {
	x := &xmlAPIDiff{
		Common: d.Common, OnlyACount: d.OnlyACount,
		OnlyBCount: d.OnlyBCount, ChangedSig: d.ChangedSig,
	}
	for _, s := range d.OnlyA {
		x.OnlyA = append(x.OnlyA, xmlAPISymbol{Name: s.Name, Kind: s.Kind, Signature: s.Signature, Package: s.Package})
	}
	for _, s := range d.OnlyB {
		x.OnlyB = append(x.OnlyB, xmlAPISymbol{Name: s.Name, Kind: s.Kind, Signature: s.Signature, Package: s.Package})
	}
	for _, c := range d.Changed {
		x.Changed = append(x.Changed, xmlAPIDelta{Name: c.Name, Kind: c.Kind, SigA: c.SigA, SigB: c.SigB})
	}
	return x
}

func convertRouteDiff(d *compare.RouteDiff) *xmlRouteDiff {
	x := &xmlRouteDiff{
		Common: d.Common, OnlyACount: d.OnlyACount, OnlyBCount: d.OnlyBCount,
	}
	for _, r := range d.OnlyA {
		x.OnlyA = append(x.OnlyA, xmlRoute{Method: r.Method, Path: r.Path, Handler: r.Handler})
	}
	for _, r := range d.OnlyB {
		x.OnlyB = append(x.OnlyB, xmlRoute{Method: r.Method, Path: r.Path, Handler: r.Handler})
	}
	return x
}

func convertMetrics(m compare.RepoMetrics) xmlCompMetrics {
	return xmlCompMetrics{
		Files: m.Files, TotalLines: m.TotalLines,
		AvgFuncLines: m.AvgFuncLines, MaxFuncLines: m.MaxFuncLines,
		AvgComplexity: m.AvgComplexity, MaxComplexity: m.MaxComplexity,
		TestRatio: m.TestRatio, DocRatio: m.DocRatio,
		ErrorHandlingRatio: m.ErrorHandlingRatio,
		Interfaces:         m.Interfaces, ExternalDeps: m.ExternalDeps,
		Grade: m.Grade,

		AvgCognitiveComplexity: m.AvgCognitiveComplexity,
		MaxCognitiveComplexity: m.MaxCognitiveComplexity,
		AvgNestingDepth:        m.AvgNestingDepth,
		MaxNestingDepth:        m.MaxNestingDepth,
		LargeFileRatio:         m.LargeFileRatio,
		DuplicationRatio:       m.DuplicationRatio,
		MagicNumberRatio:       m.MagicNumberRatio,
		AvgParamCount:          m.AvgParamCount,
		MaxParamCount:          m.MaxParamCount,
		Score:                  m.Score,
		SemanticDupRatio:       m.SemanticDupRatio,
	}
}

func convertAnalysis(a compare.LLMAnalysis) xmlAnalysis {
	xa := xmlAnalysis{Recommendations: a.Recommendations}
	for _, q := range a.Quality {
		xa.Quality = append(xa.Quality, xmlQuality{
			Aspect: q.Aspect, Winner: q.Winner, Reason: q.Reason,
			SnippetA: q.SnippetA, SnippetB: q.SnippetB,
		})
	}
	for _, g := range a.Gaps {
		xa.Gaps = append(xa.Gaps, xmlGap{
			MissingIn: g.MissingIn, Feature: g.Feature,
			Importance: g.Importance, Location: g.LocationB,
		})
	}
	for _, ai := range a.Architecture {
		xa.Architecture = append(xa.Architecture, xmlArchInsight{
			Insight: ai.Insight, Source: ai.Source,
			Example: ai.Example, Benefit: ai.Benefit,
		})
	}
	return xa
}

func convertHotspots(hh []compare.HotspotFile) *xmlHotspots {
	items := make([]xmlHotspot, len(hh))
	for i, h := range hh {
		items[i] = xmlHotspot{
			File: h.File, Score: h.Score,
			Churn: h.Churn, Complexity: h.Complexity, Risk: h.Risk,
		}
	}
	return &xmlHotspots{Items: items}
}
