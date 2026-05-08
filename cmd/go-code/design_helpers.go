package main

import (
	"encoding/json"
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

// formatDesignResults builds the XML response.
func formatDesignResults(query string, hits []brandHit, meta map[string]designmd.BrandMeta, mappings []analyze.PathMapping) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<response tool=\"design_search\">\n")
	fmt.Fprintf(&sb, "  <query>%s</query>\n", escapeXML(query))
	fmt.Fprintf(&sb, "  <results count=\"%d\">\n", len(hits))

	for i, h := range hits {
		score := 1.0 - float64(h.distance)
		fmt.Fprintf(&sb, "    <result rank=\"%d\" score=\"%.2f\" brand=\"%s\">\n", i+1, score, escapeXML(h.brand))

		if h.filePath != "" {
			fmt.Fprintf(&sb, "      <file>%s</file>\n", escapeXML(reversePathMapping(h.filePath, mappings)))
		}

		if m, ok := meta[h.brand]; ok {
			if m.Vibe != "" {
				fmt.Fprintf(&sb, "      <vibe>%s</vibe>\n", escapeXML(m.Vibe))
			}
			if len(m.Colors) > 0 {
				fmt.Fprintf(&sb, "      <colors>%s</colors>\n", escapeXML(strings.Join(m.Colors, ", ")))
			}
			if m.BestFor != "" {
				fmt.Fprintf(&sb, "      <best_for>%s</best_for>\n", escapeXML(m.BestFor))
			}
		}

		fmt.Fprintf(&sb, "      <matched_section>%s</matched_section>\n", escapeXML(h.section))
		fmt.Fprintf(&sb, "      <excerpt>%s</excerpt>\n", escapeXML(h.excerpt))
		fmt.Fprintf(&sb, "    </result>\n")
	}

	sb.WriteString("  </results>\n</response>")
	return sb.String()
}

