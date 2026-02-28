# Phase 4.2: Code Graph — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add persistent code graph in Apache AGE with NL→Cypher queries via one new `code_graph` MCP tool.

**Architecture:** New `internal/codegraph/` package handles AGE storage, indexing, and querying. Tool handler in `cmd/go-code/tool_code_graph.go`. pgx/v5 pool added to `analyze.Deps`. Lazy indexing with TTL (1h local, 24h remote). Hybrid query: 10 Cypher templates + LLM freeform fallback.

**Tech Stack:** pgx/v5 (PostgreSQL), Apache AGE (Cypher), existing parser/ingest/callgraph/llm packages.

**Design doc:** `docs/plans/2026-02-28-code-graph-design.md`

---

## Task 1: Add pgx/v5 dependency and Config fields

**Files:**
- Modify: `go.mod`
- Modify: `cmd/go-code/config.go`

**Step 1: Add pgx/v5 dependency**

Run:
```bash
cd /home/krolik/src/go-code && go get github.com/jackc/pgx/v5
```

Expected: `go.mod` updated with `pgx/v5` in require block.

**Step 2: Add DATABASE_URL and graph config to Config struct**

In `cmd/go-code/config.go`, add fields to the `Config` struct after `MaxRepoBytes`:

```go
// Database URL for PostgreSQL (Apache AGE). Empty = code_graph tool disabled.
DatabaseURL string

// Graph TTL for local repos (seconds).
GraphTTLLocal int

// Graph TTL for remote repos (seconds).
GraphTTLRemote int

// Batch size for graph upsert operations.
GraphBatchSize int
```

Add constants:

```go
defaultGraphTTLLocal  = 3600
defaultGraphTTLRemote = 86400
defaultGraphBatchSize = 100
```

In `loadConfig()`, add:

```go
DatabaseURL:    env("DATABASE_URL", ""),
GraphTTLLocal:  envInt("GRAPH_TTL_LOCAL", defaultGraphTTLLocal),
GraphTTLRemote: envInt("GRAPH_TTL_REMOTE", defaultGraphTTLRemote),
GraphBatchSize: envInt("GRAPH_BATCH_SIZE", defaultGraphBatchSize),
```

**Step 3: Run `go mod tidy`**

Run: `cd /home/krolik/src/go-code && go mod tidy`

Expected: clean exit, no errors.

**Step 4: Commit**

```bash
git add go.mod go.sum cmd/go-code/config.go
git commit -m "feat: add pgx/v5 dep and graph config fields"
```

---

## Task 2: GraphStore — schema, pool, escapeCypher

**Files:**
- Create: `internal/codegraph/store.go`
- Create: `internal/codegraph/store_test.go`

**Step 1: Write the test**

Create `internal/codegraph/store_test.go`:

