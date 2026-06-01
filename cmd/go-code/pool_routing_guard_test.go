package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// repoRoot walks up from the test's working directory (the package dir) until it
// finds go.mod, returning the module root. Fails the test if not found.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found walking up from test dir")
		}
		dir = parent
	}
}

// walkProdGoFiles calls fn for every non-test .go file under internal/ and cmd/.
func walkProdGoFiles(t *testing.T, root string, fn func(path string, src []byte)) {
	t.Helper()
	for _, sub := range []string{"internal", "cmd"} {
		base := filepath.Join(root, sub)
		err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			fn(path, src)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", base, err)
		}
	}
}

// TestNoBareDataTableDML is the Tier-1 schema-qualification guard.
//
// go-code's data tables (code_repo_state, code_embeddings, code_health_cache) live in
// `public`, but Apache AGE shares the same database and forces `ag_catalog` into the
// session search_path. A BARE (unqualified) reference to one of these tables in DML
// resolves by search_path — which means a connection dirtied by an AGE query would route
// the write into ag_catalog (the historic search_path-leak bug class). Every production
// DML site MUST schema-qualify with `public.` so correctness never depends on the
// connection's ambient search_path.
//
// This test fails if any non-test .go file under internal/ or cmd/ contains a bare
// `FROM|INTO|UPDATE|JOIN code_(repo_state|embeddings|health_cache)` without a `public.`
// (or `ag_catalog.`) schema prefix. Falsification: drop the `public.` from any data-path
// query and this test goes red.
func TestNoBareDataTableDML(t *testing.T) {
	root := repoRoot(t)
	// `bare` matches a DML keyword followed by a data-table name; `qualified` matches a
	// schema-prefixed reference. Go regexp has no lookbehind, so to decide whether a
	// line has a BARE reference we first strip every qualified reference from the line,
	// then test the residue. This is per-occurrence: a line that mixes a qualified ref
	// and a bare ref (e.g. `DELETE FROM code_embeddings a USING public.code_repo_state b`)
	// is still caught — a whole-line "has any qualified hit → accept" shortcut would
	// miss it.
	bare := regexp.MustCompile(`(?i)\b(?:FROM|INTO|UPDATE|JOIN)\s+code_(?:repo_state|embeddings|health_cache)\b`)
	qualified := regexp.MustCompile(`(?i)(?:public|ag_catalog)\.code_(?:repo_state|embeddings|health_cache)\b`)

	var offenders []string
	walkProdGoFiles(t, root, func(path string, src []byte) {
		for i, line := range strings.Split(string(src), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue // comment line
			}
			residue := qualified.ReplaceAllString(line, "")
			if !bare.MatchString(residue) {
				continue
			}
			rel, _ := filepath.Rel(root, path)
			offenders = append(offenders, rel+":"+strconv.Itoa(i+1)+"  "+trimmed)
		}
	})
	if len(offenders) > 0 {
		t.Fatalf("bare (unqualified) data-table DML found — must use public.code_* "+
			"(search_path-leak guard, Tier 1):\n  %s", strings.Join(offenders, "\n  "))
	}
}

// TestPoolRoutingInvariant is the Tier-2 pool-separation guard.
//
// register.go wires two pools: agePool (Apache AGE consumers, carries the RESET ALL
// search_path-resetting release hook) and dataPool (pure pgvector/relational consumers,
// pristine — never runs SET search_path). The leak is structurally impossible on the
// data path ONLY if data stores are wired to dataPool and AGE stores to agePool. This
// test asserts that wiring stays correct. Falsification: change any wiring below (e.g.
// embeddings.NewStore(agePool)) and the test goes red.
func TestPoolRoutingInvariant(t *testing.T) {
	root := repoRoot(t)
	src, err := os.ReadFile(filepath.Join(root, "cmd", "go-code", "register.go"))
	if err != nil {
		t.Fatalf("read register.go: %v", err)
	}
	text := string(src)

	mustContain := map[string]string{
		"codegraph.NewStore(agePool)":     "AGE graph store must use agePool",
		"embeddings.NewExpander(agePool)": "AGE cypher expander must use agePool",
		"embeddings.NewStore(dataPool)":   "pgvector embeddings store must use dataPool",
		"designmd.NewStore(dataPool)":     "design-doc store must use dataPool",
	}
	for token, why := range mustContain {
		if !strings.Contains(text, token) {
			t.Errorf("pool-routing invariant broken: expected %q (%s)", token, why)
		}
	}

	// The data stores must NOT be wired to agePool (would re-couple the data path to the
	// search_path-mutating pool), and AGE stores must NOT be wired to dataPool (the
	// hookless pool — AGE queries would dirty it with no reset).
	mustNotContain := map[string]string{
		"embeddings.NewStore(agePool)":     "embeddings data store must not use agePool",
		"designmd.NewStore(agePool)":       "design store must not use agePool",
		"codegraph.NewStore(dataPool)":     "AGE store must not use the hookless dataPool",
		"embeddings.NewExpander(dataPool)": "AGE expander must not use the hookless dataPool",
	}
	for token, why := range mustNotContain {
		if strings.Contains(text, token) {
			t.Errorf("pool-routing invariant broken: found forbidden %q (%s)", token, why)
		}
	}
}
