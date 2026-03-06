package webanalyze

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SourceMap represents a parsed source map JSON.
type SourceMap struct {
	Version        int      `json:"version"`
	Sources        []string `json:"sources"`
	SourcesContent []string `json:"sourcesContent"`
}

// SourceStats holds counts of extracted source files.
type SourceStats struct {
	Files     int
	Languages map[string]int // extension → count
}

// ParseSourceMap parses source map JSON bytes.
func ParseSourceMap(data []byte) (*SourceMap, error) {
	var sm SourceMap
	if err := json.Unmarshal(data, &sm); err != nil {
		return nil, fmt.Errorf("parse sourcemap: %w", err)
	}
	return &sm, nil
}

// WriteSourceTree writes source map contents to disk preserving directory structure.
func WriteSourceTree(dir string, sm *SourceMap) (*SourceStats, error) {
	stats := &SourceStats{Languages: make(map[string]int)}
	for i, src := range sm.Sources {
		if i >= len(sm.SourcesContent) {
			break
		}
		content := sm.SourcesContent[i]
		if content == "" {
			continue
		}
		// Sanitize path: remove webpack:/// prefix, ../ traversal.
		clean := sanitizePath(src)
		if clean == "" {
			continue
		}
		fullPath := filepath.Join(dir, clean)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o750); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", filepath.Dir(fullPath), err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o640); err != nil {
			return nil, fmt.Errorf("write %s: %w", fullPath, err)
		}
		stats.Files++
		ext := strings.TrimPrefix(filepath.Ext(clean), ".")
		if ext != "" {
			stats.Languages[ext]++
		}
	}
	return stats, nil
}

// sanitizePath cleans webpack-style source paths.
func sanitizePath(p string) string {
	// Strip common prefixes.
	p = strings.TrimPrefix(p, "webpack:///")
	p = strings.TrimPrefix(p, "webpack://")
	p = strings.TrimPrefix(p, "./")
	// Block path traversal.
	if strings.Contains(p, "..") {
		return ""
	}
	// Skip node_modules.
	if strings.Contains(p, "node_modules/") {
		return ""
	}
	return p
}

// FindSourceMapURL extracts the sourceMappingURL from JS content.
// Returns empty string if not found or if it's a data: URI.
func FindSourceMapURL(body string) string {
	for _, prefix := range []string{"//# sourceMappingURL=", "//@ sourceMappingURL="} {
		idx := strings.LastIndex(body, prefix)
		if idx < 0 {
			continue
		}
		url := strings.TrimSpace(body[idx+len(prefix):])
		if nl := strings.IndexByte(url, '\n'); nl >= 0 {
			url = url[:nl]
		}
		// Skip data: URIs (too large, embedded).
		if strings.HasPrefix(url, "data:") {
			return ""
		}
		return url
	}
	return ""
}