```go
package codegraph

import (
	"testing"
)

func TestEscapeCypher(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"it's", `it\'s`},
		{`he said "hi"`, `he said \"hi\"`},
		{"back\\slash", `back\\slash`},
		{"null\x00byte", "nullbyte"},
		{"new\nline", `new\nline`},
		{"tab\there", `tab\there`},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := escapeCypher(tt.input)
			if got != tt.want {
				t.Errorf("escapeCypher(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGraphName(t *testing.T) {
	name := graphName("/home/krolik/src/go-code")
	if name == "" {
		t.Fatal("graphName returned empty")
	}
	// Must start with "code_"
	if name[:5] != "code_" {
		t.Errorf("graphName = %q, want prefix 'code_'", name)
	}
	// Same input → same output (deterministic)
	if graphName("/home/krolik/src/go-code") != name {
		t.Error("graphName not deterministic")
	}
	// Different input → different output
	if graphName("/home/krolik/src/go-search") == name {
		t.Error("graphName collision")
	}
}

func TestIsReadOnly(t *testing.T) {
	tests := []struct {
		cypher string
		want   bool
	}{
		{"MATCH (n) RETURN n", true},
		{"MATCH (n) RETURN count(n)", true},
		{"CREATE (n:Foo)", false},
		{"MATCH (n) DELETE n", false},
		{"MATCH (n) SET n.x = 1", false},
		{"MERGE (n:Foo {id: 1})", false},
		{"MATCH (n) REMOVE n.x", false},
		{"DROP GRAPH foo CASCADE", false},
		{"match (n) return n", true},           // lowercase MATCH/RETURN
		{"MATCH (n) DETACH DELETE n", false},
	}
	for _, tt := range tests {
		t.Run(tt.cypher, func(t *testing.T) {
			got := isReadOnly(tt.cypher)
			if got != tt.want {
				t.Errorf("isReadOnly(%q) = %v, want %v", tt.cypher, got, tt.want)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/codegraph/ -v -run 'TestEscape|TestGraph|TestIsRead'`

Expected: FAIL — package doesn't exist.

**Step 3: Write the implementation**

Create `internal/codegraph/store.go`:

```go
// Package codegraph provides Apache AGE graph storage for code intelligence.
//
// It stores parsed symbols (packages, files, functions) and their relationships
// (CONTAINS, CALLS, IMPORTS) in a PostgreSQL + Apache AGE graph database.
// Queries are executed via ag_catalog.cypher() wrapped in standard SQL.
package codegraph

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ageSetup runs per-connection AGE initialization.
const ageSetup = `LOAD 'age'; SET search_path TO ag_catalog, "$user", public`

// writeOps matches Cypher write operations that must be rejected in freeform queries.
var writeOps = regexp.MustCompile(`(?i)\b(CREATE|DELETE|SET|MERGE|REMOVE|DROP|DETACH)\b`)

// Store provides AGE graph operations backed by a pgxpool.
type Store struct {
	pool     *pgxpool.Pool
	ageOnce  sync.Once
	ageAvail bool
}

// NewStore creates a new graph store from the given connection pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// HasAGE checks if Apache AGE is available. Result is cached.
func (s *Store) HasAGE(ctx context.Context) bool {
	s.ageOnce.Do(func() {
		conn, err := s.pool.Acquire(ctx)
		if err != nil {
			slog.Warn("AGE check: acquire failed", slog.Any("error", err))
			return
		}
		defer conn.Release()

		var exists bool
		err = conn.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM pg_extension WHERE extname = 'age')`,
		).Scan(&exists)
		if err != nil {
			slog.Warn("AGE check: query failed", slog.Any("error", err))
			return
		}
		s.ageAvail = exists
		slog.Info("AGE availability", slog.Bool("available", exists))
	})
	return s.ageAvail
}

// ExecCypher executes a Cypher query on the named graph and returns raw JSON rows.
// Each row is a slice of []byte (one per RETURN column, agtype JSON).
func (s *Store) ExecCypher(ctx context.Context, graph, cypher string, cols int) ([][]string, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, ageSetup); err != nil {
		return nil, fmt.Errorf("age setup: %w", err)
	}

	// Build AS clause with the right number of columns.
	asCols := make([]string, cols)
	for i := range cols {
		asCols[i] = fmt.Sprintf("c%d ag_catalog.agtype", i)
	}

	sql := fmt.Sprintf(
		`SELECT * FROM ag_catalog.cypher('%s', $$ %s $$) AS (%s)`,
		graph, cypher, strings.Join(asCols, ", "),
	)

	rows, err := conn.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("cypher exec: %w", err)
	}
	defer rows.Close()

	var results [][]string
	for rows.Next() {
		vals := make([]any, cols)
		ptrs := make([]any, cols)
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			slog.Warn("cypher scan failed", slog.Any("error", err))
			continue
		}
		row := make([]string, cols)
		for i, v := range vals {
			row[i] = fmt.Sprintf("%v", v)
		}
		results = append(results, row)
	}
	return results, rows.Err()
}

// ExecCypherWrite executes a write Cypher (CREATE/MERGE) that returns no rows.
func (s *Store) ExecCypherWrite(ctx context.Context, graph, cypher string) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, ageSetup); err != nil {
		return fmt.Errorf("age setup: %w", err)
	}

	sql := fmt.Sprintf(
		`SELECT * FROM ag_catalog.cypher('%s', $$ %s $$) AS (v ag_catalog.agtype)`,
		graph, cypher,
	)

	rows, err := conn.Query(ctx, sql)
	if err != nil {
		return fmt.Errorf("cypher write: %w", err)
	}
	rows.Close()
	return rows.Err()
}

// EnsureGraph creates the AGE graph and metadata table if they don't exist.
func (s *Store) EnsureGraph(ctx context.Context, name string) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, ageSetup); err != nil {
		return fmt.Errorf("age setup: %w", err)
	}

	// Create graph if not exists.
	_, err = conn.Exec(ctx, fmt.Sprintf(`
		DO $$
		BEGIN
			IF NOT EXISTS (SELECT 1 FROM ag_catalog.ag_graph WHERE name = '%s') THEN
				PERFORM ag_catalog.create_graph('%s');
			END IF;
		END $$`, name, name))
	if err != nil {
		return fmt.Errorf("create graph: %w", err)
	}

	// Create metadata table.
	_, err = conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS code_graph_meta (
			repo_key     TEXT PRIMARY KEY,
			repo_path    TEXT NOT NULL,
			graph_name   TEXT NOT NULL,
			file_count   INT,
			symbol_count INT,
			edge_count   INT,
			built_at     TIMESTAMPTZ NOT NULL,
			ttl_seconds  INT DEFAULT 3600
		)`)
	if err != nil {
		return fmt.Errorf("create meta table: %w", err)
	}

	return nil
}

// DropGraph drops an AGE graph and its metadata entry.
func (s *Store) DropGraph(ctx context.Context, name, repoKey string) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, ageSetup); err != nil {
		return fmt.Errorf("age setup: %w", err)
	}

	_, _ = conn.Exec(ctx, fmt.Sprintf(
		`SELECT ag_catalog.drop_graph('%s', true)`, name))

	_, err = conn.Exec(ctx, `DELETE FROM code_graph_meta WHERE repo_key = $1`, repoKey)
	return err
}

// Pool returns the underlying connection pool (for testing).
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// graphName generates a deterministic graph name from a repo path.
func graphName(repoPath string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(repoPath))
	return fmt.Sprintf("code_%08x", h.Sum32())
}

// escapeCypher escapes a string for safe use in single-quoted Cypher literals.
func escapeCypher(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "`", "\\`")
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

// isReadOnly returns true if a Cypher query contains no write operations.
func isReadOnly(cypher string) bool {
	return !writeOps.MatchString(cypher)
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-code && go test ./internal/codegraph/ -v -run 'TestEscape|TestGraph|TestIsRead'`

Expected: 3 tests PASS.

**Step 5: Commit**

```bash
git add internal/codegraph/store.go internal/codegraph/store_test.go
git commit -m "feat(codegraph): add Store with AGE helpers, escapeCypher, isReadOnly"
```

---

## Task 3: Cypher query templates

**Files:**
- Create: `internal/codegraph/templates.go`
- Create: `internal/codegraph/templates_test.go`

**Step 1: Write the test**

Create `internal/codegraph/templates_test.go`:

```go
package codegraph

import (
	"testing"
)

func TestTemplateRender(t *testing.T) {
	tests := []struct {
		id     string
		params map[string]string
		want   string // substring that must appear
	}{
		{"who_calls", map[string]string{"name": "Serve"}, "Symbol {name: 'Serve'}"},
		{"calls_of", map[string]string{"name": "main"}, "Symbol {name: 'main'}"},
		{"imports_of", map[string]string{"path": "handler.go"}, "CONTAINS 'handler.go'"},
		{"importers_of", map[string]string{"name": "net/http"}, "Package {name: 'net/http'}"},
		{"symbols_in", map[string]string{"path": "internal/store"}, "CONTAINS 'internal/store'"},
		{"call_chain", map[string]string{"from": "A", "to": "B"}, "shortestPath"},
		{"most_connected", map[string]string{"limit": "10"}, "LIMIT 10"},
		{"dead_code", map[string]string{}, "NOT ()"},
		{"depends_on", map[string]string{"pkg": "internal/auth"}, "CONTAINS 'internal/auth'"},
		{"dependents_of", map[string]string{"name": "database/sql"}, "Package {name: 'database/sql'}"},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			tmpl, ok := templates[tt.id]
			if !ok {
				t.Fatalf("template %q not found", tt.id)
			}
			got := tmpl.Render(tt.params)
			if got == "" {
				t.Fatal("rendered empty")
			}
			if !containsSubstring(got, tt.want) {
				t.Errorf("render(%q) = %q, want substring %q", tt.id, got, tt.want)
			}
		})
	}
}

func TestTemplateCount(t *testing.T) {
	if len(templates) != 10 {
		t.Errorf("expected 10 templates, got %d", len(templates))
	}
}

func TestAllTemplatesHaveColumns(t *testing.T) {
	for id, tmpl := range templates {
		if tmpl.Cols < 1 {
			t.Errorf("template %q has %d cols, want >= 1", id, tmpl.Cols)
		}
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && len(sub) > 0 && stringContains(s, sub)))
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/codegraph/ -v -run TestTemplate`

Expected: FAIL — `templates` not defined.

**Step 3: Write the implementation**

Create `internal/codegraph/templates.go`:

```go
package codegraph

import (
	"fmt"
	"strings"
)

// Template is a parameterized Cypher query template.
type Template struct {
	// ID is the template identifier used by the classifier.
	ID string

	// Description explains what this template answers (for classifier prompt).
	Description string

	// Params lists required parameter names.
	Params []string

	// Cypher is the template string with $param placeholders.
	Cypher string

	// Cols is the number of RETURN columns (for ExecCypher).
	Cols int
}

// Render substitutes params into the Cypher template.
// Params are escaped for safe Cypher interpolation.
func (t Template) Render(params map[string]string) string {
	result := t.Cypher
	for _, p := range t.Params {
		val := escapeCypher(params[p])
		result = strings.ReplaceAll(result, "$"+p, val)
	}
	return result
}

// templates is the registry of all built-in Cypher query templates.
var templates = map[string]Template{
	"who_calls": {
		ID:          "who_calls",
		Description: "Find all functions that call a given function",
		Params:      []string{"name"},
		Cypher:      "MATCH (caller:Symbol)-[:CALLS]->(target:Symbol {name: '$name'}) RETURN caller",
		Cols:        1,
	},
	"calls_of": {
		ID:          "calls_of",
		Description: "Find all functions called by a given function",
		Params:      []string{"name"},
		Cypher:      "MATCH (src:Symbol {name: '$name'})-[:CALLS]->(callee:Symbol) RETURN callee",
		Cols:        1,
	},
	"imports_of": {
		ID:          "imports_of",
		Description: "Find all packages imported by a file",
		Params:      []string{"path"},
		Cypher:      "MATCH (f:File)-[:IMPORTS]->(p:Package) WHERE f.path CONTAINS '$path' RETURN p",
		Cols:        1,
	},
	"importers_of": {
		ID:          "importers_of",
		Description: "Find all files that import a given package",
		Params:      []string{"name"},
		Cypher:      "MATCH (f:File)-[:IMPORTS]->(p:Package {name: '$name'}) RETURN f",
		Cols:        1,
	},
	"symbols_in": {
		ID:          "symbols_in",
		Description: "List all symbols in a file or package",
		Params:      []string{"path"},
		Cypher:      "MATCH (c)-[:CONTAINS]->(s:Symbol) WHERE c.path CONTAINS '$path' RETURN s",
		Cols:        1,
	},
	"call_chain": {
		ID:          "call_chain",
		Description: "Find the shortest call path between two functions",
		Params:      []string{"from", "to"},
		Cypher:      "MATCH path = shortestPath((a:Symbol {name: '$from'})-[:CALLS*..10]->(b:Symbol {name: '$to'})) RETURN path",
		Cols:        1,
	},
	"most_connected": {
		ID:          "most_connected",
		Description: "Find the most-called functions in the codebase",
		Params:      []string{"limit"},
		Cypher:      "MATCH (s:Symbol)<-[:CALLS]-(caller:Symbol) RETURN s.name, s.kind, s.file, count(caller) AS call_count ORDER BY call_count DESC LIMIT $limit",
		Cols:        4,
	},
	"dead_code": {
		ID:          "dead_code",
		Description: "Find functions that are never called by any other function",
		Params:      []string{},
		Cypher:      "MATCH (s:Symbol) WHERE s.kind = 'function' AND NOT ()-[:CALLS]->(s) RETURN s",
		Cols:        1,
	},
	"depends_on": {
		ID:          "depends_on",
		Description: "Find all packages that a given package depends on",
		Params:      []string{"pkg"},
		Cypher:      "MATCH (f:File)-[:IMPORTS]->(p:Package) WHERE f.path CONTAINS '$pkg' RETURN DISTINCT p",
		Cols:        1,
	},
	"dependents_of": {
		ID:          "dependents_of",
		Description: "Find all files that depend on a given package",
		Params:      []string{"name"},
		Cypher:      "MATCH (f:File)-[:IMPORTS]->(p:Package {name: '$name'}) RETURN DISTINCT f",
		Cols:        1,
	},
}

// GetTemplate returns a template by ID, or nil if not found.
func GetTemplate(id string) *Template {
	t, ok := templates[id]
	if !ok {
		return nil
	}
	return &t
}

// TemplateList returns a formatted list of templates for the classifier prompt.
func TemplateList() string {
	var sb strings.Builder
	for id, t := range templates {
		sb.WriteString(fmt.Sprintf("- %s: %s (params: %s)\n", id, t.Description, strings.Join(t.Params, ", ")))
	}
	return sb.String()
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-code && go test ./internal/codegraph/ -v -run TestTemplate`

Expected: 3 tests PASS.

**Step 5: Commit**

```bash
git add internal/codegraph/templates.go internal/codegraph/templates_test.go
git commit -m "feat(codegraph): add 10 Cypher query templates with param substitution"
```

---

## Task 4: LLM classifier — NL→template

**Files:**
- Create: `internal/codegraph/classify.go`
- Create: `internal/codegraph/classify_test.go`
- Modify: `internal/llm/llm.go` (add system prompt)

**Step 1: Add system prompt to llm.go**

In `internal/llm/llm.go`, add after `SystemPromptCallTrace`:

```go
// SystemPromptClassifyGraphQuery classifies a natural-language query into a graph template.
const SystemPromptClassifyGraphQuery = `You are a query classifier for a code knowledge graph.

Given a natural-language question about code, select the best matching template and extract parameters.

Available templates:
%s

Respond with ONLY a JSON object, no explanation:
{"template": "<template_id>", "params": {"param_name": "value"}}

If no template fits, respond:
{"template": "freeform", "params": {}}

Rules:
- Extract symbol/function/package names from the question into params
- Use "freeform" only if the question truly doesn't match any template
- Parameter values should be exact names from the question (case-sensitive)`
```

**Step 2: Write the test**

Create `internal/codegraph/classify_test.go`:

```go
package codegraph

import (
	"encoding/json"
	"testing"
)

func TestParseClassification(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantTmpl string
		wantOK   bool
	}{
		{"valid template", `{"template": "who_calls", "params": {"name": "Serve"}}`, "who_calls", true},
		{"freeform", `{"template": "freeform", "params": {}}`, "freeform", true},
		{"with markdown", "```json\n{\"template\": \"dead_code\", \"params\": {}}\n```", "dead_code", true},
		{"garbage", "I don't know", "", false},
		{"empty", "", "", false},
		{"unknown template", `{"template": "nonexistent", "params": {}}`, "freeform", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cls, err := parseClassification(tt.raw)
			if tt.wantOK {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if cls.Template != tt.wantTmpl {
					t.Errorf("template = %q, want %q", cls.Template, tt.wantTmpl)
				}
			} else {
				if err == nil {
					t.Error("expected error, got nil")
				}
			}
		})
	}
}

func TestClassificationJSON(t *testing.T) {
	c := Classification{
		Template: "who_calls",
		Params:   map[string]string{"name": "Serve"},
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var c2 Classification
	if err := json.Unmarshal(data, &c2); err != nil {
		t.Fatal(err)
	}
	if c2.Template != c.Template || c2.Params["name"] != "Serve" {
		t.Errorf("roundtrip failed: %+v", c2)
	}
}
```

**Step 3: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/codegraph/ -v -run TestParseClas`

Expected: FAIL — `parseClassification` not defined.

**Step 4: Write the implementation**

Create `internal/codegraph/classify.go`:

```go
package codegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/anatolykoptev/go-code/internal/llm"
)

// Classification is the result of classifying a NL query against graph templates.
type Classification struct {
	Template string            `json:"template"`
	Params   map[string]string `json:"params"`
}

// Classify sends the NL query to LLM to select a template and extract params.
func Classify(ctx context.Context, llmClient *llm.Client, query string) (*Classification, error) {
	prompt := fmt.Sprintf(llm.SystemPromptClassifyGraphQuery, TemplateList())
	raw, err := llmClient.Complete(ctx, prompt, query)
	if err != nil {
		return nil, fmt.Errorf("llm classify: %w", err)
	}

	cls, err := parseClassification(raw)
	if err != nil {
		// Fallback to freeform on parse failure.
		return &Classification{Template: "freeform", Params: map[string]string{}}, nil
	}
	return cls, nil
}

// jsonBlock extracts JSON from possible markdown code blocks.
var jsonBlock = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")

// parseClassification parses the LLM response into a Classification.
func parseClassification(raw string) (*Classification, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty response")
	}

	// Try to extract from markdown code block.
	if m := jsonBlock.FindStringSubmatch(raw); len(m) > 1 {
		raw = m[1]
	}

	var cls Classification
	if err := json.Unmarshal([]byte(raw), &cls); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	if cls.Template == "" {
		return nil, fmt.Errorf("empty template field")
	}

	// Validate template exists, fallback to freeform for unknown.
	if cls.Template != "freeform" {
		if GetTemplate(cls.Template) == nil {
			cls.Template = "freeform"
		}
	}

	if cls.Params == nil {
		cls.Params = map[string]string{}
	}

	return &cls, nil
}
```

**Step 5: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-code && go test ./internal/codegraph/ -v -run 'TestParseClas|TestClassif'`

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/codegraph/classify.go internal/codegraph/classify_test.go internal/llm/llm.go
git commit -m "feat(codegraph): add NL→template classifier with LLM + fallback"
```

---

## Task 5: Freeform Cypher generator

**Files:**
- Create: `internal/codegraph/generate.go`
- Create: `internal/codegraph/generate_test.go`
- Modify: `internal/llm/llm.go` (add system prompt)

**Step 1: Add system prompt to llm.go**

In `internal/llm/llm.go`, add:

```go
// SystemPromptGenerateCypher generates a read-only Cypher query from natural language.
const SystemPromptGenerateCypher = `You are a Cypher query generator for a code knowledge graph stored in Apache AGE.

Graph schema:
- Vertex labels: Package (name, path, repo), File (path, language, lines), Symbol (name, kind, signature, file, start_line, end_line)
- Edge labels: CONTAINS (Package→File, File→Symbol), CALLS (Symbol→Symbol, line property), IMPORTS (File→Package, alias property)
- kind values: function, method, type, struct, interface, class, const, var, module

Generate a READ-ONLY Cypher query. Do NOT use CREATE, DELETE, SET, MERGE, REMOVE, or DROP.

Respond with ONLY the Cypher query, no explanation.`
```

**Step 2: Write the test**

Create `internal/codegraph/generate_test.go`:

```go
package codegraph

import (
	"testing"
)

func TestIsReadOnlyGuard(t *testing.T) {
	// Verify guard works on LLM-like outputs.
	readQueries := []string{
		"MATCH (s:Symbol {name: 'Foo'})-[:CALLS]->(c) RETURN c",
		"MATCH (f:File) WHERE f.language = 'go' RETURN count(f)",
		"MATCH path = (a)-[:CALLS*1..5]->(b) RETURN path",
	}
	for _, q := range readQueries {
		if !isReadOnly(q) {
			t.Errorf("isReadOnly(%q) = false, want true", q)
		}
	}

	writeQueries := []string{
		"CREATE (n:Symbol {name: 'evil'})",
		"MATCH (n) SET n.hacked = true",
		"MATCH (n) DELETE n",
		"MERGE (n:Symbol {name: 'evil'})",
	}
	for _, q := range writeQueries {
		if isReadOnly(q) {
			t.Errorf("isReadOnly(%q) = true, want false", q)
		}
	}
}

func TestExtractCypher(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"plain", "MATCH (n) RETURN n", "MATCH (n) RETURN n"},
		{"with markdown", "```cypher\nMATCH (n) RETURN n\n```", "MATCH (n) RETURN n"},
		{"with explanation", "Here's the query:\n```\nMATCH (n) RETURN n\n```\nThis finds all nodes.", "MATCH (n) RETURN n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCypher(tt.raw)
			if got != tt.want {
				t.Errorf("extractCypher() = %q, want %q", got, tt.want)
			}
		})
	}
}
```

**Step 3: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/codegraph/ -v -run 'TestIsReadOnlyGuard|TestExtractCypher'`

Expected: FAIL — `extractCypher` not defined.

**Step 4: Write the implementation**

Create `internal/codegraph/generate.go`:

```go
package codegraph

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/anatolykoptev/go-code/internal/llm"
)

// cypherBlock extracts Cypher from markdown code blocks.
var cypherBlock = regexp.MustCompile("(?s)```(?:cypher)?\\s*(.+?)\\s*```")

// GenerateCypher asks LLM to create a read-only Cypher query for the given NL question.
// Returns the Cypher string or error. Validates read-only before returning.
func GenerateCypher(ctx context.Context, llmClient *llm.Client, query string) (string, error) {
	raw, err := llmClient.Complete(ctx, llm.SystemPromptGenerateCypher, query)
	if err != nil {
		return "", fmt.Errorf("llm generate: %w", err)
	}

	cypher := extractCypher(raw)
	if cypher == "" {
		return "", fmt.Errorf("empty cypher from LLM")
	}

	if !isReadOnly(cypher) {
		return "", fmt.Errorf("generated cypher contains write operations")
	}

	return cypher, nil
}

// GenerateCypherWithRetry tries once, and on failure retries with the error message.
func GenerateCypherWithRetry(ctx context.Context, llmClient *llm.Client, query string, firstErr error) (string, error) {
	retryPrompt := fmt.Sprintf("Previous query failed with: %s\n\nOriginal question: %s\n\nGenerate a corrected READ-ONLY Cypher query.", firstErr, query)
	raw, err := llmClient.Complete(ctx, llm.SystemPromptGenerateCypher, retryPrompt)
	if err != nil {
		return "", fmt.Errorf("llm retry: %w", err)
	}

	cypher := extractCypher(raw)
	if cypher == "" {
		return "", fmt.Errorf("empty cypher from LLM retry")
	}

	if !isReadOnly(cypher) {
		return "", fmt.Errorf("retry cypher contains write operations")
	}

	return cypher, nil
}

// extractCypher extracts a Cypher query from LLM output that may include markdown.
func extractCypher(raw string) string {
	raw = strings.TrimSpace(raw)

	if m := cypherBlock.FindStringSubmatch(raw); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}

	// If no code block, assume the whole thing is Cypher.
	return raw
}
```

**Step 5: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-code && go test ./internal/codegraph/ -v -run 'TestIsReadOnlyGuard|TestExtractCypher'`

