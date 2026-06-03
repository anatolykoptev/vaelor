package codegraph

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// writeTinyForgeModule writes a self-contained Go module into dir with a local
// Forge interface and two concrete implementers in separate files, mirroring the
// real shape the find_duplicates 2-hop filter (PR #221) walks over IMPLEMENTS.
// Two files (one type each) deliberately exercise per-file FileSet resolution.
func writeTinyForgeModule(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"go.mod": "module example.com/forge\n\ngo 1.21\n",
		"forge.go": `package forge

// Forge is the interface both concrete forges satisfy structurally.
type Forge interface {
	FetchREADME() string
}
`,
		"github.go": `package forge

type GitHubForge struct{}

func (GitHubForge) FetchREADME() string { return "github" }
`,
		"gitlab.go": `package forge

type GitLabForge struct{}

func (GitLabForge) FetchREADME() string { return "gitlab" }
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
}

// symbolKeySet reconstructs, from buildSymbolGraph's vertices, the set of
// composite Symbol vertex keys (name + compositeKeyDelim + relFile) exactly as
// the persisted CONTAINS-edge endpoints carry them. These are the ground-truth
// node keys an ingested symbol gets; an IMPLEMENTS edge endpoint MUST match one
// byte-for-byte or the edge is orphaned at persist time.
func symbolKeySet(t *testing.T, root string) (map[string]struct{}, []*parser.Symbol) {
	t.Helper()
	_, syms, _, _, _, _, _, err := ingestAndParse(context.Background(), root)
	if err != nil {
		t.Fatalf("ingestAndParse: %v", err)
	}
	if len(syms) == 0 {
		t.Fatal("ingestAndParse produced no symbols")
	}
	verts, _ := buildSymbolGraph(root, syms, nil)
	keys := make(map[string]struct{}, len(verts))
	for _, v := range verts {
		if v.Label != "Symbol" {
			continue
		}
		keys[v.Props["name"]+compositeKeyDelim+v.Props["file"]] = struct{}{}
	}
	return keys, syms
}

// TestImplementsEdgeKeysMatchSymbolGraph is the make-or-break contract PR #221's
// 2-hop interface-sibling filter depends on: the IMPLEMENTS edge subject endpoint
// (keyed from a go/types FileSet ABSOLUTE path via relPath) MUST be byte-identical
// to the Symbol vertex key the tree-sitter ingest path produces for the SAME type.
//
// This is NOT asserted-by-construction (cf. TestBuildGraphImplementsEdges, which
// feeds synthetic rels whose File literal equals the synthetic symbol File). Here
// two INDEPENDENT absolute-path producers must agree after relPath(abs, root):
//   - go/types FileSet  → Satisfaction.TypeFile → TypeRelationship.File (the edge subject)
//   - ingest WalkDir     → ingest.File.Path → parser.Symbol.File (the vertex)
//
// If a future change makes go/types and ingest disagree on the absolute path form
// (symlink-resolved prefix, trailing-slash root, case folding, etc.), filepath.Rel
// yields a different relative string, the edge FromKey no longer matches any Symbol
// vertex, the IMPLEMENTS edge silently orphans, and #221's 2-hop returns empty —
// with zero error. This test red-fails on exactly that drift.
func TestImplementsEdgeKeysMatchSymbolGraph(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTinyForgeModule(t, root)

	// Real index path: go/types satisfaction → TypeRelationship with an ABSOLUTE
	// FileSet TypeFile (not a synthetic literal).
	rels := extractGoImplements(context.Background(), root)
	if len(rels) == 0 {
		t.Fatal("extractGoImplements produced no IMPLEMENTS relationships (go/types load failed or found no satisfaction)")
	}
	// Sanity: every subject's File must be an ABSOLUTE path (proves we are
	// exercising the go/types-abs → relPath transform, not a pre-relativized one).
	for _, r := range rels {
		if !filepath.IsAbs(r.File) {
			t.Fatalf("expected absolute go/types subject file, got %q", r.File)
		}
	}

	// Ground-truth Symbol vertex keys from the tree-sitter ingest path.
	symKeys, syms := symbolKeySet(t, root)

	// Build the IMPLEMENTS edges through the production keying function.
	edges := buildRelationshipEdges(root, rels, syms)

	var sawGitHub, sawGitLab bool
	for _, e := range edges {
		if e.EdgeLabel != edgeLabelImplements {
			continue
		}
		// THE assertion: the go/types-derived subject endpoint key is one of the
		// tree-sitter-derived Symbol vertex keys, byte-for-byte.
		if _, ok := symKeys[e.FromKey]; !ok {
			t.Errorf("IMPLEMENTS subject key %q has no matching Symbol vertex key (orphaned edge); known keys: %v",
				e.FromKey, keysOf(symKeys))
		}
		// The target (interface) endpoint must also match a Symbol vertex key.
		if _, ok := symKeys[e.ToKey]; !ok {
			t.Errorf("IMPLEMENTS target key %q has no matching Symbol vertex key (orphaned edge); known keys: %v",
				e.ToKey, keysOf(symKeys))
		}
		switch e.FromKey {
		case "GitHubForge" + compositeKeyDelim + "github.go":
			sawGitHub = true
		case "GitLabForge" + compositeKeyDelim + "gitlab.go":
			sawGitLab = true
		}
	}
	if !sawGitHub {
		t.Errorf("missing IMPLEMENTS edge for GitHubForge (expected subject key %q)",
			"GitHubForge"+compositeKeyDelim+"github.go")
	}
	if !sawGitLab {
		t.Errorf("missing IMPLEMENTS edge for GitLabForge (expected subject key %q)",
			"GitLabForge"+compositeKeyDelim+"gitlab.go")
	}
}

func keysOf(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
