package codegraph

import (
	"context"
	"fmt"
	"sort"
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

// buildVertexUnwindBatch generates a single Cypher UNWIND statement that
// MERGEs all vertices of the SAME label in one DB round-trip.
// All vertices in the slice MUST have the same Label.
func buildVertexUnwindBatch(graphName string, vertices []vertexData) string {
	if len(vertices) == 0 {
		return ""
	}
	label := vertices[0].Label

	// Collect all property keys across the batch for a consistent SET clause.
	keySet := make(map[string]struct{})
	for _, v := range vertices {
		for k := range v.Props {
			keySet[k] = struct{}{}
		}
	}
	keys := make([]string, 0, len(keySet))
	for k := range keySet {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("UNWIND [")
	for i, v := range vertices {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("{")
		for j, k := range keys {
			if j > 0 {
				sb.WriteString(", ")
			}
			val := v.Props[k]
			fmt.Fprintf(&sb, "%s: '%s'", k, escapeCypher(val))
		}
		sb.WriteString("}")
	}
	sb.WriteString("] AS props\n")

	matchProp := unwindVertexMatch(label, keys)
	fmt.Fprintf(&sb, "MERGE (v:%s {%s})\n", label, matchProp)

	var setParts []string
	for _, k := range keys {
		setParts = append(setParts, fmt.Sprintf("v.%s = props.%s", k, k))
	}
	if len(setParts) > 0 {
		fmt.Fprintf(&sb, "SET %s\n", strings.Join(setParts, ", "))
	}
	sb.WriteString("RETURN count(v)")
	return sb.String()
}

// unwindVertexMatch returns the MERGE match expression using props.key syntax.
func unwindVertexMatch(label string, keys []string) string {
	hasPath := false
	for _, k := range keys {
		if k == "path" {
			hasPath = true
			break
		}
	}
	switch label {
	case "Symbol":
		return "name: props.name, file: props.file"
	case "Package":
		if hasPath {
			return "path: props.path"
		}
		return "name: props.name"
	case "Layer":
		return "name: props.name"
	case "Route":
		return "method: props.method, path: props.path"
	default:
		return "path: props.path"
	}
}

// edgeGroupKey groups edges with identical endpoint labels and edge label
// so they can share a single UNWIND+MATCH+MERGE statement.
type edgeGroupKey struct {
	FromLabel, ToLabel, EdgeLabel string
}

// buildEdgeUnwindBatch generates UNWIND+MATCH+MERGE Cypher for a batch of
// edges sharing the same FromLabel, ToLabel, and EdgeLabel.
// AGE 1.7.0: MATCH after UNWIND works; MATCH after MERGE does not.
func buildEdgeUnwindBatch(graphName string, edges []edgeData) string {
	if len(edges) == 0 {
		return ""
	}
	e0 := edges[0]

	var sb strings.Builder
	sb.WriteString("UNWIND [")
	for i, e := range edges {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "{fk: '%s', tk: '%s'}", escapeCypher(e.FromKey), escapeCypher(e.ToKey))
	}
	sb.WriteString("] AS e\n")

	fmt.Fprintf(&sb, "MATCH (f:%s {%s})\n", e0.FromLabel, unwindEdgeMatch(e0.FromLabel, "fk"))
	fmt.Fprintf(&sb, "MATCH (t:%s {%s})\n", e0.ToLabel, unwindEdgeMatch(e0.ToLabel, "tk"))
	fmt.Fprintf(&sb, "MERGE (f)-[:%s]->(t)\n", e0.EdgeLabel)
	sb.WriteString("RETURN count(*)")
	return sb.String()
}

// unwindEdgeMatch builds the MATCH property expression for an edge endpoint.
func unwindEdgeMatch(label, field string) string {
	switch label {
	case "Symbol":
		// Symbol FromKey/ToKey is "name:file" — split in Cypher.
		return fmt.Sprintf("name: split(e.%s, ':')[0], file: split(e.%s, ':')[1]", field, field)
	case "Package":
		return fmt.Sprintf("path: e.%s", field)
	case "File":
		return fmt.Sprintf("path: e.%s", field)
	case "Layer":
		return fmt.Sprintf("name: e.%s", field)
	case "Route":
		return fmt.Sprintf("method: split(e.%s, ':')[0], path: split(e.%s, ':')[1]", field, field)
	default:
		return fmt.Sprintf("path: e.%s", field)
	}
}

// insertBatches groups vertices by label and inserts each group via UNWIND
// batches of batchSize. AGE UNWIND is stable to 5000+ items; the old
// multi-MERGE approach crashed AGE on ARM beyond ~20 items per query.
func insertBatches(
	ctx context.Context,
	w CypherWriter,
	gname string,
	batchSize int,
	vertices []vertexData,
	_ func(string, []vertexData) string, // kept for API compat
) error {
	if len(vertices) == 0 {
		return nil
	}
	groups := make(map[string][]vertexData)
	for _, v := range vertices {
		groups[v.Label] = append(groups[v.Label], v)
	}
	for label, group := range groups {
		for i := 0; i < len(group); i += batchSize {
			end := i + batchSize
			if end > len(group) {
				end = len(group)
			}
			cypher := buildVertexUnwindBatch(gname, group[i:end])
			if cypher == "" {
				continue
			}
			if err := w.ExecCypherWrite(ctx, gname, cypher); err != nil {
				return fmt.Errorf("vertices label=%s batch [%d:%d]: %w", label, i, end, err)
			}
		}
	}
	return nil
}

// insertEdgeBatches groups edges by (fromLabel, toLabel, edgeLabel) and
// inserts each group via UNWIND+MATCH+MERGE batches of batchSize.
func insertEdgeBatches(ctx context.Context, w CypherWriter, gname string, batchSize int, edges []edgeData) error {
	if len(edges) == 0 {
		return nil
	}
	groups := make(map[edgeGroupKey][]edgeData)
	for _, e := range edges {
		k := edgeGroupKey{e.FromLabel, e.ToLabel, e.EdgeLabel}
		groups[k] = append(groups[k], e)
	}
	for key, group := range groups {
		for i := 0; i < len(group); i += batchSize {
			end := i + batchSize
			if end > len(group) {
				end = len(group)
			}
			cypher := buildEdgeUnwindBatch(gname, group[i:end])
			if cypher == "" {
				continue
			}
			if err := w.ExecCypherWrite(ctx, gname, cypher); err != nil {
				return fmt.Errorf("edges %s-[%s]->%s batch [%d:%d]: %w",
					key.FromLabel, key.EdgeLabel, key.ToLabel, i, end, err)
			}
		}
	}
	return nil
}