Expected: PASS.

**Step 6: Commit**

```bash
git add internal/codegraph/generate.go internal/codegraph/generate_test.go internal/llm/llm.go
git commit -m "feat(codegraph): add freeform Cypher generator with read-only guard"
```

---

## Task 6: IndexRepo — build graph from parsed code

**Files:**
- Create: `internal/codegraph/index.go`
- Create: `internal/codegraph/index_test.go`

**Step 1: Write the test**

Create `internal/codegraph/index_test.go`:

```go
package codegraph

import (
	"testing"
	"time"
)

func TestIsFresh(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name    string
		builtAt time.Time
		ttl     int
		want    bool
	}{
		{"fresh", now.Add(-30 * time.Minute), 3600, true},
		{"stale", now.Add(-2 * time.Hour), 3600, false},
		{"exact boundary", now.Add(-time.Duration(3600) * time.Second), 3600, false},
		{"zero ttl", now, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isFresh(tt.builtAt, tt.ttl)
			if got != tt.want {
				t.Errorf("isFresh(%v, %d) = %v, want %v", tt.builtAt, tt.ttl, got, tt.want)
			}
		})
	}
}

func TestBuildBatchCypher(t *testing.T) {
	vertices := []vertexData{
		{Label: "Symbol", Props: map[string]string{"name": "Foo", "kind": "function", "file": "main.go"}},
		{Label: "Symbol", Props: map[string]string{"name": "Bar", "kind": "method", "file": "bar.go"}},
	}

	cypher := buildVertexBatch("code_test", vertices)
	if cypher == "" {
		t.Fatal("empty batch cypher")
	}
	// Must contain UNWIND
	if !stringContains(cypher, "UNWIND") {
		t.Error("batch cypher missing UNWIND")
	}
	// Must contain both names
	if !stringContains(cypher, "Foo") || !stringContains(cypher, "Bar") {
		t.Error("batch cypher missing vertex names")
	}
}

func TestBuildEdgeBatchCypher(t *testing.T) {
	edges := []edgeData{
		{FromLabel: "File", FromKey: "main.go", ToLabel: "Symbol", ToKey: "Foo", EdgeLabel: "CONTAINS"},
		{FromLabel: "Symbol", FromKey: "main", ToLabel: "Symbol", ToKey: "Foo", EdgeLabel: "CALLS", Props: map[string]string{"line": "42"}},
	}

	cypher := buildEdgeBatch("code_test", edges)
	if cypher == "" {
		t.Fatal("empty edge batch")
	}
	if !stringContains(cypher, "CONTAINS") || !stringContains(cypher, "CALLS") {
		t.Error("edge batch missing edge labels")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /home/krolik/src/go-code && go test ./internal/codegraph/ -v -run 'TestIsFresh|TestBuildBatch|TestBuildEdge'`

