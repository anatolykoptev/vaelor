package scip

import "os/exec"

// IndexerConfig describes how to invoke a SCIP indexer for a language.
type IndexerConfig struct {
	Name string   // binary name (e.g. "scip-typescript")
	Args []string // default args to pass after the binary name
}

// indexerRegistry maps language names to their SCIP indexer configurations.
var indexerRegistry = map[string]IndexerConfig{
	"go":         {Name: "scip-go"},
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
