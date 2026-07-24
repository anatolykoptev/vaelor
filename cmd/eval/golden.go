// Package main — eval harness for go-code retrieval quality.
//
// This file: load golden dataset records from JSONL files.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// GoldenRecord is one labeled query for the eval harness.
//
// Fields:
//   - Query: free-form natural-language or identifier query
//   - ExpectedTop3: 3 symbols the labeler considers the relevant top results.
//     Stored in the form "<file>:<symbol>" or just "<symbol>" — matched leniently
//     by the metric (containment + suffix match).
//   - Repo: optional override of the repo arg to semantic_search; when empty
//     the file's basename (without .jsonl) is used as the repo identifier the
//     harness will hand to semantic_search.
//   - Language: optional language filter passed to semantic_search (e.g. "go",
//     "python", "typescript"). When empty, no language filter is sent and the
//     record aggregates under the "unspecified" bucket in per-language reports.
//     Backward-compatible: old records without this field parse and run exactly
//     as before (no filter sent, identical search results).
//   - Notes: optional free-form for the labeler.
type GoldenRecord struct {
	Query        string   `json:"query"`
	ExpectedTop3 []string `json:"expected_top_3"`
	Repo         string   `json:"repo,omitempty"`
	Language     string   `json:"language,omitempty"`
	Notes        string   `json:"notes,omitempty"`
}

// GoldenSet groups golden records by repo (file basename).
type GoldenSet struct {
	// PerRepo maps repo identifier -> records loaded from <repo>.jsonl
	PerRepo map[string][]GoldenRecord
}

// LoadGolden reads all *.jsonl files from dir, parses one record per line.
//
// Empty / comment lines (lines beginning with '#') are skipped. Returns the
// records grouped by file basename without the .jsonl suffix.
func LoadGolden(dir string) (*GoldenSet, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read golden dir: %w", err)
	}

	set := &GoldenSet{PerRepo: make(map[string][]GoldenRecord)}
	var jsonlFiles []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		jsonlFiles = append(jsonlFiles, e.Name())
	}
	// Deterministic order so per-query lists are reproducible across runs.
	sort.Strings(jsonlFiles)

	for _, name := range jsonlFiles {
		repoKey := strings.TrimSuffix(name, ".jsonl")
		records, err := loadJSONL(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", name, err)
		}
		// Inject per-record repo if absent.
		for i := range records {
			if records[i].Repo == "" {
				records[i].Repo = repoKey
			}
		}
		set.PerRepo[repoKey] = records
	}

	if len(set.PerRepo) == 0 {
		return nil, fmt.Errorf("no .jsonl files found in %s", dir)
	}
	return set, nil
}

// loadJSONL parses a single JSONL file. Validates each record minimally.
func loadJSONL(path string) ([]GoldenRecord, error) {
	f, err := os.Open(path) // #nosec G304 -- path is a CLI flag, operator-controlled
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var out []GoldenRecord
	scanner := bufio.NewScanner(f)
	// Allow long single-line records (golden notes can grow).
	const (
		bufStart = 64 * 1024
		bufMax   = 1024 * 1024
	)
	scanner.Buffer(make([]byte, 0, bufStart), bufMax)

	lineno := 0
	for scanner.Scan() {
		lineno++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var rec GoldenRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineno, err)
		}
		if rec.Query == "" {
			return nil, fmt.Errorf("line %d: empty query", lineno)
		}
		if len(rec.ExpectedTop3) == 0 {
			return nil, fmt.Errorf("line %d: empty expected_top_3", lineno)
		}
		out = append(out, rec)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// FlatQueries returns all records flattened into a single slice. Order is
// stable across runs: repos sorted alphabetically, records preserve file order.
func (g *GoldenSet) FlatQueries() []GoldenRecord {
	repos := make([]string, 0, len(g.PerRepo))
	for k := range g.PerRepo {
		repos = append(repos, k)
	}
	sort.Strings(repos)

	var out []GoldenRecord
	for _, r := range repos {
		out = append(out, g.PerRepo[r]...)
	}
	return out
}

// ApplyRepoMap overrides each record's Repo field with the mapped path for its
// repo_key (the .jsonl file basename). Records whose repo_key is not in the
// map keep their existing Repo field (the record's own path or the injected
// file basename). This lets the golden JSONL stay portable — placeholder paths
// like "/path/to/repo" are resolved to real absolute paths or forge slugs at
// run time without committing operator-specific paths.
func (g *GoldenSet) ApplyRepoMap(repoMap map[string]string) {
	for repoKey, records := range g.PerRepo {
		mapped, ok := repoMap[repoKey]
		if !ok || mapped == "" {
			continue
		}
		for i := range records {
			g.PerRepo[repoKey][i].Repo = mapped
		}
	}
}

// ParseRepoMap parses a comma-separated "key=path,key=path" string into a
// map. Whitespace around keys and values is trimmed. Empty input returns nil.
// Keys or values containing '=' are rejected (paths with '=' are vanishingly
// rare and would make the format ambiguous — use a JSON file if needed).
func ParseRepoMap(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	out := make(map[string]string)
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		key, val, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("repo-map entry %q: missing '=' (expected key=path)", pair)
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key == "" || val == "" {
			return nil, fmt.Errorf("repo-map entry %q: empty key or path", pair)
		}
		out[key] = val
	}
	return out, nil
}