Expected: FAIL — functions not defined.

**Step 3: Write the implementation**

Create `internal/codegraph/index.go`:

```go
package codegraph

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

const maxFileBytes = 512 * 1024

// IndexConfig holds indexing parameters.
type IndexConfig struct {
	TTLLocal   int // TTL seconds for local repos.
	TTLRemote  int // TTL seconds for remote repos.
	BatchSize  int // Batch size for UNWIND operations.
}

// GraphMeta holds metadata about an indexed graph.
type GraphMeta struct {
	RepoKey     string    `json:"repo_key"`
	RepoPath    string    `json:"repo_path"`
	GraphName   string    `json:"graph_name"`
	FileCount   int       `json:"file_count"`
	SymbolCount int       `json:"symbol_count"`
	EdgeCount   int       `json:"edge_count"`
	BuiltAt     time.Time `json:"built_at"`
	TTLSeconds  int       `json:"ttl_seconds"`
}

// vertexData represents a vertex to insert.
type vertexData struct {
	Label string
	Props map[string]string
}

// edgeData represents an edge to insert.
type edgeData struct {
	FromLabel string
	FromKey   string
	ToLabel   string
	ToKey     string
	EdgeLabel string
	Props     map[string]string
}

// IndexRepo builds the code graph for a repository in AGE.
// It ingests, parses, extracts calls, and batch-inserts everything into the graph.
func IndexRepo(ctx context.Context, store *Store, root string, isRemote bool, cfg IndexConfig) (*GraphMeta, error) {
	repoKey := graphName(root)
	gName := repoKey

	// Check if fresh graph exists.
	meta, err := getMeta(ctx, store, repoKey)
	if err == nil && meta != nil && isFresh(meta.BuiltAt, meta.TTLSeconds) {
		slog.Info("graph fresh, skipping rebuild", slog.String("repo", root), slog.String("graph", gName))
		return meta, nil
	}

	// Drop stale graph if exists.
	if meta != nil {
		slog.Info("dropping stale graph", slog.String("graph", gName))
		_ = store.DropGraph(ctx, gName, repoKey)
	}

	// Create fresh graph.
	if err := store.EnsureGraph(ctx, gName); err != nil {
		return nil, fmt.Errorf("ensure graph: %w", err)
	}

	// Ingest files.
	ir, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         root,
		MaxFileBytes: maxFileBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("ingest: %w", err)
	}

	// Parse all files and extract calls.
	type fileResult struct {
		file    *ingest.File
		symbols []*parser.Symbol
		calls   []parser.CallSite
		imports []string
	}

	var results []fileResult
	for _, f := range ir.Files {
		source, err := readFileBytes(f.Path)
		if err != nil {
			continue
		}

		opts := parser.ParseOpts{
			Language:       f.Language,
			IncludeBody:    false, // Signatures only for graph.
			IncludeImports: true,
		}

		pr, err := parser.ParseFile(f.Path, source, opts)
		if err != nil {
			continue
		}

		calls, _ := parser.ExtractCalls(f.Path, source, opts)
		results = append(results, fileResult{
			file:    f,
			symbols: pr.Symbols,
			calls:   calls,
			imports: pr.Imports,
		})
	}

	// Collect all symbols for call graph.
	var allSymbols []*parser.Symbol
	var allCalls []parser.CallSite
	for _, r := range results {
		allSymbols = append(allSymbols, r.symbols...)
		allCalls = append(allCalls, r.calls...)
	}

	cg := callgraph.BuildCallGraph(allSymbols, allCalls)

	// Build vertices.
	var vertices []vertexData
	packages := map[string]bool{}

	for _, r := range results {
		dir := filepath.Dir(r.file.RelPath)
		if !packages[dir] {
			packages[dir] = true
			vertices = append(vertices, vertexData{
				Label: "Package",
				Props: map[string]string{"name": filepath.Base(dir), "path": dir, "repo": root},
			})
		}

		vertices = append(vertices, vertexData{
			Label: "File",
			Props: map[string]string{
				"path":     r.file.RelPath,
				"language": r.file.Language,
				"lines":    fmt.Sprintf("%d", r.file.Size/40), // rough estimate
			},
		})

		for _, sym := range r.symbols {
			vertices = append(vertices, vertexData{
				Label: "Symbol",
				Props: map[string]string{
					"name":       sym.Name,
					"kind":       string(sym.Kind),
					"signature":  sym.Signature,
					"file":       r.file.RelPath,
					"start_line": fmt.Sprintf("%d", sym.StartLine),
					"end_line":   fmt.Sprintf("%d", sym.EndLine),
				},
			})
		}
	}

	// Batch insert vertices.
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	for i := 0; i < len(vertices); i += batchSize {
		end := i + batchSize
		if end > len(vertices) {
			end = len(vertices)
		}
		cypher := buildVertexBatch(gName, vertices[i:end])
		if err := store.ExecCypherWrite(ctx, gName, cypher); err != nil {
			slog.Warn("vertex batch failed", slog.Int("offset", i), slog.Any("error", err))
		}
	}

	// Build edges.
	var edges []edgeData

	for _, r := range results {
		dir := filepath.Dir(r.file.RelPath)

		// Package → File (CONTAINS)
		edges = append(edges, edgeData{
			FromLabel: "Package", FromKey: dir,
			ToLabel: "File", ToKey: r.file.RelPath,
			EdgeLabel: "CONTAINS",
		})

		// File → Symbol (CONTAINS)
		for _, sym := range r.symbols {
			edges = append(edges, edgeData{
				FromLabel: "File", FromKey: r.file.RelPath,
				ToLabel: "Symbol", ToKey: sym.Name + ":" + r.file.RelPath,
				EdgeLabel: "CONTAINS",
			})
		}

		// File → Package (IMPORTS)
		for _, imp := range r.imports {
			edges = append(edges, edgeData{
				FromLabel: "File", FromKey: r.file.RelPath,
				ToLabel: "Package", ToKey: imp,
				EdgeLabel: "IMPORTS",
			})
		}
	}

	// CALLS edges from call graph.
	for _, e := range cg.Edges {
		if e.Callee == nil {
			continue // unresolved
		}
		callerKey := e.Caller.Name + ":" + relPath(e.Caller.File, root)
		calleeKey := e.Callee.Name + ":" + relPath(e.Callee.File, root)
		edges = append(edges, edgeData{
			FromLabel: "Symbol", FromKey: callerKey,
			ToLabel: "Symbol", ToKey: calleeKey,
			EdgeLabel: "CALLS",
			Props:     map[string]string{"line": fmt.Sprintf("%d", e.Line)},
		})
	}

	// Batch insert edges.
	for i := 0; i < len(edges); i += batchSize {
		end := i + batchSize
		if end > len(edges) {
			end = len(edges)
		}
		cypher := buildEdgeBatch(gName, edges[i:end])
		if err := store.ExecCypherWrite(ctx, gName, cypher); err != nil {
			slog.Warn("edge batch failed", slog.Int("offset", i), slog.Any("error", err))
		}
	}

	// Write metadata.
	ttl := cfg.TTLLocal
	if isRemote {
		ttl = cfg.TTLRemote
	}

	m := &GraphMeta{
		RepoKey:     repoKey,
		RepoPath:    root,
		GraphName:   gName,
		FileCount:   len(results),
		SymbolCount: len(allSymbols),
		EdgeCount:   len(edges),
		BuiltAt:     time.Now().UTC(),
		TTLSeconds:  ttl,
	}

	if err := upsertMeta(ctx, store, m); err != nil {
		slog.Warn("failed to write graph meta", slog.Any("error", err))
	}

	slog.Info("graph indexed",
		slog.String("graph", gName),
		slog.Int("files", m.FileCount),
		slog.Int("symbols", m.SymbolCount),
		slog.Int("edges", m.EdgeCount),
	)

	return m, nil
}

func getMeta(ctx context.Context, store *Store, repoKey string) (*GraphMeta, error) {
	conn, err := store.Pool().Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()

	var m GraphMeta
	err = conn.QueryRow(ctx,
		`SELECT repo_key, repo_path, graph_name, file_count, symbol_count, edge_count, built_at, ttl_seconds
		 FROM code_graph_meta WHERE repo_key = $1`, repoKey,
	).Scan(&m.RepoKey, &m.RepoPath, &m.GraphName, &m.FileCount, &m.SymbolCount, &m.EdgeCount, &m.BuiltAt, &m.TTLSeconds)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func upsertMeta(ctx context.Context, store *Store, m *GraphMeta) error {
	conn, err := store.Pool().Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	_, err = conn.Exec(ctx,
		`INSERT INTO code_graph_meta (repo_key, repo_path, graph_name, file_count, symbol_count, edge_count, built_at, ttl_seconds)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (repo_key) DO UPDATE SET
		   repo_path = EXCLUDED.repo_path, graph_name = EXCLUDED.graph_name,
		   file_count = EXCLUDED.file_count, symbol_count = EXCLUDED.symbol_count,
		   edge_count = EXCLUDED.edge_count, built_at = EXCLUDED.built_at,
		   ttl_seconds = EXCLUDED.ttl_seconds`,
		m.RepoKey, m.RepoPath, m.GraphName, m.FileCount, m.SymbolCount, m.EdgeCount, m.BuiltAt, m.TTLSeconds,
	)
	return err
}

// isFresh checks if a graph built at builtAt with the given TTL is still fresh.
func isFresh(builtAt time.Time, ttlSeconds int) bool {
	if ttlSeconds <= 0 {
		return false
	}
	return time.Since(builtAt) < time.Duration(ttlSeconds)*time.Second
}

// buildVertexBatch creates a Cypher query that batch-inserts vertices using multiple MERGE statements.
// AGE does not support UNWIND with MERGE, so we chain MERGE statements.
func buildVertexBatch(_ string, vertices []vertexData) string {
	var sb strings.Builder
	for i, v := range vertices {
		props := formatProps(v.Props)
		// Use unique key: for Symbol use name+file, for File use path, for Package use path.
		key := vertexKey(v)
		sb.WriteString(fmt.Sprintf("MERGE (v%d:%s {%s})\n", i, v.Label, key))
		if props != key {
			sb.WriteString(fmt.Sprintf("SET %s\n", formatSet(fmt.Sprintf("v%d", i), v.Props)))
		}
	}
	sb.WriteString("RETURN 1")
	return sb.String()
}

// buildEdgeBatch creates a Cypher query that batch-inserts edges.
func buildEdgeBatch(_ string, edges []edgeData) string {
	var sb strings.Builder
	for i, e := range edges {
		fromKey := matchKey(e.FromLabel, e.FromKey)
		toKey := matchKey(e.ToLabel, e.ToKey)

		sb.WriteString(fmt.Sprintf("MATCH (a%d:%s {%s}), (b%d:%s {%s})\n",
			i, e.FromLabel, fromKey, i, e.ToLabel, toKey))

		if len(e.Props) > 0 {
			sb.WriteString(fmt.Sprintf("MERGE (a%d)-[r%d:%s]->(b%d)\n", i, i, e.EdgeLabel, i))
			sb.WriteString(fmt.Sprintf("SET %s\n", formatSet(fmt.Sprintf("r%d", i), e.Props)))
		} else {
			sb.WriteString(fmt.Sprintf("MERGE (a%d)-[:%s]->(b%d)\n", i, e.EdgeLabel, i))
		}
	}
	sb.WriteString("RETURN 1")
	return sb.String()
}

func vertexKey(v vertexData) string {
	switch v.Label {
	case "Package":
		return fmt.Sprintf("path: '%s'", escapeCypher(v.Props["path"]))
	case "File":
		return fmt.Sprintf("path: '%s'", escapeCypher(v.Props["path"]))
	case "Symbol":
		return fmt.Sprintf("name: '%s', file: '%s'", escapeCypher(v.Props["name"]), escapeCypher(v.Props["file"]))
	default:
		return formatProps(v.Props)
	}
}

func matchKey(label, key string) string {
	switch label {
	case "Package":
		// Key might be a path or a name.
		if strings.Contains(key, "/") {
			return fmt.Sprintf("path: '%s'", escapeCypher(key))
		}
		return fmt.Sprintf("name: '%s'", escapeCypher(key))
	case "File":
		return fmt.Sprintf("path: '%s'", escapeCypher(key))
	case "Symbol":
		// Key is "name:file".
		parts := strings.SplitN(key, ":", 2)
		if len(parts) == 2 {
			return fmt.Sprintf("name: '%s', file: '%s'", escapeCypher(parts[0]), escapeCypher(parts[1]))
		}
		return fmt.Sprintf("name: '%s'", escapeCypher(key))
	default:
		return fmt.Sprintf("name: '%s'", escapeCypher(key))
	}
}

func formatProps(props map[string]string) string {
	var parts []string
	for k, v := range props {
		parts = append(parts, fmt.Sprintf("%s: '%s'", k, escapeCypher(v)))
	}
	return strings.Join(parts, ", ")
}

func formatSet(varName string, props map[string]string) string {
	var parts []string
	for k, v := range props {
		parts = append(parts, fmt.Sprintf("%s.%s = '%s'", varName, k, escapeCypher(v)))
	}
	return strings.Join(parts, ", ")
}

func relPath(abs, root string) string {
	if strings.HasPrefix(abs, root) {
		rel := abs[len(root):]
		if len(rel) > 0 && rel[0] == '/' {
			rel = rel[1:]
		}
		return rel
	}
	return abs
}

func readFileBytes(path string) ([]byte, error) {
	// Use os.ReadFile — imported at top of file.
	// We need to add os import.
	return nil, fmt.Errorf("not implemented")
}
```

