package codegraph

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// compositeKeyDelim is the delimiter used to join and split composite vertex/edge
// keys such as Symbol ("name\x00file") and Route ("method\x00path").
//
// NUL (\x00) is chosen because it cannot appear in a method name, path segment,
// symbol name, or file path, making the split unambiguous even when the path or
// name contains ':' (e.g. /peer1:unknown, :id params). The delimiter is split
// away before any Cypher is emitted — it never appears in the generated query.
const compositeKeyDelim = "\x00"

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

// vertexKey returns the primary key value for a vertex.
// Package: keyed by path. File: keyed by path.
// Symbol: keyed by "name\x00file" (compositeKeyDelim).
// Route:  keyed by "method\x00path\x00side" (compositeKeyDelim, 3-part).
//
// The 3-part Route key makes a server GET /api/x and a client GET /api/x into
// DISTINCT vertices — the precondition for cross-repo provider↔consumer confirm
// (server-route in repo A ∩ client-route in repo B, same method+path).
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
		return name + compositeKeyDelim + file
	case "Layer":
		return v.Props["name"]
	case "Route":
		return v.Props["method"] + compositeKeyDelim + v.Props["path"] + compositeKeyDelim + v.Props["side"]
	default:
		return v.Props["name"]
	}
}

// matchKey builds the Cypher property filter for a MATCH/MERGE by label and key.
// Package: if key contains "/" match by path, else by name.
// Symbol: split "name\x00file" (compositeKeyDelim) into name + file props.
// Route:  split "method\x00path\x00side" (compositeKeyDelim, 3-part) into method + path + side props.
// Everything else: match by path.
// The compositeKeyDelim (\x00) is split away here and never appears in the
// returned Cypher string.
func matchKey(label, key string) string {
	switch label {
	case "Package":
		if strings.Contains(key, "/") {
			return fmt.Sprintf("path: '%s'", escapeCypher(key))
		}
		return fmt.Sprintf("name: '%s'", escapeCypher(key))
	case "Symbol":
		parts := strings.SplitN(key, compositeKeyDelim, 2)
		if len(parts) == 2 {
			return fmt.Sprintf("name: '%s', file: '%s'", escapeCypher(parts[0]), escapeCypher(parts[1]))
		}
		return fmt.Sprintf("name: '%s'", escapeCypher(key))
	case "Layer":
		return fmt.Sprintf("name: '%s'", escapeCypher(key))
	case "Route":
		// 3-part key: method\x00path\x00side — split into all three props.
		parts := strings.SplitN(key, compositeKeyDelim, 3)
		if len(parts) == 3 {
			return fmt.Sprintf("method: '%s', path: '%s', side: '%s'",
				escapeCypher(parts[0]), escapeCypher(parts[1]), escapeCypher(parts[2]))
		}
		// Legacy 2-part fallback (no side) — treat as path+method only (graceful degradation).
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
		return "method: props.method, path: props.path, side: props.side"
	default:
		return "path: props.path"
	}
}

// edgeGroupKey groups edges with identical endpoint labels and edge label
// so they can share a single UNWIND+MATCH+MERGE statement.
type edgeGroupKey struct {
	FromLabel, ToLabel, EdgeLabel string
}

