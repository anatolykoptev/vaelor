package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/designmd"
)

// loadDesignMeta discovers design directories under baseDir and loads all index.json metadata.
// baseDir is e.g. /host/tools/awesome-design-md — contains design-md/ and design-md-styles/.
func loadDesignMeta(baseDir string) (map[string]designmd.BrandMeta, []string) {
	meta := make(map[string]designmd.BrandMeta)
	if baseDir == "" {
		return meta, nil
	}

	// Discover subdirectories that contain */DESIGN.md files.
	var dirs []string
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return meta, nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(baseDir, e.Name())
		// Check if this dir has brand subdirs with DESIGN.md.
		matches, _ := filepath.Glob(filepath.Join(sub, "*", "DESIGN.md"))
		if len(matches) > 0 {
			dirs = append(dirs, sub)
			loadMetaFile(filepath.Join(sub, "index.json"), meta)
		}
	}

	// Fallback to persistent volume.
	loadMetaFile("/tmp/go-code-output/design-md-index.json", meta)

	return meta, dirs
}

func loadMetaFile(path string, into map[string]designmd.BrandMeta) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var m map[string]designmd.BrandMeta
	if json.Unmarshal(data, &m) == nil {
		for k, v := range m {
			if _, exists := into[k]; !exists {
				into[k] = v
			}
		}
	}
}

// findDesignFile locates the DESIGN.md file for a brand across directories.
func findDesignFile(brand string, dirs []string) string {
	for _, dir := range dirs {
		path := filepath.Join(dir, brand, "DESIGN.md")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

type brandHit struct {
	brand    string
	section  string
	distance float32
	excerpt  string
	filePath string
}

// groupByBrand deduplicates search results by brand, reads excerpts from files.
func groupByBrand(results []designmd.SearchResult, dirs []string, topK int) []brandHit {
	seen := make(map[string]bool)
	var hits []brandHit
	for _, r := range results {
		if seen[r.Brand] {
			continue
		}
		seen[r.Brand] = true

		excerpt := r.Section
		filePath := findDesignFile(r.Brand, dirs)
		if filePath != "" {
			excerpt = readExcerpt(filePath, r.Section)
		}

		hits = append(hits, brandHit{
			brand: r.Brand, section: r.Section,
			distance: r.Distance, excerpt: excerpt, filePath: filePath,
		})
		if len(hits) >= topK {
			break
		}
	}
	return hits
}

func readExcerpt(filePath, section string) string {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return section
	}
	for _, s := range designmd.SplitSections(string(content)) {
		if s.Title != section {
			continue
		}
		parts := strings.SplitN(s.Body, "\n", 2)
		if len(parts) < 2 {
			return section
		}
		excerpt := strings.TrimSpace(parts[1])
		if len(excerpt) > 200 {
			if cut := strings.LastIndex(excerpt[:200], " "); cut > 100 {
				return excerpt[:cut] + "..."
			}
			return excerpt[:200] + "..."
		}
		return excerpt
	}
	return section
}

// ---- design_search XML types ----
//
// Migrated from a hand-rolled fmt.Fprintf formatter onto encoding/xml.Marshal so
// well-formedness is correct by construction. Field order fixes element order;
// omitempty on the optional children reproduces the prior conditional emission.
// The score attribute is pre-formatted to a string (%.2f) because raw
// xml.Marshal of a float renders via strconv and drops trailing zeros
// (0.90 -> "0.9"), breaking attribute equivalence.

type designSearchRespXML struct {
	XMLName xml.Name         `xml:"response"`
	Tool    string           `xml:"tool,attr"`
	Query   string           `xml:"query"`
	Results designResultsXML `xml:"results"`
}

type designResultsXML struct {
	Count int               `xml:"count,attr"`
	Items []designResultXML `xml:"result"`
}

type designResultXML struct {
	Rank    int    `xml:"rank,attr"`
	Score   string `xml:"score,attr"`
	Brand   string `xml:"brand,attr"`
	File    string `xml:"file,omitempty"`
	Vibe    string `xml:"vibe,omitempty"`
	Colors  string `xml:"colors,omitempty"`
	BestFor string `xml:"best_for,omitempty"`
	Section string `xml:"matched_section"`
	Excerpt string `xml:"excerpt"`
}

// formatDesignResults builds the design_search XML response.
func formatDesignResults(query string, hits []brandHit, meta map[string]designmd.BrandMeta, mappings []analyze.PathMapping) string {
	resp := designSearchRespXML{
		Tool:  "design_search",
		Query: query,
		Results: designResultsXML{
			Count: len(hits),
			Items: make([]designResultXML, 0, len(hits)),
		},
	}

	for i, h := range hits {
		item := designResultXML{
			Rank:    i + 1,
			Score:   fmt.Sprintf("%.2f", 1.0-float64(h.distance)),
			Brand:   h.brand,
			Section: h.section,
			Excerpt: h.excerpt,
		}
		if h.filePath != "" {
			item.File = reversePathMapping(h.filePath, mappings)
		}
		if m, ok := meta[h.brand]; ok {
			item.Vibe = m.Vibe
			if len(m.Colors) > 0 {
				item.Colors = strings.Join(m.Colors, ", ")
			}
			item.BestFor = m.BestFor
		}
		resp.Results.Items = append(resp.Results.Items, item)
	}

	return xmlMarshalFragment(resp)
}

// designStatusXML is the design_search status response (e.g. not_indexed),
// migrated from an inline hand-rolled <response><status><message></response>
// string so the last manual-XML-string-concatenation site in design_search is
// gone. The message is a fixed constant with no XML-hostile characters, so the
// emitted bytes are unchanged.
type designStatusXML struct {
	XMLName xml.Name `xml:"response"`
	Tool    string   `xml:"tool,attr"`
	Status  string   `xml:"status"`
	Message string   `xml:"message"`
}

// formatDesignStatus renders a design_search <status>/<message> response.
func formatDesignStatus(status, message string) string {
	return xmlMarshalFragment(designStatusXML{Tool: "design_search", Status: status, Message: message})
}