**IMPORTANT**: Add `"os"` to the imports and fix `readFileBytes`:

```go
func readFileBytes(path string) ([]byte, error) {
	return os.ReadFile(path)
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /home/krolik/src/go-code && go test ./internal/codegraph/ -v -run 'TestIsFresh|TestBuildBatch|TestBuildEdge'`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/codegraph/index.go internal/codegraph/index_test.go
git commit -m "feat(codegraph): add IndexRepo with batch vertex/edge upsert and TTL"
```

---

## Task 7: QueryGraph — orchestrate classify → execute → format

**Files:**
- Create: `internal/codegraph/query.go`
- Modify: `internal/llm/llm.go` (add narrative prompt)

**Step 1: Add narrative system prompt to llm.go**

In `internal/llm/llm.go`, add:

```go
// SystemPromptGraphNarrative formats raw graph query results into a narrative.
const SystemPromptGraphNarrative = `You are a senior software engineer explaining code graph query results.
You receive: the original question, the Cypher query used, and the raw results.
Provide a concise narrative answer. Reference file paths and function names.
If results are empty, say so clearly. Do not speculate beyond what the data shows.`
```

**Step 2: Write the implementation**

Create `internal/codegraph/query.go`:

```go
package codegraph

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/anatolykoptev/go-code/internal/llm"
)

