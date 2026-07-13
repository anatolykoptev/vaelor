package codegraph

import (
	"fmt"
	"sort"
	"strings"
)

// Template is a named Cypher query template with parameter substitution.
type Template struct {
	ID          string
	Description string
	Params      []string
	Cypher      string
	Cols        int
}

// Render substitutes {param} placeholders with escaped values from params.
// Uses curly-brace syntax to avoid conflicts with AGE's $-parameter references.
func (t *Template) Render(params map[string]string) string {
	q := t.Cypher
	for _, key := range t.Params {
		v, ok := params[key]
		if !ok || v == "" {
			v = templateDefaults[key]
		}
		q = strings.ReplaceAll(q, "{"+key+"}", escapeCypher(v))
	}
	return q
}

// templateDefaults provides fallback values for unspecified template parameters.
var templateDefaults = map[string]string{
	"limit": "20",
	"name":  "",
	"path":  "",
	"pkg":   "",
	"from":  "",
	"to":    "",
}

// templates holds all built-in query templates keyed by ID.
var templates = map[string]*Template{
	"who_calls": {
		ID:          "who_calls",
		Description: "Find all symbols that call the named symbol",
		Params:      []string{"name"},
		Cypher:      "MATCH (caller:Symbol)-[:CALLS]->(target:Symbol {name: '{name}'}) RETURN DISTINCT caller",
		Cols:        1,
	},
	"calls_of": {
		ID:          "calls_of",
		Description: "Find all symbols called by the named symbol",
		Params:      []string{"name"},
		Cypher:      "MATCH (src:Symbol {name: '{name}'})-[:CALLS]->(callee:Symbol) RETURN DISTINCT callee",
		Cols:        1,
	},
	"imports_of": {
		ID:          "imports_of",
		Description: "Find packages imported by files matching a path",
		Params:      []string{"path"},
		Cypher:      "MATCH (f:File)-[:IMPORTS]->(p:Package) WHERE f.path CONTAINS '{path}' RETURN p",
		Cols:        1,
	},
	"importers_of": {
		ID:          "importers_of",
		Description: "Find files that import the named package",
		Params:      []string{"name"},
		Cypher:      "MATCH (f:File)-[:IMPORTS]->(p:Package {name: '{name}'}) RETURN f",
		Cols:        1,
	},
	"symbols_in": {
		ID:          "symbols_in",
		Description: "Find symbols contained in files matching a path",
		Params:      []string{"path"},
		Cypher:      "MATCH (c)-[:CONTAINS]->(s:Symbol) WHERE c.path CONTAINS '{path}' RETURN DISTINCT s",
		Cols:        1,
	},
	"call_chain": {
		ID:          "call_chain",
		Description: "Find a call path between two symbols",
		Params:      []string{"from", "to"},
		Cypher:      "MATCH (a:Symbol {name: '{from}'})-[:CALLS*1..10]->(b:Symbol {name: '{to}'}) RETURN a, b",
		Cols:        2,
	},
	"most_connected": {
		ID:          "most_connected",
		Description: "List the most-called symbols up to a limit",
		Params:      []string{"limit"},
		Cypher:      "MATCH (s:Symbol)<-[:CALLS]-(caller:Symbol) RETURN s.name, s.kind, s.file, count(caller) AS call_count ORDER BY call_count DESC LIMIT {limit}",
		Cols:        4,
	},
	"dead_code": {
		ID:          "dead_code",
		Description: "Find functions that are never called",
		Params:      []string{},
		Cypher:      "MATCH (s:Symbol) WHERE s.kind = 'function' OPTIONAL MATCH (caller:Symbol)-[:CALLS]->(s) WITH s, caller WHERE caller IS NULL RETURN s LIMIT 100",
		Cols:        1,
	},
	"depends_on": {
		ID:          "depends_on",
		Description: "Find distinct packages depended on by files matching a path prefix",
		Params:      []string{"pkg"},
		Cypher:      "MATCH (f:File)-[:IMPORTS]->(p:Package) WHERE f.path CONTAINS '{pkg}' RETURN DISTINCT p",
		Cols:        1,
	},
	"dependents_of": {
		ID:          "dependents_of",
		Description: "Find distinct files that depend on the named package",
		Params:      []string{"name"},
		Cypher:      "MATCH (f:File)-[:IMPORTS]->(p:Package {name: '{name}'}) RETURN DISTINCT f",
		Cols:        1,
	},
	"api_routes": {
		ID:          "api_routes",
		Description: "Find HTTP routes with their handler symbols, optionally filtered by path",
		Params:      []string{"path"},
		Cypher:      "MATCH (s:Symbol)-[r]->(route:Route) WHERE route.path CONTAINS '{path}' RETURN s.name, s.file, type(r) AS relation, route.method, route.path",
		Cols:        5,
	},
	"cross_calls": {
		ID:          "cross_calls",
		Description: "Find backend handlers and frontend callers connected through shared HTTP routes",
		Params:      []string{"path"},
		Cypher:      "MATCH (server:Symbol)-[:HANDLES]->(route:Route)<-[:FETCHES]-(client:Symbol) WHERE route.path CONTAINS '{path}' RETURN server.name, server.file, route.method, route.path, client.name, client.file",
		Cols:        6,
	},
	"layer_deps": {
		ID:          "layer_deps",
		Description: "Show dependencies between architectural layers via function calls",
		Params:      []string{},
		Cypher:      "MATCH (f1:File)-[:BELONGS_TO]->(l1:Layer), (f2:File)-[:BELONGS_TO]->(l2:Layer), (s1:Symbol)<-[:CONTAINS]-(f1), (s1)-[:CALLS]->(s2), (s2)<-[:CONTAINS]-(f2) WHERE l1.name <> l2.name RETURN l1.name, l2.name, count(*) AS connections ORDER BY connections DESC",
		Cols:        3,
	},
	"polyglot_overview": {
		ID:          "polyglot_overview",
		Description: "Show repository structure with layers, languages, and route counts",
		Params:      []string{},
		Cypher:      "MATCH (l:Layer)<-[:BELONGS_TO]-(f:File) OPTIONAL MATCH (f)-[:CONTAINS]->(s:Symbol)-[:HANDLES]->(r:Route) RETURN l.name, l.role, l.language, count(DISTINCT f) AS files, count(DISTINCT r) AS routes",
		Cols:        5,
	},
	"complex_symbols": {
		ID:          "complex_symbols",
		Description: "Find functions with highest cyclomatic complexity",
		Params:      []string{"limit"},
		Cypher:      "MATCH (s:Symbol) WHERE s.kind IN ['function', 'method'] AND s.complexity IS NOT NULL RETURN s.name, s.file, s.complexity, s.lines ORDER BY s.complexity DESC LIMIT {limit}",
		Cols:        4,
	},
	"hotspots": {
		ID:          "hotspots",
		Description: "Find hotspot functions — high complexity combined with high line count",
		Params:      []string{"limit"},
		Cypher:      "MATCH (s:Symbol) WHERE s.kind IN ['function', 'method'] AND s.complexity IS NOT NULL AND s.lines IS NOT NULL RETURN s.name, s.file, s.complexity, s.lines ORDER BY s.complexity DESC, s.lines DESC LIMIT {limit}",
		Cols:        4,
	},
	"inherits": {
		ID:          "inherits",
		Description: "Find what a type inherits from or implements (embeds, extends, implements)",
		Params:      []string{"name"},
		Cypher:      "MATCH (child:Symbol {name: '{name}'})-[r]->(parent:Symbol) WHERE type(r) = 'INHERITS' OR type(r) = 'IMPLEMENTS' RETURN parent.name, parent.file, type(r) AS relation",
		Cols:        3,
	},
	"implementations": {
		ID:          "implementations",
		Description: "Find all types that inherit from or implement the named type",
		Params:      []string{"name"},
		Cypher:      "MATCH (child:Symbol)-[r]->(parent:Symbol {name: '{name}'}) WHERE type(r) = 'INHERITS' OR type(r) = 'IMPLEMENTS' RETURN child.name, child.file, type(r) AS relation",
		Cols:        3,
	},
	"type_hierarchy": {
		ID:          "type_hierarchy",
		Description: "Show the full type hierarchy (parents and children) for a named type",
		Params:      []string{"name"},
		Cypher:      "MATCH (s:Symbol {name: '{name}'}) OPTIONAL MATCH (s)-[:INHERITS]->(parent:Symbol) OPTIONAL MATCH (child:Symbol)-[:INHERITS]->(s) RETURN s, parent, child",
		Cols:        3,
	},
	"subtypes": {
		ID:          "subtypes",
		Description: "Find all transitive subtypes of the named type (up to 5 levels deep)",
		Params:      []string{"name"},
		Cypher:      "MATCH (child:Symbol)-[:INHERITS*1..5]->(ancestor:Symbol {name: '{name}'}) RETURN child",
		Cols:        1,
	},
	"important_symbols": {
		ID:          "important_symbols",
		Description: "Most structurally central symbols by PageRank — the load-bearing code you must understand first before diving into any feature area",
		Params:      []string{"limit"},
		Cypher:      "MATCH (s:Symbol) WHERE s.pagerank IS NOT NULL RETURN s.name, s.file, s.kind, s.pagerank ORDER BY s.pagerank DESC LIMIT {limit}",
		Cols:        4,
	},
	"explain_architecture": {
		ID:          "explain_architecture",
		Description: "Top architecturally important symbols with their files and structural communities — the essential map for understanding any codebase",
		Params:      []string{"limit"},
		Cypher:      "MATCH (s:Symbol) WHERE s.pagerank IS NOT NULL AND s.kind IN ['function', 'method'] WITH s ORDER BY toFloat(s.pagerank) DESC LIMIT {limit} RETURN s.name, s.file, s.kind, s.pagerank, s.community",
		Cols:        5,
	},
	"hotspot_files": {
		ID:          "hotspot_files",
		Description: "Files containing the most architecturally important symbols — the structural hotspots of the codebase where changes carry highest risk",
		Params:      []string{"limit"},
		Cypher:      "MATCH (f:File)-[:CONTAINS]->(s:Symbol) WHERE s.pagerank IS NOT NULL WITH f.path AS fpath, max(toFloat(s.pagerank)) AS maxPR, count(s) AS symCount RETURN fpath, maxPR, symCount ORDER BY maxPR DESC LIMIT {limit}",
		Cols:        3,
	},
	"hook_handlers": {
		ID:          "hook_handlers",
		Description: "Find all callback functions registered for a WordPress hook",
		Params:      []string{"name"},
		Cypher:      "MATCH (s:Symbol)-[:HANDLES]->(r:Route {framework: 'wordpress', path: '{name}', side: 'server'}) RETURN s.name, s.file, s.kind, r.method",
		Cols:        4,
	},
	"hook_fires": {
		ID:          "hook_fires",
		Description: "Find all functions that fire (invoke) a WordPress hook",
		Params:      []string{"name"},
		Cypher:      "MATCH (s:Symbol)-[:FETCHES]->(r:Route {framework: 'wordpress', path: '{name}', side: 'client'}) RETURN s.name, s.file",
		Cols:        2,
	},
	"all_hooks": {
		ID:          "all_hooks",
		Description: "List all WordPress hooks found in the codebase",
		Params:      []string{},
		Cypher:      "MATCH (r:Route {framework: 'wordpress'}) RETURN r.method, r.path, r.side ORDER BY r.path",
		Cols:        3,
	},
}

// GetTemplate returns the template with the given ID, or nil if not found.
func GetTemplate(id string) *Template {
	return templates[id]
}

// TemplateList returns a formatted list of all templates suitable for a classifier prompt.
// Templates are sorted alphabetically by ID for deterministic prompt generation.
func TemplateList() string {
	ids := make([]string, 0, len(templates))
	for id := range templates {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var sb strings.Builder
	for _, id := range ids {
		t := templates[id]
		params := strings.Join(t.Params, ", ")
		if params == "" {
			params = "(none)"
		}
		fmt.Fprintf(&sb, "- %s: %s [params: %s]\n", t.ID, t.Description, params)
	}
	return sb.String()
}
