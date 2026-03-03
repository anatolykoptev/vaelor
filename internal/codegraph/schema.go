package codegraph

import "strings"

// GraphSchemaText returns a human-readable description of the full code
// knowledge graph schema (vertex labels, edge labels, and their properties).
// It is injected into the freeform Cypher generation prompt so the LLM knows
// about every node and relationship type, including cross-language constructs.
func GraphSchemaText() string {
	var b strings.Builder

	b.WriteString("Vertex labels:\n")
	b.WriteString("  - Package (name, path, repo)\n")
	b.WriteString("  - File (path, language, lines)\n")
	b.WriteString("  - Symbol (name, kind, signature, file, start_line, end_line, complexity, lines, pagerank)\n")
	b.WriteString("  - Layer (name, role, language, root_dir)\n")
	b.WriteString("  - Route (method, path, framework)\n")
	b.WriteString("\n")

	b.WriteString("Edge labels:\n")
	b.WriteString("  - CONTAINS (Package->File, File->Symbol)\n")
	b.WriteString("  - CALLS (Symbol->Symbol; properties: line)\n")
	b.WriteString("  - INHERITS (Symbol->Symbol) — struct embedding (Go), class extends (Python/Java/TS)\n")
	b.WriteString("  - IMPLEMENTS (Symbol->Symbol) — interface implementation (Java/TS)\n")
	b.WriteString("  - IMPORTS (File->Package; properties: alias)\n")
	b.WriteString("  - BELONGS_TO (File->Layer)\n")
	b.WriteString("  - HANDLES (Symbol->Route; properties: line) — server-side handler (HTTP or WordPress hook callback)\n")
	b.WriteString("  - FETCHES (Symbol->Route; properties: line) — client-side caller (HTTP or WordPress hook invocation)\n")
	b.WriteString("\n")
	b.WriteString("Route also represents WordPress hooks: Method=ACTION|FILTER, Path=hook_name, framework=wordpress\n")
	b.WriteString("\n")

	b.WriteString("Symbol kind values: function, method, type, struct, interface, class, const, var, module")

	return b.String()
}