// QueryResult is the output of a code graph query.
type QueryResult struct {
	Repo       string     `json:"repo"`
	Query      string     `json:"query"`
	Template   string     `json:"template"`
	Cypher     string     `json:"cypher"`
	Results    [][]string `json:"results"`
	Narrative  string     `json:"narrative,omitempty"`
	GraphStats GraphStats `json:"graph_stats"`
}

// GraphStats reports graph metadata.
type GraphStats struct {
	Vertices int  `json:"vertices"`
	Edges    int  `json:"edges"`
	Cached   bool `json:"cached"`
}

// QueryGraph classifies a NL query, executes Cypher, and formats the result.
func QueryGraph(ctx context.Context, store *Store, llmClient *llm.Client, graphName, query string, meta *GraphMeta) (*QueryResult, error) {
	// Step 1: Classify.
	cls, err := Classify(ctx, llmClient, query)
	if err != nil {
		// Total failure — fallback to freeform.
		cls = &Classification{Template: "freeform", Params: map[string]string{}}
	}

	var cypher string
	var cols int

	if cls.Template != "freeform" {
		// Template path.
		tmpl := GetTemplate(cls.Template)
		if tmpl != nil {
			cypher = tmpl.Render(cls.Params)
			cols = tmpl.Cols
		} else {
			cls.Template = "freeform"
		}
	}

	if cls.Template == "freeform" {
		// Freeform path.
		generated, err := GenerateCypher(ctx, llmClient, query)
		if err != nil {
			return nil, fmt.Errorf("generate cypher: %w", err)
		}
		cypher = generated
		cols = 1 // default, may need adjustment
	}

	// Step 2: Execute.
	rows, err := store.ExecCypher(ctx, graphName, cypher, cols)
	if err != nil {
		// Retry once for freeform.
		if cls.Template == "freeform" {
			slog.Info("freeform cypher failed, retrying", slog.Any("error", err))
			retryCypher, retryErr := GenerateCypherWithRetry(ctx, llmClient, query, err)
			if retryErr != nil {
				return nil, fmt.Errorf("cypher failed after retry: %w (original: %w)", retryErr, err)
			}
			cypher = retryCypher
			rows, err = store.ExecCypher(ctx, graphName, cypher, cols)
			if err != nil {
				return nil, fmt.Errorf("cypher retry exec: %w", err)
			}
		} else {
			return nil, fmt.Errorf("cypher exec: %w", err)
		}
	}

	// Step 3: Format narrative.
	result := &QueryResult{
		Repo:     meta.RepoPath,
		Query:    query,
		Template: cls.Template,
		Cypher:   cypher,
		Results:  rows,
		GraphStats: GraphStats{
			Vertices: meta.SymbolCount + meta.FileCount,
			Edges:    meta.EdgeCount,
			Cached:   true,
		},
	}

	// LLM narrative (non-fatal).
	if llmClient != nil && len(rows) > 0 {
		rawJSON, _ := json.Marshal(rows)
		prompt := fmt.Sprintf("Question: %s\nCypher: %s\nResults:\n%s", query, cypher, string(rawJSON))
		narrative, err := llmClient.Complete(ctx, llm.SystemPromptGraphNarrative, prompt)
		if err == nil {
			result.Narrative = narrative
		}
	}

	return result, nil
}
```

**Step 3: Commit**

```bash
git add internal/codegraph/query.go internal/llm/llm.go
git commit -m "feat(codegraph): add QueryGraph orchestrator (classify → execute → format)"
```

---

## Task 8: MCP tool handler — code_graph

**Files:**
- Create: `cmd/go-code/tool_code_graph.go`
- Modify: `cmd/go-code/register.go` (add registration)
- Modify: `cmd/go-code/main.go` (update toolCount)

**Step 1: Create tool handler**

Create `cmd/go-code/tool_code_graph.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CodeGraphInput is the input schema for the code_graph tool.
type CodeGraphInput struct {
	Repo     string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path"`
	Query    string `json:"query" jsonschema_description:"Natural language question about the code graph (e.g. 'who calls ParseFile?', 'what depends on package store?', 'find dead code')"`
	Language string `json:"language,omitempty" jsonschema_description:"Limit graph to files of this language (e.g. go, python)"`
	Refresh  bool   `json:"refresh,omitempty" jsonschema_description:"Force re-indexing of the graph even if cached"`
}

