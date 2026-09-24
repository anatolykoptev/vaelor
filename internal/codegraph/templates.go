package codegraph

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Template is a named Cypher query template with parameter substitution.
type Template struct {
	ID          string
	Description string
	Params      []string
	// Optional lists the Params an explicit call may omit. Every other param
	// except limit is required (see ExplicitClassification).
	Optional []string
	// Defaults overrides templateDefaults for this template (e.g. a larger
	// limit for list-shaped results than for top-N rankings).
	Defaults map[string]string
	Cypher   string
	Cols     int
}

// Render substitutes {param} placeholders with escaped values from params.
// Uses curly-brace syntax to avoid conflicts with AGE's $-parameter references.
func (t *Template) Render(params map[string]string) string {
	q, _ := t.render(params, 0)
	return q
}

// RenderProbe renders like Render but asks for one row past the effective
// {limit}, so the caller can tell a complete answer from a truncated one.
// limit is that effective limit, or 0 for a template without {limit}.
func (t *Template) RenderProbe(params map[string]string) (q string, limit int) {
	return t.render(params, 1)
}

func (t *Template) render(params map[string]string, extra int) (string, int) {
	q, limit := t.Cypher, 0
	for _, key := range t.Params {
		v, ok := params[key]
		if !ok || v == "" {
			v = t.defaultFor(key)
		}
		if key == paramLimit {
			v = sanitizeLimit(v, t.defaultFor(paramLimit))
			limit, _ = strconv.Atoi(v)
			v = strconv.Itoa(limit + extra)
		}
		q = strings.ReplaceAll(q, "{"+key+"}", escapeCypher(v))
	}
	return q, limit
}

// defaultFor returns the template's own default for key, else the global one.
func (t *Template) defaultFor(key string) string {
	if v, ok := t.Defaults[key]; ok {
		return v
	}
	return templateDefaults[key]
}

// templateDefaults provides fallback values for unspecified template parameters.
var templateDefaults = map[string]string{
	paramLimit: "20",
	paramName:  "",
	paramPath:  "",
	paramPkg:   "",
	paramFrom:  "",
	paramTo:    "",
	paramFile:  "",
}

// importersCypher is shared by importers_of and dependents_of: files importing
// a package named by its Go name, full path, last path segment (pgx/v5's name
// is "v5", so "pgx" needs the path), or as a parent of subpackages. Exact
// matches sort first so a LIMIT never drops them for subpackage rows. A
// major-version module path (pgx/v5 asked for as "pgx") is labelled
// "subpackage" — the row's package path column says which module it is.
const importersCypher = "MATCH (f:File)-[:IMPORTS]->(p:Package) " +
	"WHERE p.name = '{name}' OR p.path = '{name}' OR p.path ENDS WITH '/{name}' " +
	"OR p.path STARTS WITH '{name}/' OR p.path CONTAINS '/{name}/' " +
	"WITH f, p, CASE WHEN p.name = '{name}' OR p.path = '{name}' OR p.path ENDS WITH '/{name}' " +
	"THEN 'exact' ELSE 'subpackage' END AS relation " +
	"RETURN DISTINCT f.path, p.path, relation ORDER BY relation, f.path LIMIT {limit}"

