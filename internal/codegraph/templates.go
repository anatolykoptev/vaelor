package codegraph

import (
	"fmt"
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

// Render substitutes $param placeholders with escaped values from params.
func (t *Template) Render(params map[string]string) string {
	q := t.Cypher
	for k, v := range params {
		q = strings.ReplaceAll(q, "$"+k, escapeCypher(v))
	}
	return q
}

// templates holds all built-in query templates keyed by ID.
var templates = map[string]*Template{
	"who_calls": {
		ID:          "who_calls",
		Description: "Find all symbols that call the named symbol",
		Params:      []string{"name"},
		Cypher:      "MATCH (caller:Symbol)-[:CALLS]->(target:Symbol {name: '$name'}) RETURN caller",
		Cols:        1,
	},
	"calls_of": {
		ID:          "calls_of",
		Description: "Find all symbols called by the named symbol",
		Params:      []string{"name"},
		Cypher:      "MATCH (src:Symbol {name: '$name'})-[:CALLS]->(callee:Symbol) RETURN callee",
		Cols:        1,
	},
	"imports_of": {
		ID:          "imports_of",
		Description: "Find packages imported by files matching a path",
		Params:      []string{"path"},
		Cypher:      "MATCH (f:File)-[:IMPORTS]->(p:Package) WHERE f.path CONTAINS '$path' RETURN p",
		Cols:        1,
	},
	"importers_of": {
		ID:          "importers_of",
		Description: "Find files that import the named package",
		Params:      []string{"name"},
		Cypher:      "MATCH (f:File)-[:IMPORTS]->(p:Package {name: '$name'}) RETURN f",
		Cols:        1,
	},
	"symbols_in": {
		ID:          "symbols_in",
		Description: "Find symbols contained in files matching a path",
		Params:      []string{"path"},
		Cypher:      "MATCH (c)-[:CONTAINS]->(s:Symbol) WHERE c.path CONTAINS '$path' RETURN s",
		Cols:        1,
	},
	"call_chain": {
		ID:          "call_chain",
		Description: "Find the shortest call path between two symbols",
		Params:      []string{"from", "to"},
		Cypher:      "MATCH path = shortestPath((a:Symbol {name: '$from'})-[:CALLS*..10]->(b:Symbol {name: '$to'})) RETURN path",
		Cols:        1,
	},
	"most_connected": {
		ID:          "most_connected",
		Description: "List the most-called symbols up to a limit",
		Params:      []string{"limit"},
		Cypher:      "MATCH (s:Symbol)<-[:CALLS]-(caller:Symbol) RETURN s.name, s.kind, s.file, count(caller) AS call_count ORDER BY call_count DESC LIMIT $limit",
		Cols:        4,
	},
	"dead_code": {
		ID:          "dead_code",
		Description: "Find functions that are never called",
		Params:      []string{},
		Cypher:      "MATCH (s:Symbol) WHERE s.kind = 'function' AND NOT ()-[:CALLS]->(s) RETURN s",
		Cols:        1,
	},
	"depends_on": {
		ID:          "depends_on",
		Description: "Find distinct packages depended on by files matching a path prefix",
		Params:      []string{"pkg"},
		Cypher:      "MATCH (f:File)-[:IMPORTS]->(p:Package) WHERE f.path CONTAINS '$pkg' RETURN DISTINCT p",
		Cols:        1,
	},
	"dependents_of": {
		ID:          "dependents_of",
		Description: "Find distinct files that depend on the named package",
		Params:      []string{"name"},
		Cypher:      "MATCH (f:File)-[:IMPORTS]->(p:Package {name: '$name'}) RETURN DISTINCT f",
		Cols:        1,
	},
}

// GetTemplate returns the template with the given ID, or nil if not found.
func GetTemplate(id string) *Template {
	return templates[id]
}

// TemplateList returns a formatted list of all templates suitable for a classifier prompt.
func TemplateList() string {
	var sb strings.Builder
	for _, t := range templates {
		params := strings.Join(t.Params, ", ")
		if params == "" {
			params = "(none)"
		}
		fmt.Fprintf(&sb, "- %s: %s [params: %s]\n", t.ID, t.Description, params)
	}
	return sb.String()
}
