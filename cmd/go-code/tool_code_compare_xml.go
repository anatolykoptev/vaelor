package main

import "encoding/xml"

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
	CouplingA      *xmlCoupling          `xml:"couplingA,omitempty"`
	CouplingB      *xmlCoupling          `xml:"couplingB,omitempty"`
	ArchMetricsA   *xmlArchMetrics       `xml:"archMetricsA,omitempty"`
	ArchMetricsB   *xmlArchMetrics       `xml:"archMetricsB,omitempty"`
	CrossLang      *xmlCrossLang         `xml:"crossLang,omitempty"`
}

type xmlCoupling struct {
	Pairs []xmlCoupledPair `xml:"pair"`
}

type xmlCoupledPair struct {
	FileA     string  `xml:"fileA,attr"`
	FileB     string  `xml:"fileB,attr"`
	CoChanges int     `xml:"coChanges,attr"`
	Ratio     float64 `xml:"ratio,attr"`
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

type xmlVerdict struct {
	CanReplace string   `xml:"canReplace,attr"`
	Reason     string   `xml:"reason,attr"`
	Blockers   []string `xml:"blockers>blocker,omitempty"`
}

type xmlAnalysis struct {
	Quality         []xmlQuality     `xml:"quality,omitempty"`
	Gaps            []xmlGap         `xml:"gap,omitempty"`
	Architecture    []xmlArchInsight `xml:"architecture,omitempty"`
	Recommendations []string         `xml:"recommendation,omitempty"`
	Verdict         *xmlVerdict      `xml:"verdict,omitempty"`
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

type xmlArchMetrics struct {
	PackageCount      int          `xml:"packages,attr"`
	CommunityCount    int          `xml:"communityCount,attr,omitempty"`
	CrossPkgCallRatio float64      `xml:"crossPkgCalls,attr"`
	MaxCallDepth      int          `xml:"maxCallDepth,attr"`
	InterfaceRatio    float64      `xml:"interfaceRatio,attr"`
	NotIndexed        bool         `xml:"notIndexed,attr,omitempty"`
	Hint              string       `xml:"hint,attr,omitempty"`
	GodPackages       []xmlGodPkg  `xml:"godPkg,omitempty"`
	CircularDeps      []xmlCircDep `xml:"circularDep,omitempty"`
}

type xmlGodPkg struct {
	Name      string `xml:"name,attr"`
	Importers int    `xml:"importers,attr"`
}

type xmlCircDep struct {
	A string `xml:"a,attr"`
	B string `xml:"b,attr"`
}

type xmlCrossLang struct {
	LangA   string          `xml:"langA,attr"`
	LangB   string          `xml:"langB,attr"`
	Matches int             `xml:"matches,attr"`
	Top     []xmlCrossMatch `xml:"match,omitempty"`
}

type xmlCrossMatch struct {
	NameA string  `xml:"nameA,attr"`
	NameB string  `xml:"nameB,attr"`
	Sim   float64 `xml:"sim,attr"`
}
