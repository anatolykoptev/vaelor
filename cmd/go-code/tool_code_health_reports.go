package main

import (
	"encoding/xml"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/compare"
	"github.com/anatolykoptev/go-code/internal/explore"
	"github.com/anatolykoptev/go-code/internal/freshness"
	"github.com/anatolykoptev/go-code/internal/langutil"
	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/semhealth"
)

// xmlHealthResponse is the top-level XML envelope for code_health output.
type xmlHealthResponse struct {
	XMLName xml.Name  `xml:"response"`
	Health  xmlHealth `xml:"health"`
}

type xmlHealth struct {
	Repo string `xml:"repo,attr"`
	// Partial is set true when the snapshot underlying this report dropped one
	// or more files (clone vanished mid-walk, or a per-file ctx-cancel). When
	// true the metrics under-count the repo and MUST NOT be read as complete —
	// in particular a low TestRatio may be an artifact of the drop, not a real
	// gap. Omitted (absent) for complete reports.
	Partial         bool                `xml:"partial,attr,omitempty"`
	Language        string              `xml:"language,attr,omitempty"`
	Metrics         xmlCompMetrics      `xml:"metrics"`
	Score           float64             `xml:"score,attr"`
	DepFreshness    *xmlDepFreshness    `xml:"depFreshness,omitempty"`
	Vulnerabilities *xmlVulnerabilities `xml:"vulnerabilities,omitempty"`
	Hotspots        *xmlHotspots        `xml:"hotspots,omitempty"`
	RelStats        *xmlRelStats        `xml:"relStats,omitempty"`
	Recommendations *xmlRecommendations `xml:"recommendations,omitempty"`
	OxChecks        *xmlOxCodesChecks   `xml:"oxChecks,omitempty"`
	ArchMetrics     *xmlArchMetrics     `xml:"archMetrics,omitempty"`
}

// xmlOxCodesChecks holds additional quality indicators from ox-codes scoped search.
type xmlOxCodesChecks struct {
	TodoCount       int `xml:"todoCount,attr"`
	UnhandledErrors int `xml:"unhandledErrors,attr"`
	MagicNumbers    int `xml:"magicNumbers,attr"`
}

func convertOxCodesChecks(c *explore.OxCodesHealthChecks) *xmlOxCodesChecks {
	if c == nil {
		return nil
	}
	return &xmlOxCodesChecks{
		TodoCount:       c.TodoCount,
		UnhandledErrors: c.UnhandledErrors,
		MagicNumbers:    c.MagicNumbers,
	}
}

type xmlRecommendations struct {
	Items []xmlRecommendation `xml:"item"`
}

type xmlRecommendation struct {
	Priority  int    `xml:"priority,attr"`
	Potential string `xml:"potential,attr"`
	Area      string `xml:"area,attr"`
	Message   string `xml:",chardata"`
}

// xmlMagicNumbersResponse is the XML envelope for focus=magic_numbers.
type xmlMagicNumbersResponse struct {
	XMLName xml.Name       `xml:"response"`
	Report  xmlMagicReport `xml:"magic_numbers"`
}

type xmlMagicReport struct {
	Repo     string          `xml:"repo,attr"`
	Language string          `xml:"language,attr,omitempty"`
	Total    int             `xml:"total,attr"`
	Items    []xmlMagicEntry `xml:"function"`
}

type xmlMagicEntry struct {
	Name  string `xml:"name,attr"`
	File  string `xml:"file,attr"`
	Line  int    `xml:"line,attr"`
	Count int    `xml:"count,attr"`
}

// xmlSemanticDupResponse is the XML envelope for focus=semantic_duplicates.
type xmlSemanticDupResponse struct {
	XMLName xml.Name          `xml:"response"`
	Report  xmlSemanticReport `xml:"semantic_duplicates"`
}

type xmlSemanticReport struct {
	Repo     string        `xml:"repo,attr"`
	Language string        `xml:"language,attr,omitempty"`
	Total    int           `xml:"total,attr"`
	Groups   []xmlDupGroup `xml:"group"`
}

type xmlDupGroup struct {
	Similarity string         `xml:"similarity,attr"`
	Tier       string         `xml:"tier,attr,omitempty"`
	Symbols    []xmlDupSymbol `xml:"function"`
}

type xmlDupSymbol struct {
	Name string `xml:"name,attr"`
	File string `xml:"file,attr"`
	Line int    `xml:"line,attr"`
	Kind string `xml:"kind,attr,omitempty"`
}

func buildMagicNumbersXML(name, language string, entries []compare.MagicNumberEntry) xmlMagicNumbersResponse {
	items := make([]xmlMagicEntry, len(entries))
	for i, e := range entries {
		items[i] = xmlMagicEntry{Name: e.Name, File: e.File, Line: e.Line, Count: e.Count}
	}
	return xmlMagicNumbersResponse{
		Report: xmlMagicReport{
			Repo: name, Language: language,
			Total: len(entries), Items: items,
		},
	}
}

