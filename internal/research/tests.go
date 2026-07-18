package research

import (
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/langutil"
)

// linkTestFiles attaches matching *_test.go siblings (and equivalents for
// other languages) to each kept production file. Returns a new slice with
// test files appended at distance=0 and a small score decay (0.8× parent).
//
// Files that are already test files are left alone — no self-linking.
// Siblings that don't exist in allFiles are silently skipped.
func linkTestFiles(kept []scoredFile, allFiles map[string]bool) []scoredFile {
	const testSiblingScoreFactor = 0.8

	out := make([]scoredFile, 0, len(kept))
	added := make(map[string]bool)
	for _, sf := range kept {
		out = append(out, sf)
		added[sf.expand.relPath] = true
	}

	for _, sf := range kept {
		if langutil.IsTestFile(sf.expand.relPath) {
			continue
		}
		for _, candidate := range testCandidates(sf.expand.relPath) {
			if !allFiles[candidate] || added[candidate] {
				continue
			}
			added[candidate] = true
			out = append(out, scoredFile{
				expand: expandResult{
					relPath:   candidate,
					distance:  0,
					whyLinked: "test for " + filepath.Base(sf.expand.relPath),
				},
				seedScore: sf.seedScore * testSiblingScoreFactor,
			})
		}
	}
	return out
}

// testCandidates returns plausible test-file paths for a production file,
// covering Go, Python, Rust, and JS/TS conventions.
func testCandidates(prod string) []string {
	dir := filepath.Dir(prod)
	base := filepath.Base(prod)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	var out []string
	switch ext {
	case ".go":
		out = append(out, filepath.Join(dir, stem+"_test.go"))
	case ".py":
		out = append(out,
			filepath.Join(dir, "test_"+stem+".py"),
			filepath.Join(dir, "tests", "test_"+stem+".py"),
		)
	case ".rs":
		out = append(out, filepath.Join(dir, stem+"_test.rs"))
	case ".ts", ".tsx", ".js", ".jsx":
		out = append(out,
			filepath.Join(dir, stem+".test"+ext),
			filepath.Join(dir, stem+".spec"+ext),
		)
	case ".svelte":
		out = append(out,
			filepath.Join(dir, stem+".test.svelte"),
			filepath.Join(dir, stem+".spec.svelte"),
			filepath.Join(dir, "__tests__", stem+".test.svelte"),
			filepath.Join(dir, "__tests__", stem+".test.ts"),
		)
	case ".astro":
		out = append(out,
			filepath.Join(dir, stem+".test.astro"),
			filepath.Join(dir, stem+".spec.astro"),
			filepath.Join(dir, "__tests__", stem+".test.astro"),
		)
	}
	return out
}