// registerCodeGraph registers the code_graph MCP tool.
// Only registers if store is non-nil (DATABASE_URL configured).
func registerCodeGraph(server *mcp.Server, cfg Config, deps analyze.Deps, store *codegraph.Store) {
	if store == nil {
		slog.Info("code_graph: DATABASE_URL not set, tool disabled")
		return
	}

	mcp.AddTool(server, &mcp.Tool{
		Name: "code_graph",
		Description: "Query a persistent code knowledge graph for a repository. " +
			"Automatically indexes the repo on first call (lazy). " +
			"Answers natural-language questions like 'who calls X?', 'what imports Y?', " +
			"'find dead code', 'path between A and B'. " +
			"Uses Apache AGE (Cypher) with 10 built-in query templates + LLM freeform fallback.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodeGraphInput) (*mcp.CallToolResult, any, error) {
		if input.Repo == "" {
			return errResult("repo is required"), nil, nil
		}
		if input.Query == "" {
			return errResult("query is required"), nil, nil
		}

		if !store.HasAGE(ctx) {
			return errResult("Apache AGE extension not available in PostgreSQL"), nil, nil
		}

		root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
		if err != nil {
			return errResult(fmt.Sprintf("resolve repo: %s", err)), nil, nil
		}
		defer cleanup()

		isRemote := ingest.IsRemote(input.Repo)

		// Force refresh if requested.
		if input.Refresh {
			key := codegraph.GraphNameFor(root)
			_ = store.DropGraph(ctx, key, key)
		}

		// Index (lazy — skips if fresh).
		meta, err := codegraph.IndexRepo(ctx, store, root, isRemote, codegraph.IndexConfig{
			TTLLocal:  cfg.GraphTTLLocal,
			TTLRemote: cfg.GraphTTLRemote,
			BatchSize: cfg.GraphBatchSize,
		})
		if err != nil {
			return errResult(fmt.Sprintf("index: %s", err)), nil, nil
		}

		// Query.
		result, err := codegraph.QueryGraph(ctx, store, deps.LLM, meta.GraphName, input.Query, meta)
		if err != nil {
			return errResult(fmt.Sprintf("query: %s", err)), nil, nil
		}

		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return errResult(fmt.Sprintf("marshal: %s", err)), nil, nil
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
		}, nil, nil
	})
}
```

**Step 2: Add `GraphNameFor` export to store.go**

In `internal/codegraph/store.go`, add:

```go
// GraphNameFor returns the deterministic graph name for a repo path.
func GraphNameFor(repoPath string) string {
	return graphName(repoPath)
}
```

**Step 3: Modify register.go — add pgxpool creation and tool registration**

In `cmd/go-code/register.go`, add imports for `codegraph` and `pgxpool`, then after the existing 6 `register*` calls:

```go
// Code graph (optional — needs DATABASE_URL).
var graphStore *codegraph.Store
if cfg.DatabaseURL != "" {
    pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
    if err != nil {
        slog.Warn("code_graph: failed to connect to database, tool disabled",
            slog.Any("error", err))
    } else {
        graphStore = codegraph.NewStore(pool)
    }
}
registerCodeGraph(server, cfg, deps, graphStore)
```

Add imports:
```go
"context"
"log/slog"