// edgeUnwindFields returns the UNWIND map field names for an edge endpoint's key,
// and the MATCH property expression referencing those fields.
//
// For Symbol and Route endpoints the composite key is pre-split in Go into
// separate fields so that compositeKeyDelim (\x00) NEVER appears in the emitted Cypher.
//
//   - Symbol FromKey (field="fk"):  stored as fn (name), ff (file).
//   - Symbol ToKey   (field="tk"):  stored as tn (name), tf (file).
//   - Route  FromKey (field="fk"):  stored as fm (method), fp (path), fs (side).
//   - Route  ToKey   (field="tk"):  stored as tm (method), tp (path), ts (side).
//   - All other labels: single field (fk or tk) holding the raw key.
//
// splitCompositeKey extracts the two parts from a compositeKeyDelim key (Symbol).
// Returns ("", key) if the delimiter is absent (fallback for non-composite keys).
func splitCompositeKey(key string) (part1, part2 string) {
	parts := strings.SplitN(key, compositeKeyDelim, 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", key
}

// splitRouteKey extracts the three parts (method, path, side) from a 3-part
// Route composite key (method\x00path\x00side). Returns ("","",key) when the
// delimiter is absent (fallback for malformed keys).
func splitRouteKey(key string) (method, path, side string) {
	parts := strings.SplitN(key, compositeKeyDelim, 3)
	if len(parts) == 3 {
		return parts[0], parts[1], parts[2]
	}
	return "", "", key
}

// buildEdgeUnwindBatch generates UNWIND+MATCH+MERGE Cypher for a batch of
// edges sharing the same FromLabel, ToLabel, and EdgeLabel.
// AGE 1.7.0: MATCH after UNWIND works; MATCH after MERGE does not.
//
// Symbol and Route composite keys are pre-split in Go so that compositeKeyDelim
// (\x00) is never embedded in the emitted Cypher string.
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
		sb.WriteString("{")
		// From-endpoint fields.
		switch e0.FromLabel {
		case "Symbol":
			n, f := splitCompositeKey(e.FromKey)
			fmt.Fprintf(&sb, "fn: '%s', ff: '%s'", escapeCypher(n), escapeCypher(f))
		case "Route":
			m, p, s := splitRouteKey(e.FromKey)
			fmt.Fprintf(&sb, "fm: '%s', fp: '%s', fs: '%s'", escapeCypher(m), escapeCypher(p), escapeCypher(s))
		default:
			fmt.Fprintf(&sb, "fk: '%s'", escapeCypher(e.FromKey))
		}
		sb.WriteString(", ")
		// To-endpoint fields.
		switch e0.ToLabel {
		case "Symbol":
			n, f := splitCompositeKey(e.ToKey)
			fmt.Fprintf(&sb, "tn: '%s', tf: '%s'", escapeCypher(n), escapeCypher(f))
		case "Route":
			m, p, s := splitRouteKey(e.ToKey)
			fmt.Fprintf(&sb, "tm: '%s', tp: '%s', ts: '%s'", escapeCypher(m), escapeCypher(p), escapeCypher(s))
		default:
			fmt.Fprintf(&sb, "tk: '%s'", escapeCypher(e.ToKey))
		}
		sb.WriteString("}")
	}
	sb.WriteString("] AS e\n")

	fmt.Fprintf(&sb, "MATCH (f:%s {%s})\n", e0.FromLabel, unwindEdgeMatch(e0.FromLabel, "f"))
	fmt.Fprintf(&sb, "MATCH (t:%s {%s})\n", e0.ToLabel, unwindEdgeMatch(e0.ToLabel, "t"))
	fmt.Fprintf(&sb, "MERGE (f)-[:%s]->(t)\n", e0.EdgeLabel)
	sb.WriteString("RETURN count(*)")
	return sb.String()
}

// unwindEdgeMatch builds the MATCH property expression for an edge endpoint.
// prefix is "f" for FromLabel endpoints and "t" for ToLabel endpoints.
//
// For Symbol and Route the composite key has already been pre-split in Go by
// buildEdgeUnwindBatch into separate UNWIND fields (fn/ff, tm/tp, etc.), so no
// Cypher-side split() is needed and compositeKeyDelim (\x00) never appears in
// the generated query.
func unwindEdgeMatch(label, prefix string) string {
	switch label {
	case "Symbol":
		// Pre-split fields: <prefix>n = name, <prefix>f = file.
		return fmt.Sprintf("name: e.%sn, file: e.%sf", prefix, prefix)
	case "Package":
		return fmt.Sprintf("path: e.%sk", prefix)
	case "File":
		return fmt.Sprintf("path: e.%sk", prefix)
	case "Layer":
		return fmt.Sprintf("name: e.%sk", prefix)
	case "Route":
		// Pre-split fields: <prefix>m = method, <prefix>p = path, <prefix>s = side.
		return fmt.Sprintf("method: e.%sm, path: e.%sp, side: e.%ss", prefix, prefix, prefix)
	default:
		return fmt.Sprintf("path: e.%sk", prefix)
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
		labelBatch := adaptiveBatchSize(label)
		for i := 0; i < len(group); i += labelBatch {
			end := i + labelBatch
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

// maxCypherBatchBytes is the target max Cypher UNWIND query size per batch.
// Large queries cause AGE parser slowness: Symbol avg 283 bytes/vertex,
// 500-vertex UNWIND = 141KB → 9s. At 8KB: ~23 per batch → ~40ms.
const maxCypherBatchBytes = 8192

// adaptiveBatchSize returns how many vertices of the given label
// fit within maxCypherBatchBytes.
func adaptiveBatchSize(label string) int {
	var bytesPerVertex int
	switch label {
	case "Symbol":
		bytesPerVertex = 350
	case "File":
		bytesPerVertex = 120
	case "Package":
		bytesPerVertex = 80
	default:
		bytesPerVertex = 100
	}
	n := maxCypherBatchBytes / bytesPerVertex
	if n < 1 {
		return 1
	}
	return n
}