// templates holds all built-in query templates keyed by ID.
var templates = map[string]*Template{
	"who_calls": {
		ID:          "who_calls",
		Description: "Find all symbols that call the named symbol",
		Params:      []string{paramName, paramFile, paramLimit},
		Optional:    []string{paramFile},
		Defaults:    map[string]string{paramLimit: "100"},
		Cypher:      "MATCH (caller:Symbol)-[:CALLS]->(target:Symbol {name: '{name}'}) WHERE target.file CONTAINS '{file}' RETURN DISTINCT caller.name, caller.kind, caller.file, toInteger(caller.start_line) AS line, target.file ORDER BY caller.file, line LIMIT {limit}",
		Cols:        5,
	},
	"calls_of": {
		ID:          "calls_of",
		Description: "Find all symbols called by the named symbol",
		Params:      []string{paramName, paramFile, paramLimit},
		Optional:    []string{paramFile},
		Defaults:    map[string]string{paramLimit: "100"},
		Cypher:      "MATCH (src:Symbol {name: '{name}'})-[:CALLS]->(callee:Symbol) WHERE src.file CONTAINS '{file}' RETURN DISTINCT callee.name, callee.kind, callee.file, toInteger(callee.start_line) AS line, src.file ORDER BY callee.file, line LIMIT {limit}",
		Cols:        5,
	},
	"imports_of": {
		ID:          "imports_of",
		Description: "Find packages imported by files matching a path",
		Params:      []string{paramPath, paramLimit},
		Defaults:    map[string]string{paramLimit: "300"},
		Cypher:      "MATCH (f:File)-[:IMPORTS]->(p:Package) WHERE f.path CONTAINS '{path}' RETURN DISTINCT p.path, p.repo ORDER BY p.path LIMIT {limit}",
		Cols:        2,
	},
	"importers_of": {
		ID:          "importers_of",
		Description: "Find files that import the named package or its subpackages; relation says exact or subpackage",
		Params:      []string{paramName, paramLimit},
		Defaults:    map[string]string{paramLimit: "100"},
		Cypher:      importersCypher,
		Cols:        3,
	},
	"symbols_in": {
		ID:          "symbols_in",
		Description: "Find symbols contained in files matching a path",
		Params:      []string{paramPath, paramLimit},
		Defaults:    map[string]string{paramLimit: "100"},
		Cypher:      "MATCH (c)-[:CONTAINS]->(s:Symbol) WHERE c.path CONTAINS '{path}' RETURN DISTINCT s.name, s.kind, s.file, toInteger(s.start_line) AS line ORDER BY s.file, line LIMIT {limit}",
		Cols:        4,
	},
	"call_chain": {
		ID:          "call_chain",
		Description: "Find a call path between two symbols",
		Params:      []string{paramFrom, paramTo},
		Cypher:      "MATCH p = (a:Symbol {name: '{from}'})-[:CALLS*1..10]->(b:Symbol {name: '{to}'}) WITH p ORDER BY length(p) LIMIT 1 UNWIND nodes(p) AS n RETURN n.name, n.file",
		Cols:        2,
	},
	"most_connected": {
		ID:          "most_connected",
		Description: "List the most-called symbols up to a limit",
		Params:      []string{paramLimit},
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
		Params:      []string{paramPkg, paramLimit},
		Defaults:    map[string]string{paramLimit: "300"},
		Cypher:      "MATCH (f:File)-[:IMPORTS]->(p:Package) WHERE f.path CONTAINS '{pkg}' RETURN DISTINCT p.path, p.repo ORDER BY p.path LIMIT {limit}",
		Cols:        2,
	},
	"dependents_of": {
		ID:          "dependents_of",
		Description: "Find files that depend on the named package or its subpackages; relation says exact or subpackage",
		Params:      []string{paramName, paramLimit},
		Defaults:    map[string]string{paramLimit: "100"},
		Cypher:      importersCypher,
		Cols:        3,
	},
	"api_routes": {
		ID:          "api_routes",
		Description: "Find HTTP routes with their handler symbols, optionally filtered by path",
		Params:      []string{paramPath, paramLimit},
		Optional:    []string{paramPath},
		Defaults:    map[string]string{paramLimit: "100"},
		Cypher:      "MATCH (s:Symbol)-[r]->(route:Route) WHERE route.path CONTAINS '{path}' RETURN s.name, s.file, type(r) AS relation, route.method, route.path ORDER BY route.path LIMIT {limit}",
		Cols:        5,
	},
	"cross_calls": {
		ID:          "cross_calls",
		Description: "Find backend handlers and frontend callers connected through shared HTTP routes",
		Params:      []string{paramPath, paramLimit},
		Optional:    []string{paramPath},
		Defaults:    map[string]string{paramLimit: "100"},
		Cypher:      "MATCH (server:Symbol)-[:HANDLES]->(route:Route)<-[:FETCHES]-(client:Symbol) WHERE route.path CONTAINS '{path}' RETURN server.name, server.file, route.method, route.path, client.name, client.file ORDER BY route.path LIMIT {limit}",
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
		Params:      []string{paramLimit},
		Cypher:      "MATCH (s:Symbol) WHERE s.kind IN ['function', 'method'] AND s.complexity IS NOT NULL RETURN s.name, s.file, toInteger(s.complexity) AS complexity, toInteger(s.lines) AS lines ORDER BY complexity DESC, lines DESC LIMIT {limit}",
		Cols:        4,
	},
	"hotspots": {
		ID:          "hotspots",
		Description: "Find hotspot functions — high complexity combined with high line count",
		Params:      []string{paramLimit},
		Cypher:      "MATCH (s:Symbol) WHERE s.kind IN ['function', 'method'] AND s.complexity IS NOT NULL AND s.lines IS NOT NULL WITH s, toInteger(s.complexity) AS complexity, toInteger(s.lines) AS lines RETURN s.name, s.file, complexity, lines ORDER BY complexity * lines DESC LIMIT {limit}",
		Cols:        4,
	},
	"inherits": {
		ID:          "inherits",
		Description: "Find what a type inherits from or implements (embeds, extends, implements)",
		Params:      []string{paramName, paramFile, paramLimit},
		Optional:    []string{paramFile},
		Defaults:    map[string]string{paramLimit: "100"},
		Cypher:      "MATCH (child:Symbol {name: '{name}'})-[r]->(parent:Symbol) WHERE (type(r) = 'INHERITS' OR type(r) = 'IMPLEMENTS') AND child.file CONTAINS '{file}' RETURN parent.name, parent.file, type(r) AS relation ORDER BY parent.file, parent.name LIMIT {limit}",
		Cols:        3,
	},
	"implementations": {
		ID:          "implementations",
		Description: "Find all types that inherit from or implement the named type",
		Params:      []string{paramName, paramFile, paramLimit},
		Optional:    []string{paramFile},
		Defaults:    map[string]string{paramLimit: "100"},
		Cypher:      "MATCH (child:Symbol)-[r]->(parent:Symbol {name: '{name}'}) WHERE (type(r) = 'INHERITS' OR type(r) = 'IMPLEMENTS') AND parent.file CONTAINS '{file}' RETURN child.name, child.file, type(r) AS relation, parent.file ORDER BY child.file, child.name LIMIT {limit}",
		Cols:        4,
	},
	"type_hierarchy": {
		ID:          "type_hierarchy",
		Description: "Show the full type hierarchy (parents and children) for a named type",
		Params:      []string{paramName, paramFile, paramLimit},
		Optional:    []string{paramFile},
		Defaults:    map[string]string{paramLimit: "100"},
		Cypher:      "MATCH (s:Symbol {name: '{name}'}) WHERE s.file CONTAINS '{file}' OPTIONAL MATCH (s)-[:INHERITS]->(parent:Symbol) OPTIONAL MATCH (child:Symbol)-[:INHERITS]->(s) RETURN s.name, s.file, parent.name, child.name ORDER BY s.file, parent.name, child.name LIMIT {limit}",
		Cols:        4,
	},
	"subtypes": {
		ID:          "subtypes",
		Description: "Find all transitive subtypes of the named type (up to 5 levels deep)",
		Params:      []string{paramName, paramFile, paramLimit},
		Optional:    []string{paramFile},
		Defaults:    map[string]string{paramLimit: "100"},
		Cypher:      "MATCH (child:Symbol)-[:INHERITS*1..5]->(ancestor:Symbol {name: '{name}'}) WHERE ancestor.file CONTAINS '{file}' RETURN DISTINCT child.name, child.file ORDER BY child.file LIMIT {limit}",
		Cols:        2,
	},
	"important_symbols": {
		ID:          "important_symbols",
		Description: "Most structurally central symbols by PageRank — the load-bearing code you must understand first before diving into any feature area",
		Params:      []string{paramLimit},
		Cypher:      "MATCH (s:Symbol) WHERE s.pagerank IS NOT NULL RETURN s.name, s.file, s.kind, toFloat(s.pagerank) AS pagerank ORDER BY pagerank DESC LIMIT {limit}",
		Cols:        4,
	},
	"explain_architecture": {
		ID:          "explain_architecture",
		Description: "Top architecturally important symbols with their files and structural communities — the essential map for understanding any codebase",
		Params:      []string{paramLimit},
		Cypher:      "MATCH (s:Symbol) WHERE s.pagerank IS NOT NULL AND s.kind IN ['function', 'method'] WITH s ORDER BY toFloat(s.pagerank) DESC LIMIT {limit} RETURN s.name, s.file, s.kind, toFloat(s.pagerank), s.community",
		Cols:        5,
	},
	"hotspot_files": {
		ID:          "hotspot_files",
		Description: "Files containing the most architecturally important symbols — the structural hotspots of the codebase where changes carry highest risk",
		Params:      []string{paramLimit},
		Cypher:      "MATCH (f:File)-[:CONTAINS]->(s:Symbol) WHERE s.pagerank IS NOT NULL WITH f.path AS fpath, max(toFloat(s.pagerank)) AS maxPR, count(s) AS symCount RETURN fpath, maxPR, symCount ORDER BY maxPR DESC LIMIT {limit}",
		Cols:        3,
	},
	"hook_handlers": {
		ID:          "hook_handlers",
		Description: "Find all callback functions registered for a WordPress hook",
		Params:      []string{paramName, paramLimit},
		Defaults:    map[string]string{paramLimit: "100"},
		Cypher:      "MATCH (s:Symbol)-[:HANDLES]->(r:Route {framework: 'wordpress', path: '{name}', side: 'server'}) RETURN s.name, s.file, s.kind, r.method LIMIT {limit}",
		Cols:        4,
	},
	"hook_fires": {
		ID:          "hook_fires",
		Description: "Find all functions that fire (invoke) a WordPress hook",
		Params:      []string{paramName, paramLimit},
		Defaults:    map[string]string{paramLimit: "100"},
		Cypher:      "MATCH (s:Symbol)-[:FETCHES]->(r:Route {framework: 'wordpress', path: '{name}', side: 'client'}) RETURN s.name, s.file LIMIT {limit}",
		Cols:        2,
	},
	"all_hooks": {
		ID:          "all_hooks",
		Description: "List all WordPress hooks found in the codebase",
		Params:      []string{paramLimit},
		Defaults:    map[string]string{paramLimit: "100"},
		Cypher:      "MATCH (r:Route {framework: 'wordpress'}) RETURN r.method, r.path, r.side ORDER BY r.path LIMIT {limit}",
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
		params := strings.Trim(signature(t), "()")
		if params == "" {
			params = "(none)"
		}
		fmt.Fprintf(&sb, "- %s: %s [params: %s]\n", t.ID, t.Description, params)
	}
	return sb.String()
}