"github.com/anatolykoptev/go-code/internal/codegraph"
"github.com/jackc/pgx/v5/pgxpool"
```

**Step 4: Update toolCount in main.go**

Change `toolCount = 6` to `toolCount = 7` in `cmd/go-code/main.go`.

Also update the package doc comment to include `code_graph`:
```go
// Tools: repo_analyze, file_parse, code_compare, dep_graph, symbol_search, call_trace, code_graph
```

**Step 5: Verify it compiles**

Run: `cd /home/krolik/src/go-code && go build ./cmd/go-code/`

Expected: clean build.

**Step 6: Commit**

```bash
git add cmd/go-code/tool_code_graph.go cmd/go-code/register.go cmd/go-code/main.go internal/codegraph/store.go
git commit -m "feat: add code_graph MCP tool with lazy indexing and NL→Cypher queries"
```

---

## Task 9: Docker + deploy

**Files:**
- Modify: `~/deploy/krolik-server/docker-compose.yml` (add DATABASE_URL to go-code)

**Step 1: Add DATABASE_URL env var to go-code service in docker-compose.yml**

Find the `go-code` service `environment:` section and add:

```yaml
- DATABASE_URL=postgresql://${POSTGRES_USER:-memos}:${POSTGRES_PASSWORD}@postgres:5432/${POSTGRES_DB:-memos}
```

**Step 2: Build and deploy**

Run:
```bash
cd ~/deploy/krolik-server && docker compose build --no-cache go-code && docker compose up -d --no-deps --force-recreate go-code
```

**Step 3: Verify**

Run: `docker logs go-code --tail 5`

Expected: `tools registered count=7` and `AGE availability available=true`.

**Step 4: Commit docker-compose change**

```bash
cd ~/deploy/krolik-server && git add docker-compose.yml
git commit -m "feat: add DATABASE_URL to go-code for code_graph tool"
```

---

## Task 10: Smoke test with real repo

**No files to modify — manual testing.**

**Step 1: Test indexing**

Call `code_graph` MCP tool:
```
repo: "/home/krolik/src/go-code"
query: "who calls ParseFile?"
```

Expected: Graph gets indexed, returns list of callers with narrative.

**Step 2: Test template queries**

Try various NL queries:
- "what does AnalyzeRepo call?" (→ `calls_of`)
- "find dead code" (→ `dead_code`)
- "what imports internal/parser?" (→ `importers_of`)

**Step 3: Test freeform fallback**

Try a non-template question:
- "which files have the most symbols?" (→ freeform Cypher)

**Step 4: Test caching**

Call the same query again — should return instantly with `cached: true`.

**Step 5: Test refresh**

Call with `refresh: true` — should re-index.

---

## Task 11: Update docs and ROADMAP

**Files:**
- Modify: `docs/ROADMAP.md` (mark 4.2 complete)
- Modify: `CLAUDE.md` (add code_graph to tool table)

**Step 1: Update ROADMAP.md**

Mark all 4.2 items as `[x]`, add status line, add release tag.

**Step 2: Update CLAUDE.md**

Add `code_graph` to the MCP Tools table:

```
| `code_graph` | Query a persistent code knowledge graph. NL questions about callers, dependencies, dead code. Apache AGE + Cypher. |
```

**Step 3: Commit**

```bash
cd ~/src/go-code && git add docs/ROADMAP.md CLAUDE.md
git commit -m "docs: mark Phase 4.2 complete, update tool table"
```

**Step 4: Tag release**

```bash
git tag v1.7.0
```
