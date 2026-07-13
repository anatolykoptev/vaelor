package semhealth

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/go-code/internal/embeddings"
)

// writeFile creates root/relPath with the given content, making parent dirs.
func writeFile(t *testing.T, root, relPath, content string) {
	t.Helper()
	full := filepath.Join(root, relPath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
}

func TestFilterBuildTagVariants_DropsDisjointPlatformSplit(t *testing.T) {
	root := t.TempDir()
	// The atomicDirectorySwap false-positive: linux vs !linux — never compile
	// together, so the pair is the platform-split idiom, not a duplicate.
	writeFile(t, root, "internal/ingest/clone_swap_linux.go",
		"//go:build linux\n\npackage ingest\n\nfunc atomicDirectorySwap(a, b string) error { return nil }\n")
	writeFile(t, root, "internal/ingest/clone_swap_other.go",
		"//go:build !linux\n\npackage ingest\n\nfunc atomicDirectorySwap(a, b string) error { return nil }\n")

	pairs := []embeddings.SimilarPair{{
		FileA: "internal/ingest/clone_swap_linux.go", SymbolA: "atomicDirectorySwap", KindA: "function",
		FileB: "internal/ingest/clone_swap_other.go", SymbolB: "atomicDirectorySwap", KindB: "function",
		Similarity: 0.93,
	}}
	kept, dropped := filterBuildTagVariants(root, pairs)
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1 (linux vs !linux is disjoint)", dropped)
	}
	if len(kept) != 0 {
		t.Errorf("len(kept) = %d, want 0", len(kept))
	}
}

func TestFilterBuildTagVariants_DropsDisjointGoosSibling(t *testing.T) {
	root := t.TempDir()
	// Sibling files named by GOOS: a_windows.go and a_darwin.go. These never
	// compile together because GOOS tags are mutually exclusive.
	writeFile(t, root, "a_windows.go", "//go:build windows\n\npackage p\nfunc f() {}\n")
	writeFile(t, root, "a_darwin.go", "//go:build darwin\n\npackage p\nfunc f() {}\n")

	pairs := []embeddings.SimilarPair{{
		FileA: "a_windows.go", SymbolA: "f", KindA: "function",
		FileB: "a_darwin.go", SymbolB: "f", KindB: "function",
	}}
	_, dropped := filterBuildTagVariants(root, pairs)
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1 (windows vs darwin are mutually exclusive GOOS tags)", dropped)
	}
}

func TestFilterBuildTagVariants_DropsExplicitMutex(t *testing.T) {
	root := t.TempDir()
	// Explicit mutual exclusion that IS provable by tag enumeration.
	writeFile(t, root, "a.go", "//go:build linux\n\npackage p\nfunc f() {}\n")
	writeFile(t, root, "b.go", "//go:build !linux && !windows\n\npackage p\nfunc f() {}\n")

	pairs := []embeddings.SimilarPair{{
		FileA: "a.go", SymbolA: "f", KindA: "function",
		FileB: "b.go", SymbolB: "f", KindB: "function",
	}}
	_, dropped := filterBuildTagVariants(root, pairs)
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1 (linux vs !linux&&!windows is disjoint)", dropped)
	}
}

func TestFilterBuildTagVariants_KeepsSamePackageNoTags(t *testing.T) {
	root := t.TempDir()
	// Two ordinary files in the same package, no build constraints — a genuine
	// duplicate candidate that must NOT be dropped.
	writeFile(t, root, "x.go", "package p\nfunc Parse() {}\n")
	writeFile(t, root, "y.go", "package p\nfunc ParseV2() {}\n")

	pairs := []embeddings.SimilarPair{{
		FileA: "x.go", SymbolA: "Parse", KindA: "function",
		FileB: "y.go", SymbolB: "ParseV2", KindB: "function",
	}}
	kept, dropped := filterBuildTagVariants(root, pairs)
	if dropped != 0 {
		t.Errorf("dropped = %d, want 0 (no constraints → keep)", dropped)
	}
	if len(kept) != 1 {
		t.Errorf("len(kept) = %d, want 1", len(kept))
	}
}

func TestFilterBuildTagVariants_KeepsOneConstrainedOneNot(t *testing.T) {
	root := t.TempDir()
	// One file constrained to linux, the other unconstrained (compiles
	// everywhere). They DO compile together on linux → not disjoint → keep.
	writeFile(t, root, "a.go", "//go:build linux\n\npackage p\nfunc f() {}\n")
	writeFile(t, root, "b.go", "package p\nfunc f() {}\n")

	pairs := []embeddings.SimilarPair{{
		FileA: "a.go", SymbolA: "f", KindA: "function",
		FileB: "b.go", SymbolB: "f", KindB: "function",
	}}
	_, dropped := filterBuildTagVariants(root, pairs)
	if dropped != 0 {
		t.Errorf("dropped = %d, want 0 (one unconstrained file compiles with the other)", dropped)
	}
}

func TestFilterBuildTagVariants_EmptyRootNoOp(t *testing.T) {
	pairs := []embeddings.SimilarPair{{
		FileA: "a.go", SymbolA: "f", FileB: "b.go", SymbolB: "f",
	}}
	kept, dropped := filterBuildTagVariants("", pairs)
	if dropped != 0 || len(kept) != 1 {
		t.Errorf("empty root must be a no-op: kept=%d dropped=%d", len(kept), dropped)
	}
}

func TestFilterBuildTagVariants_NonGoFilesKept(t *testing.T) {
	root := t.TempDir()
	pairs := []embeddings.SimilarPair{{
		FileA: "a.ts", SymbolA: "f", FileB: "b.ts", SymbolB: "f",
	}}
	_, dropped := filterBuildTagVariants(root, pairs)
	if dropped != 0 {
		t.Errorf("non-go pair must be kept: dropped=%d", dropped)
	}
}

func TestFilterBuildTagVariants_PlusBuildSyntax(t *testing.T) {
	root := t.TempDir()
	// Legacy // +build syntax must also be recognized.
	writeFile(t, root, "a.go", "// +build linux\n\npackage p\nfunc f() {}\n")
	writeFile(t, root, "b.go", "// +build !linux\n\npackage p\nfunc f() {}\n")

	pairs := []embeddings.SimilarPair{{
		FileA: "a.go", SymbolA: "f", KindA: "function",
		FileB: "b.go", SymbolB: "f", KindB: "function",
	}}
	_, dropped := filterBuildTagVariants(root, pairs)
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1 (legacy +build linux vs !linux is disjoint)", dropped)
	}
}
