package scip_test

import (
	"testing"

	gocodescip "github.com/anatolykoptev/go-code/internal/scip"
)

func TestDetectIndexer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		lang     string
		wantOK   bool
		wantName string
		wantArgs []string
	}{
		// "go" is intentionally absent from the registry; Go uses go/types instead.
		{"go", false, "", nil},
		{"typescript", true, "scip-typescript", []string{"index"}},
		{"javascript", true, "scip-typescript", []string{"index", "--infer-tsconfig"}},
		{"python", true, "scip-python", []string{"index", "."}},
		{"java", true, "scip-java", []string{"index"}},
		{"rust", true, "rust-analyzer", []string{"scip", "."}},
		{"ruby", true, "scip-ruby", nil},     // deferred: no linux/aarch64 prebuilt
		{"csharp", true, "scip-dotnet", nil}, // deferred: Docker-image-only distribution
		{"c", true, "scip-clang", nil},       // deferred: no linux/aarch64 prebuilt
		{"cpp", true, "scip-clang", nil},     // deferred: no linux/aarch64 prebuilt
		{"php", false, "", nil},
		{"unknown", false, "", nil},
		{"", false, "", nil},
	}

	for _, tt := range tests {
		t.Run(tt.lang, func(t *testing.T) {
			cfg, ok := gocodescip.DetectIndexer(tt.lang)
			if ok != tt.wantOK {
				t.Fatalf("DetectIndexer(%q) ok=%v, want %v", tt.lang, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if cfg.Name != tt.wantName {
				t.Errorf("Name=%q, want %q", cfg.Name, tt.wantName)
			}
			if len(cfg.Args) != len(tt.wantArgs) {
				t.Errorf("Args=%v, want %v", cfg.Args, tt.wantArgs)
				return
			}
			for i, arg := range cfg.Args {
				if arg != tt.wantArgs[i] {
					t.Errorf("Args[%d]=%q, want %q", i, arg, tt.wantArgs[i])
				}
			}
		})
	}
}

func TestIndexerAvailable(t *testing.T) {
	t.Parallel()
	// A binary that definitely doesn't exist.
	if gocodescip.IndexerAvailable("nonexistent-scip-indexer-xyz") {
		t.Error("IndexerAvailable should return false for nonexistent binary")
	}
}
