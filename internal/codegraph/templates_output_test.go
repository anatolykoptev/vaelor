package codegraph

import (
	"regexp"
	"strings"
	"testing"
)

// vertexConsumers return whole vertices on purpose: their rows are parsed as
// vertex JSON downstream (RerankDeadCode / PersistInsights), or they are a
// placeholder replaced by post-processing (graph_diff).
var vertexConsumers = map[string]bool{"dead_code": true, "graph_diff": true}

// aggregateOnly templates return one row per layer, bounded by the layer count.
var aggregateOnly = map[string]bool{"layer_deps": true, "polyglot_overview": true}

// TestTemplatesBoundOutput: an unbounded template on a common name (who_calls
// Close) produced 200k chars, which the tool spilled to a server-side file the
// MCP client cannot read — the caller saw "0 rows". Every template must cap
// its rows. Derived from the registry, so a new template is checked too.
func TestTemplatesBoundOutput(t *testing.T) {
	for id, tm := range templates {
		if aggregateOnly[id] {
			continue
		}
		if !strings.Contains(tm.Cypher, "LIMIT ") {
			t.Errorf("template %s has no LIMIT: %s", id, tm.Cypher)
		}
	}
}

var (
	reNodeVar   = regexp.MustCompile(`\((\w+)(?::\w+)?[\s)\{]`)
	reUnwindVar = regexp.MustCompile(`(?i)\bUNWIND\s+nodes\(\w+\)\s+AS\s+(\w+)`)
	reReturnArg = regexp.MustCompile(`(?is)\bRETURN\b\s+(?:DISTINCT\s+)?(.+?)(?:\bORDER\b|\bLIMIT\b|$)`)
)

// TestTemplatesReturnColumnsNotVertices: returning a whole vertex serializes
// every property (signature, pagerank, community …) — ~700 chars a row, which
// is what pushed common-name queries past the inline output limit.
func TestTemplatesReturnColumnsNotVertices(t *testing.T) {
	for id, tm := range templates {
		if vertexConsumers[id] {
			continue
		}
		nodes := map[string]bool{}
		for _, re := range []*regexp.Regexp{reNodeVar, reUnwindVar} {
			for _, m := range re.FindAllStringSubmatch(tm.Cypher, -1) {
				nodes[m[1]] = true
			}
		}
		ret := reReturnArg.FindStringSubmatch(tm.Cypher)
		if ret == nil {
			t.Errorf("template %s: no RETURN clause", id)
			continue
		}
		for _, item := range strings.Split(ret[1], ",") {
			item = strings.TrimSpace(item)
			if nodes[item] {
				t.Errorf("template %s returns whole vertex %q; return its properties", id, item)
			}
		}
	}
}

func TestTemplateLimitDefaults(t *testing.T) {
	cases := []struct {
		id, limit, want string
	}{
		{"who_calls", "", "LIMIT 100"},    // list-shaped: template default
		{"who_calls", "abc", "LIMIT 100"}, // invalid falls back to the template default
		{"who_calls", "7", "LIMIT 7"},
		{"complex_symbols", "", "LIMIT 20"}, // ranking: global default
		{"complex_symbols", "0", "LIMIT 20"},
	}
	for _, tc := range cases {
		got := GetTemplate(tc.id).Render(map[string]string{"name": "X", "limit": tc.limit})
		if !strings.Contains(got, tc.want) {
			t.Errorf("%s limit=%q: rendered %q, want %q", tc.id, tc.limit, got, tc.want)
		}
	}
	if got := GetTemplate("who_calls").Render(map[string]string{"name": "Close"}); !strings.Contains(got, "target.file CONTAINS ''") {
		t.Errorf("omitted file must not filter: %s", got)
	}
	if got := GetTemplate("who_calls").Render(map[string]string{"name": "Close", "file": "internal/a"}); !strings.Contains(got, "target.file CONTAINS 'internal/a'") {
		t.Errorf("file filter not rendered: %s", got)
	}
}
