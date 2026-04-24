package codegraph

import (
	"context"
	"fmt"
	"strings"
)

// buildVertexBatch generates a Cypher statement that MERGEs all vertices in batch.
func buildVertexBatch(graphName string, vertices []vertexData) string {
	if len(vertices) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, v := range vertices {
		key := vertexKey(v)
		varName := fmt.Sprintf("v%d", i)
		fmt.Fprintf(&sb, "MERGE (%s:%s {%s})\n", varName, v.Label, matchKey(v.Label, key))
		if len(v.Props) > 0 {
			fmt.Fprintf(&sb, "SET %s\n", formatSet(varName, v.Props))
		}
	}
	sb.WriteString("RETURN 1")
	return sb.String()
}

// buildEdgeBatch generates a Cypher statement that MATCHes endpoints and MERGEs edges.
// NOTE: Currently unused — AGE doesn't support MATCH after MERGE in a single cypher() call.
// Kept for potential future use with Neo4j or AGE improvements.
func buildEdgeBatch(graphName string, edges []edgeData) string {
	if len(edges) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, e := range edges {
		fromVar := fmt.Sprintf("f%d", i)
		toVar := fmt.Sprintf("t%d", i)
		edgeVar := fmt.Sprintf("e%d", i)
		fmt.Fprintf(&sb, "MATCH (%s:%s {%s})\n", fromVar, e.FromLabel, matchKey(e.FromLabel, e.FromKey))
		fmt.Fprintf(&sb, "MATCH (%s:%s {%s})\n", toVar, e.ToLabel, matchKey(e.ToLabel, e.ToKey))
		if len(e.Props) > 0 {
			fmt.Fprintf(&sb, "MERGE (%s)-[%s:%s {%s}]->(%s)\n",
				fromVar, edgeVar, e.EdgeLabel, formatProps(e.Props), toVar)
		} else {
			fmt.Fprintf(&sb, "MERGE (%s)-[%s:%s]->(%s)\n",
				fromVar, edgeVar, e.EdgeLabel, toVar)
		}
	}
	sb.WriteString("RETURN 1")
	return sb.String()
}

// buildSingleEdge generates a Cypher statement for one edge: MATCH endpoints, MERGE edge.
func buildSingleEdge(e edgeData) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "MATCH (f:%s {%s})\n", e.FromLabel, matchKey(e.FromLabel, e.FromKey))
	fmt.Fprintf(&sb, "MATCH (t:%s {%s})\n", e.ToLabel, matchKey(e.ToLabel, e.ToKey))
	if len(e.Props) > 0 {
		fmt.Fprintf(&sb, "MERGE (f)-[e:%s {%s}]->(t)\n", e.EdgeLabel, formatProps(e.Props))
	} else {
		fmt.Fprintf(&sb, "MERGE (f)-[e:%s]->(t)\n", e.EdgeLabel)
	}
	sb.WriteString("RETURN 1")
	return sb.String()
}

// vertexKey returns the primary key value for a vertex.
// Package: keyed by path. File: keyed by path. Symbol: keyed by "name:file".
func vertexKey(v vertexData) string {
	switch v.Label {
	case "Package":
		if p, ok := v.Props["path"]; ok {
			return p
		}
		return v.Props["name"]
	case "File":
		return v.Props["path"]
	case "Symbol":
		name := v.Props["name"]
		file := v.Props["file"]
		return name + ":" + file
	case "Layer":
		return v.Props["name"]
	case "Route":
		return v.Props["method"] + ":" + v.Props["path"]
	default:
		return v.Props["name"]
	}
}

// matchKey builds the Cypher property filter for a MATCH/MERGE by label and key.
// Package: if key contains "/" match by path, else by name.
// Symbol: split "name:file" into name + file props.
// Everything else: match by path.
func matchKey(label, key string) string {
	switch label {
	case "Package":
		if strings.Contains(key, "/") {
			return fmt.Sprintf("path: '%s'", escapeCypher(key))
		}
		return fmt.Sprintf("name: '%s'", escapeCypher(key))
	case "Symbol":
		idx := strings.Index(key, ":")
		if idx >= 0 {
			name := key[:idx]
			file := key[idx+1:]
			return fmt.Sprintf("name: '%s', file: '%s'", escapeCypher(name), escapeCypher(file))
		}
		return fmt.Sprintf("name: '%s'", escapeCypher(key))
	case "Layer":
		return fmt.Sprintf("name: '%s'", escapeCypher(key))
	case "Route":
		parts := strings.SplitN(key, ":", 2)
		if len(parts) == 2 {
			return fmt.Sprintf("method: '%s', path: '%s'", escapeCypher(parts[0]), escapeCypher(parts[1]))
		}
		return fmt.Sprintf("path: '%s'", escapeCypher(key))
	default:
		return fmt.Sprintf("path: '%s'", escapeCypher(key))
	}
}

// formatProps renders a Props map as a Cypher inline property literal.
// e.g. key: 'value', key2: 'value2'
func formatProps(props map[string]string) string {
	if len(props) == 0 {
		return ""
	}
	parts := make([]string, 0, len(props))
	for k, v := range props {
		parts = append(parts, fmt.Sprintf("%s: '%s'", k, escapeCypher(v)))
	}
	return strings.Join(parts, ", ")
}

// formatSet renders a SET clause fragment for a variable.
// e.g. v.key = 'value', v.key2 = 'value2'
func formatSet(varName string, props map[string]string) string {
	parts := make([]string, 0, len(props))
	for k, v := range props {
		parts = append(parts, fmt.Sprintf("%s.%s = '%s'", varName, k, escapeCypher(v)))
	}
	return strings.Join(parts, ", ")
}

// insertBatches inserts vertices one at a time using sequential Cypher writes.
// Multi-MERGE batches crash AGE on ARM; concurrent writes cause "Entity failed
// to be updated" races because AGE MERGE is not concurrency-safe.
// Single-vertex sequential inserts are stable. The background context in
// tool_code_graph.go ensures large repos are not limited by MCP client timeout.
//
// The batchSize parameter is kept for API compatibility but is ignored.
func insertBatches(
	ctx context.Context,
	store *Store,
	gname string,
	_ int,
	vertices []vertexData,
	buildFn func(string, []vertexData) string,
) error {
	for _, v := range vertices {
		cypher := buildFn(gname, []vertexData{v})
		if cypher == "" {
			continue
		}
		if err := store.ExecCypherWrite(ctx, gname, cypher); err != nil {
			return fmt.Errorf("vertex %q: %w", v.Props["name"], err)
		}
	}
	return nil
}

// insertEdgeBatches inserts edges one at a time. AGE does not support
// MATCH-after-MERGE in a single cypher() call, so edges are always one
// statement each.
func insertEdgeBatches(ctx context.Context, store *Store, gname string, _ int, edges []edgeData) error {
	for i, e := range edges {
		cypher := buildSingleEdge(e)
		if cypher == "" {
			continue
		}
		if err := store.ExecCypherWrite(ctx, gname, cypher); err != nil {
			return fmt.Errorf("edge %d (%s->%s): %w", i, e.FromKey, e.ToKey, err)
		}
	}
	return nil
}