func buildSemanticDupXML(name, language string, groups []semhealth.DupGroup) xmlSemanticDupResponse {
	xmlGroups := make([]xmlDupGroup, len(groups))
	for i, g := range groups {
		syms := make([]xmlDupSymbol, len(g.Symbols))
		for j, s := range g.Symbols {
			syms[j] = xmlDupSymbol{Name: s.Name, File: s.File, Line: s.Line, Kind: s.Kind}
		}
		xmlGroups[i] = xmlDupGroup{
			Similarity: fmt.Sprintf("%.2f", g.AvgSimilarity),
			Tier:       g.Tier,
			Symbols:    syms,
		}
	}
	return xmlSemanticDupResponse{
		Report: xmlSemanticReport{
			Repo: name, Language: language,
			Total: len(groups), Groups: xmlGroups,
		},
	}
}

func convertRecommendations(recs []compare.Recommendation) *xmlRecommendations {
	items := make([]xmlRecommendation, len(recs))
	for i, r := range recs {
		items[i] = xmlRecommendation{
			Priority:  r.Priority,
			Potential: fmt.Sprintf("+%d", r.Potential),
			Area:      r.Area,
			Message:   r.Message,
		}
	}
	return &xmlRecommendations{Items: items}
}

// countFuncs counts function and method symbols (excluding test files).
func countFuncs(symbols []*parser.Symbol) int {
	count := 0
	for _, sym := range symbols {
		if (sym.Kind == parser.KindFunction || sym.Kind == parser.KindMethod) && !isTestFilePath(sym.File) {
			count++
		}
	}
	return count
}

// isTestFilePath checks if a file path looks like a test file.
func isTestFilePath(path string) bool {
	return langutil.IsTestFile(path)
}

// xmlDepFreshness represents dependency freshness in XML output.
type xmlDepFreshness struct {
	Total         int              `xml:"total,attr"`
	Current       int              `xml:"current,attr"`
	Ratio         string           `xml:"ratio,attr"`
	RuntimeStatus string           `xml:"runtimeStatus,attr,omitempty"`
	Outdated      []xmlOutdatedDep `xml:"dep,omitempty"`
}

// xmlOutdatedDep represents a single outdated dependency in XML output.
type xmlOutdatedDep struct {
	Name    string `xml:"name,attr"`
	Current string `xml:"current,attr"`
	Latest  string `xml:"latest,attr"`
	Kind    string `xml:"kind,attr"`
}

type xmlVulnerabilities struct {
	Total      int          `xml:"total,attr"`
	Vulnerable int          `xml:"vulnerable,attr"`
	Critical   int          `xml:"critical,attr,omitempty"`
	High       int          `xml:"high,attr,omitempty"`
	Medium     int          `xml:"medium,attr,omitempty"`
	Low        int          `xml:"low,attr,omitempty"`
	Ratio      string       `xml:"ratio,attr"`
	Vulns      []xmlVulnDep `xml:"vuln,omitempty"`
}

type xmlVulnDep struct {
	Name     string `xml:"name,attr"`
	Version  string `xml:"version,attr"`
	ID       string `xml:"id,attr"`
	Severity string `xml:"severity,attr"`
	Summary  string `xml:"summary,attr"`
}

func convertVulnerabilities(vr *freshness.VulnResult) *xmlVulnerabilities {
	xv := &xmlVulnerabilities{
		Total:      vr.Total,
		Vulnerable: vr.Vulnerable,
		Critical:   vr.Critical,
		High:       vr.High,
		Medium:     vr.Medium,
		Low:        vr.Low,
		Ratio:      fmt.Sprintf("%.2f", vr.Ratio),
	}
	for _, v := range vr.Vulns {
		xv.Vulns = append(xv.Vulns, xmlVulnDep{
			Name:     v.Name,
			Version:  v.Version,
			ID:       v.ID,
			Severity: v.Severity,
			Summary:  v.Summary,
		})
	}
	return xv
}

func convertDepFreshness(fr *freshness.FreshnessResult) *xmlDepFreshness {
	xf := &xmlDepFreshness{
		Total:         fr.Total,
		Current:       fr.UpToDate,
		Ratio:         fmt.Sprintf("%.2f", fr.Ratio),
		RuntimeStatus: fr.RuntimeStatus,
	}
	for _, od := range fr.Outdated {
		xf.Outdated = append(xf.Outdated, xmlOutdatedDep{
			Name:    od.Name,
			Current: od.Current,
			Latest:  od.Latest,
			Kind:    od.Kind,
		})
	}
	return xf
}
