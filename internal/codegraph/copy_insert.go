package codegraph

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// labelInfo holds AGE label metadata for one vertex or edge label.
type labelInfo struct {
	LabelID uint64
	SeqName string
}

// copyChunkSize is the maximum number of rows per single COPY FROM STDIN call.
// Each COPY is a single transaction in postgres — the entire buffer is
// materialised in postgres memory before commit. Chunking bounds the peak
// postgres working set so a 10K-vertex repo doesn't load 10K rows into WAL
// buffers at once. 1000 rows x ~200 bytes/row ~ 200KB per COPY — well under
// the 2GB postgres cgroup even with concurrent AGE overhead.
const copyChunkSize = 1000

// BulkCopyInsert inserts all vertices and edges directly into AGE's internal
// PostgreSQL tables using text-format COPY FROM STDIN — bypassing the Cypher
// parser and executor entirely.
//
// graphid formula: (label_id << 48) | seq_num, seq starts at 1 per label.
// Both graphid and agtype accept string input in text-format COPY.
//
// Returns an error on failure; the caller should fall back to UNWIND inserts.
func (s *Store) BulkCopyInsert(ctx context.Context, gname string, vertices []vertexData, edges []edgeData) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SET statement_timeout = 0; SET synchronous_commit = off"); err != nil {
		return fmt.Errorf("set session vars: %w", err)
	}

	// Query label metadata from ag_catalog.ag_label.
	labels, err := queryLabelInfos(ctx, conn, gname)
	if err != nil {
		return fmt.Errorf("query label infos: %w", err)
	}
	if len(labels) == 0 {
		return fmt.Errorf("no labels found for graph %q — graph may not exist", gname)
	}

	// Group vertices by label, dedup by vertexKey, assign graphids, build lookup
	// index for edge resolution.
	type vWithID struct {
		v  vertexData
		id uint64
	}
	vertexGroups := make(map[string][]vWithID)
	// vertexIndex[label][key] = graphid — used to resolve edge endpoints.
	vertexIndex := make(map[string]map[string]uint64)
	// labelSeqs tracks max seq used per label, for advancing sequences after COPY.
	labelSeqs := make(map[string]uint64)

	byLabel := make(map[string][]vertexData)
	for _, v := range vertices {
		byLabel[v.Label] = append(byLabel[v.Label], v)
	}

	for label, group := range byLabel {
		info, ok := labels[label]
		if !ok {
			slog.Warn("codegraph copy: unknown vertex label, skipping", slog.String("label", label))
			continue
		}
		if vertexIndex[label] == nil {
			vertexIndex[label] = make(map[string]uint64, len(group))
		}
		seq := uint64(0)
		for _, v := range group {
			vk := vertexKey(v)
			if _, seen := vertexIndex[label][vk]; seen {
				// Dedup: COPY does not MERGE — skip duplicate vertices to avoid
				// multiple rows with different graphids for the same key.
				continue
			}
			seq++
			gid := (info.LabelID << 48) | seq
			vertexIndex[label][vk] = gid
			vertexGroups[label] = append(vertexGroups[label], vWithID{v, gid})
		}
		if seq > 0 {
			labelSeqs[label] = seq
		}
	}

	// COPY vertices — chunked per label to bound postgres memory.
	// Each chunk is a separate COPY (transaction), so postgres commits and
	// releases WAL buffers between chunks instead of accumulating the full
	// label in one transaction.
	totalVertices := 0
	pgConn := conn.Conn().PgConn()
	for label, group := range vertexGroups {
		sql := fmt.Sprintf(`COPY "%s"."%s" (id, properties) FROM STDIN (FORMAT text)`, gname, label)
		for i := 0; i < len(group); i += copyChunkSize {
			end := i + copyChunkSize
			if end > len(group) {
				end = len(group)
			}
			var buf bytes.Buffer
			for _, row := range group[i:end] {
				props, err := agtypeJSON(row.v.Props)
				if err != nil {
					return fmt.Errorf("json props vertex %s: %w", label, err)
				}
				props = copyEscape(props)
				fmt.Fprintf(&buf, "%d\t%s\n", row.id, props)
			}
			if _, err := pgConn.CopyFrom(ctx, &buf, sql); err != nil {
				return fmt.Errorf("copy vertices label=%s chunk [%d:%d]: %w", label, i, end, err)
			}
		}
		totalVertices += len(group)
	}

	// Guard: if we had vertices to insert but inserted none, all labels were
	// unknown — treat as error so the fallback fires.
	if len(vertices) > 0 && totalVertices == 0 {
		return fmt.Errorf("no vertices inserted (0 of %d) — all vertex labels unknown", len(vertices))
	}
	slog.Info("codegraph: copy: vertices done", slog.Int("count", totalVertices))

	// Group edges by label, assign graphids, COPY.
	edgesByLabel := make(map[string][]edgeData)
	for _, e := range edges {
		edgesByLabel[e.EdgeLabel] = append(edgesByLabel[e.EdgeLabel], e)
	}

	totalEdges := 0
	for edgeLabel, group := range edgesByLabel {
		info, ok := labels[edgeLabel]
		if !ok {
			slog.Warn("codegraph copy: unknown edge label, skipping", slog.String("label", edgeLabel))
			continue
		}

		seq := uint64(0)
		skipped := 0

		// First pass: resolve endpoint IDs and build resolved edge list.
		type resolvedEdge struct {
			edgeID, fromID, toID uint64
			props                string
		}
		var resolved []resolvedEdge
		for _, e := range group {
			fromID, ok1 := vertexIndex[e.FromLabel][e.FromKey]
			toID, ok2 := vertexIndex[e.ToLabel][e.ToKey]
			if !ok1 || !ok2 {
				skipped++
				continue
			}
			seq++
			edgeID := (info.LabelID << 48) | seq
			props := "{}"
			if len(e.Props) > 0 {
				if j, jerr := agtypeJSON(e.Props); jerr == nil {
					props = copyEscape(j)
				}
			}
			resolved = append(resolved, resolvedEdge{edgeID, fromID, toID, props})
		}

		if skipped > 0 {
			slog.Debug("codegraph copy: dangling edges skipped",
				slog.String("label", edgeLabel), slog.Int("count", skipped))
		}
		if len(resolved) == 0 {
			continue
		}

		// Chunked COPY — each chunk is a separate transaction.
		sql := fmt.Sprintf(`COPY "%s"."%s" (id, start_id, end_id, properties) FROM STDIN (FORMAT text)`, gname, edgeLabel)
		for i := 0; i < len(resolved); i += copyChunkSize {
			end := i + copyChunkSize
			if end > len(resolved) {
				end = len(resolved)
			}
			var buf bytes.Buffer
			for _, re := range resolved[i:end] {
				fmt.Fprintf(&buf, "%d\t%d\t%d\t%s\n", re.edgeID, re.fromID, re.toID, re.props)
			}
			if _, err := pgConn.CopyFrom(ctx, &buf, sql); err != nil {
				return fmt.Errorf("copy edges label=%s chunk [%d:%d]: %w", edgeLabel, i, end, err)
			}
		}
		labelSeqs[edgeLabel] = seq
		totalEdges += int(seq)
	}
	slog.Info("codegraph: copy: edges done", slog.Int("count", totalEdges))

	// Advance each label's sequence past the highest inserted seq so that
	// subsequent Cypher MERGE/CREATE generates unique IDs.
	if err := advanceLabelSeqs(ctx, conn, gname, labels, labelSeqs); err != nil {
		return fmt.Errorf("advance sequences: %w", err)
	}

	return nil
}

