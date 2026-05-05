package scip

import "os/exec"

// IndexerConfig describes how to invoke a SCIP indexer for a language.
type IndexerConfig struct {
	Name string   // binary name (e.g. "scip-typescript")
	Args []string // default args to pass after the binary name
}

// indexerRegistry maps language names to their SCIP indexer configurations.
//
// Notes on what is and isn't shipped in the runtime image (Dockerfile is the source of truth):
//   - "go" is intentionally absent: Go uses go/types (internal/goanalysis) instead of scip-go.
//   - svelte/astro are absent: astro has no SCIP indexer, svelte's language tools emit LSIF.
//   - ruby/csharp/c/cpp keep entries here so language detection still resolves a known
//     indexer name, but the binaries are NOT installed in the runtime image (P3.F5 audit,
//     2026-05-05): scip-ruby and scip-clang ship no linux/aarch64 release binary, and
//     scip-dotnet has no release-asset binary at all (Docker-image-only). At runtime,
//     IndexerAvailable() returns false for these and callers fall back to the basic tier.
var indexerRegistry = map[string]IndexerConfig{
	"typescript": {Name: "scip-typescript", Args: []string{"index"}},
	"javascript": {Name: "scip-typescript", Args: []string{"index", "--infer-tsconfig"}},
	"python":     {Name: "scip-python", Args: []string{"index", "."}},
	"java":       {Name: "scip-java", Args: []string{"index"}},
	"rust":       {Name: "rust-analyzer", Args: []string{"scip", "."}},
	"ruby":       {Name: "scip-ruby"},
	"csharp":     {Name: "scip-dotnet"},
	"c":          {Name: "scip-clang"},
	"cpp":        {Name: "scip-clang"},
}

// DetectIndexer returns the IndexerConfig for the given language.
// Returns (IndexerConfig{}, false) for unsupported languages.
func DetectIndexer(lang string) (IndexerConfig, bool) {
	cfg, ok := indexerRegistry[lang]
	return cfg, ok
}

// IndexerAvailable reports whether the indexer binary for the given binary name
// is present in PATH. Callers typically pass cfg.Name from DetectIndexer.
func IndexerAvailable(binaryName string) bool {
	_, err := exec.LookPath(binaryName)
	return err == nil
}
