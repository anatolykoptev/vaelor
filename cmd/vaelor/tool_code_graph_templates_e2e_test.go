package main

import (
	"context"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	reGraphRow = regexp.MustCompile(`(?s)<row>(.*?)</row>`)
	reGraphCol = regexp.MustCompile(`(?s)<col>(.*?)</col>`)
)

// graphRows parses code_graph XML into rows of unquoted column strings.
func graphRows(t *testing.T, text string) [][]string {
	t.Helper()
	var out [][]string
	for _, r := range reGraphRow.FindAllStringSubmatch(text, -1) {
		var cols []string
		for _, c := range reGraphCol.FindAllStringSubmatch(r[1], -1) {
			cols = append(cols, strings.Trim(html.UnescapeString(c[1]), `"`))
		}
		out = append(out, cols)
	}
	return out
}

func branches(n int) string {
	var sb strings.Builder
	for i := range n {
		sb.WriteString("\tif x == " + strings.Repeat("1", i+1) + " {\n\t\tx++\n\t}\n")
	}
	return sb.String()
}

// TestCodeGraphTemplatesE2E runs templates against a real AGE graph and checks
// what each promises, on data built to break the old behaviour:
//   - complex_symbols ranks complexity numerically (stored as strings, the old
//     ORDER BY put "9" above "11");
//   - call_chain returns the path itself, in order (it used to return only the
//     two endpoint vertices, once per path);
//   - who_calls honours file to pick one of several same-named symbols;
//   - importers_of finds a module by its path segment (pgx/v5's name is "v5");
//   - rows are property columns, never whole vertices.
//
// Skipped unless DATABASE_URL is set (CI preflight provides postgres+age).
func TestCodeGraphTemplatesE2E(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping code_graph templates e2e test")
	}
	ctx := context.Background()

	root := t.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/cgtemplates\n\ngo 1.22\n",
		"main.go": "package main\n\nfunc main() { step1() }\n\nfunc step1() { step2() }\n\nfunc step2() { target() }\n\nfunc target() {}\n\n" +
			// 8 hops: deeper than a *1..6 ceiling, inside *1..10.
			"func deep0() { deep1() }\nfunc deep1() { deep2() }\nfunc deep2() { deep3() }\nfunc deep3() { deep4() }\n" +
			"func deep4() { deep5() }\nfunc deep5() { deep6() }\nfunc deep6() { deep7() }\nfunc deep7() { deep8() }\nfunc deep8() {}\n\n" +
			"func highComplexity(x int) int {\n" + branches(10) + "\treturn x\n}\n\n" +
			"func lowComplexity(x int) int {\n" + branches(8) + "\treturn x\n}\n",
		// pgx/v5's Package vertex is named "v5"; asked for "pgx" it must still match.
		"pa/pa.go": "package pa\n\nimport _ \"github.com/jackc/pgx/v5\"\n\nfunc Close() {}\n\nfunc UseA() { Close() }\n",
		"pb/pb.go": "package pb\n\nfunc Close() {}\n\nfunc UseB() { Close() }\n",
		// aa/ sorts before zz/: at limit=1 the exact net/http importer must
		// still win over the httptest (subpackage) one.
		"aa/aa.go": "package aa\n\nimport _ \"net/http/httptest\"\n",
		"zz/zz.go": "package zz\n\nimport _ \"net/http\"\n",
		// io/fs is a subpackage of io; "io" must reach it (path STARTS WITH "io/").
		"fsuser/fs.go": "package fsuser\n\nimport _ \"io/fs\"\n",
	}
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() }) // registered first -> runs last
	store := codegraph.NewStore(pool)
	repoKey := codegraph.GraphNameFor(root)
	t.Cleanup(func() { _ = store.DropGraph(ctx, repoKey, repoKey) })

	cfg := Config{GraphTTLLocal: 3600, GraphTTLRemote: 3600}
	if _, err := codegraph.IndexRepo(ctx, store, root, false,
		codegraph.IndexConfig{TTLLocal: cfg.GraphTTLLocal, TTLRemote: cfg.GraphTTLRemote}); err != nil {
		t.Fatalf("IndexRepo: %v", err)
	}
	deps := analyze.Deps{LLM: &countingLLM{}, LLMHasKey: true}

	var text string
	run := func(template string, params map[string]string) [][]string {
		t.Helper()
		res, err := handleCodeGraph(ctx, CodeGraphInput{Repo: root, Template: template, Params: params}, cfg, deps, store)
		if err != nil {
			t.Fatalf("%s: %v", template, err)
		}
		text = resultText(res)
		if res.IsError || strings.Contains(text, "<status>") {
			t.Fatalf("%s: unexpected result %q", template, text)
		}
		if strings.Contains(text, `"label"`) || strings.Contains(text, "::vertex") {
			t.Errorf("%s returned a whole vertex: %q", template, text)
		}
		return graphRows(t, text)
	}

	rows := run("complex_symbols", map[string]string{"limit": "50"})
	pos := map[string]int{}
	for i, r := range rows {
		pos[r[0]] = i
	}
	hi, okHi := pos["highComplexity"]
	lo, okLo := pos["lowComplexity"]
	if !okHi || !okLo || hi > lo {
		t.Errorf("complex_symbols order wrong (high=%d ok=%v, low=%d ok=%v): %v", hi, okHi, lo, okLo, rows)
	}

	rows = run("hotspots", map[string]string{"limit": "50"})
	pos = map[string]int{}
	for i, r := range rows {
		pos[r[0]] = i
	}
	hi, okHi = pos["highComplexity"]
	lo, okLo = pos["lowComplexity"]
	if !okHi || !okLo || hi > lo {
		t.Errorf("hotspots order wrong (high=%d ok=%v, low=%d ok=%v): %v", hi, okHi, lo, okLo, rows)
	}

	rows = run("call_chain", map[string]string{"from": "deep0", "to": "deep8"})
	if len(rows) != 9 || rows[0][0] != "deep0" || rows[8][0] != "deep8" {
		t.Errorf("call_chain deep0->deep8 (8 hops) = %v, want the 9-symbol path", rows)
	}

	rows = run("who_calls", map[string]string{"name": "Close", "limit": "1"})
	if len(rows) != 1 || !strings.Contains(text, `truncated="true"`) || !strings.Contains(text, `limit="1"`) {
		t.Errorf("who_calls Close limit=1: want 1 row flagged truncated, got %d rows: %q", len(rows), text)
	}
	rows = run("who_calls", map[string]string{"name": "Close", "limit": "5"})
	if len(rows) != 2 || strings.Contains(text, "truncated") {
		t.Errorf("who_calls Close limit=5: want 2 rows, not truncated, got %d rows: %q", len(rows), text)
	}

	rows = run("call_chain", map[string]string{"from": "main", "to": "target"})
	var chain []string
	for _, r := range rows {
		chain = append(chain, r[0])
	}
	if got := strings.Join(chain, ">"); got != "main>step1>step2>target" {
		t.Errorf("call_chain = %q, want main>step1>step2>target", got)
	}

	rows = run("who_calls", map[string]string{"name": "Close", "file": "pa/"})
	if len(rows) == 0 {
		t.Fatal("who_calls Close file=pa/: no rows")
	}
	for _, r := range rows {
		if len(r) != 5 || !strings.Contains(r[4], "pa/") {
			t.Errorf("who_calls file=pa/ returned a row for another Close: %v", r)
		}
	}

	rows = run("importers_of", map[string]string{"name": "pgx"})
	if len(rows) != 1 || rows[0][0] != "pa/pa.go" || rows[0][1] != "github.com/jackc/pgx/v5" || rows[0][2] != "subpackage" {
		t.Errorf("importers_of pgx = %v, want [[pa/pa.go github.com/jackc/pgx/v5 subpackage]]", rows)
	}
	rows = run("importers_of", map[string]string{"name": "http", "limit": "1"})
	if len(rows) != 1 || rows[0][0] != "zz/zz.go" || rows[0][2] != "exact" || !strings.Contains(text, `truncated="true"`) {
		t.Errorf("importers_of http limit=1 = %v, want the exact zz/zz.go row first (truncated)", rows)
	}

	rows = run("dependents_of", map[string]string{"name": "io"})
	if len(rows) != 1 || rows[0][0] != "fsuser/fs.go" || rows[0][1] != "io/fs" || rows[0][2] != "subpackage" {
		t.Errorf("dependents_of io = %v, want [[fsuser/fs.go io/fs subpackage]]", rows)
	}

	rows = run("dependents_of", map[string]string{"name": "v5"})
	if len(rows) != 1 || rows[0][2] != "exact" {
		t.Errorf("dependents_of v5 = %v, want one exact match", rows)
	}
}