// queryLabelInfos fetches label_id and seq_name for all labels in the graph.
func queryLabelInfos(ctx context.Context, conn *pgxpool.Conn, gname string) (map[string]labelInfo, error) {
	rows, err := conn.Query(ctx, `
		SELECT l.name, l.id::bigint, l.seq_name
		FROM ag_catalog.ag_label l
		JOIN ag_catalog.ag_graph g ON l.graph = g.graphid
		WHERE g.name = $1
	`, gname)
	if err != nil {
		return nil, fmt.Errorf("query ag_label: %w", err)
	}
	defer rows.Close()

	result := make(map[string]labelInfo)
	for rows.Next() {
		var name, seqName string
		var labelID int64
		if err := rows.Scan(&name, &labelID, &seqName); err != nil {
			return nil, fmt.Errorf("scan ag_label row: %w", err)
		}
		result[name] = labelInfo{LabelID: uint64(labelID), SeqName: seqName}
	}
	return result, rows.Err()
}

// advanceLabelSeqs calls setval on each label's sequence to the count inserted.
// Critical: without this, subsequent Cypher MERGE/CREATE will generate IDs that
// collide with those assigned via COPY.
func advanceLabelSeqs(ctx context.Context, conn *pgxpool.Conn, gname string, labels map[string]labelInfo, seqs map[string]uint64) error {
	for label, count := range seqs {
		if count == 0 {
			continue
		}
		info, ok := labels[label]
		if !ok {
			continue
		}
		// Qualify seq_name with graph schema (ag_label stores it unqualified).
		seqQual := fmt.Sprintf(`"%s"."%s"`, gname, info.SeqName)
		if _, err := conn.Exec(ctx, fmt.Sprintf(`SELECT setval('%s', $1)`, seqQual), int64(count)); err != nil {
			return fmt.Errorf("setval %s: %w", label, err)
		}
	}
	return nil
}

// agtypeJSON serializes string→string props to a JSON object string.
// agtype_in() accepts standard JSON.
func agtypeJSON(props map[string]string) (string, error) {
	if len(props) == 0 {
		return "{}", nil
	}
	var sb strings.Builder
	sb.WriteByte('{')
	first := true
	for k, v := range props {
		if !first {
			sb.WriteByte(',')
		}
		first = false
		sb.WriteByte('"')
		writeJSONString(&sb, k)
		sb.WriteString(`":"`)
		writeJSONString(&sb, v)
		sb.WriteByte('"')
	}
	sb.WriteByte('}')
	return sb.String(), nil
}

// writeJSONString writes s into sb with JSON string escaping.
// Escapes control characters, quotes, and backslashes.
func writeJSONString(sb *strings.Builder, s string) {
	for _, r := range s {
		switch {
		case r == '"':
			sb.WriteString(`\"`)
		case r == '\\':
			sb.WriteString(`\\`)
		case r == '\n':
			sb.WriteString(`\n`)
		case r == '\r':
			sb.WriteString(`\r`)
		case r == '\t':
			sb.WriteString(`\t`)
		case r < 0x20:
			// Escape other control characters as \uXXXX.
			fmt.Fprintf(sb, `\u%04x`, r)
		default:
			sb.WriteRune(r)
		}
	}
}

// copyEscape prepares a JSON string for COPY text-format transmission.
// COPY text-format interprets backslash sequences (\n, \t, \\, etc.) BEFORE
// handing bytes to agtype_in. We must double every backslash so that COPY
// decodes \\ → \ and agtype_in receives the original JSON.
// Example: JSON {"sig":"def f(x):\n\treturn x"} becomes
// {"sig":"def f(x):\\n\\treturn x"} in the COPY stream,
// which COPY decodes back to the original JSON for agtype_in.
func copyEscape(s string) string {
	return strings.ReplaceAll(s, `\`, `\\`)
}
