package codegraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// snapshotTableSQL defines the schema for storing code graph snapshots.
// Schema-qualified to public — see metaTableSQL for the leak-prevention rationale.
const snapshotTableSQL = `
CREATE TABLE IF NOT EXISTS public.code_graph_snapshots (
    repo_key    TEXT NOT NULL,
    snapshot_at TIMESTAMPTZ NOT NULL,
    symbols     JSONB NOT NULL,
    edges       JSONB NOT NULL,
    PRIMARY KEY (repo_key, snapshot_at)
)`

// semanticEdgeLabels defines which edge types to include in snapshots.
var semanticEdgeLabels = map[string]bool{
	"CALLS": true, "INHERITS": true, "IMPLEMENTS": true, "TESTED_BY": true, "USES": true,
}

// SnapshotSymbol holds a single symbol entry in a snapshot.
type SnapshotSymbol struct {
	Name       string `json:"n"`
	Kind       string `json:"k"`
	File       string `json:"f"`
	Community  int    `json:"c"`
	Complexity int    `json:"x,omitempty"`
}

// SnapshotEdge holds a single directed edge entry in a snapshot.
type SnapshotEdge struct {
	From  string `json:"s"`
	To    string `json:"t"`
	Label string `json:"l"`
}

// Snapshot is a point-in-time capture of a code graph for diff purposes.
type Snapshot struct {
	RepoKey    string           `json:"repo_key"`
	SnapshotAt time.Time        `json:"snapshot_at"`
	Symbols    []SnapshotSymbol `json:"symbols"`
	Edges      []SnapshotEdge   `json:"edges"`
}

// stripQuotes removes surrounding double-quotes from AGE agtype strings.
func stripQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// buildSnapshot extracts Symbol vertices and semantic edges from in-memory graph data.
// Only Symbol vertices are included; CONTAINS and IMPORTS edges are skipped.
func buildSnapshot(vertices []vertexData, edges []edgeData) Snapshot {
	var symbols []SnapshotSymbol
	for _, v := range vertices {
		if v.Label != "Symbol" {
			continue
		}
		sym := SnapshotSymbol{
			Name:      v.Props["name"],
			Kind:      v.Props["kind"],
			File:      v.Props["file"],
			Community: atoiSafe(v.Props["community"]),
		}
		if c := atoiSafe(v.Props["complexity"]); c != 0 {
			sym.Complexity = c
		}
		symbols = append(symbols, sym)
	}

	var snapshotEdges []SnapshotEdge
	for _, e := range edges {
		if !semanticEdgeLabels[e.EdgeLabel] {
			continue
		}
		snapshotEdges = append(snapshotEdges, SnapshotEdge{
			From:  e.FromKey,
			To:    e.ToKey,
			Label: e.EdgeLabel,
		})
	}

	return Snapshot{
		Symbols: symbols,
		Edges:   snapshotEdges,
	}
}

// saveSnapshot saves a snapshot to PostgreSQL, keeping only the latest 5 per repo.
func saveSnapshot(ctx context.Context, store *Store, repoKey string, snap Snapshot) error {
	snap.RepoKey = repoKey
	snap.SnapshotAt = time.Now().UTC()

	symbolsJSON, err := json.Marshal(snap.Symbols)
	if err != nil {
		return fmt.Errorf("marshal symbols: %w", err)
	}
	edgesJSON, err := json.Marshal(snap.Edges)
	if err != nil {
		return fmt.Errorf("marshal edges: %w", err)
	}

	conn, err := store.acquireAGE(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	// Ensure the table exists.
	if _, err := conn.Exec(ctx, snapshotTableSQL); err != nil {
		return fmt.Errorf("ensure snapshot table: %w", err)
	}

	// Insert the new snapshot.
	_, err = conn.Exec(ctx, `
		INSERT INTO code_graph_snapshots (repo_key, snapshot_at, symbols, edges)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (repo_key, snapshot_at) DO UPDATE SET
		    symbols = EXCLUDED.symbols,
		    edges   = EXCLUDED.edges`,
		repoKey, snap.SnapshotAt, symbolsJSON, edgesJSON,
	)
	if err != nil {
		return fmt.Errorf("insert snapshot: %w", err)
	}

	// Prune: keep only the latest 5 snapshots per repo.
	_, err = conn.Exec(ctx, `
		DELETE FROM code_graph_snapshots
		WHERE repo_key = $1
		  AND snapshot_at NOT IN (
		      SELECT snapshot_at FROM code_graph_snapshots
		      WHERE repo_key = $1
		      ORDER BY snapshot_at DESC
		      LIMIT 5
		  )`, repoKey,
	)
	if err != nil {
		return fmt.Errorf("prune snapshots: %w", err)
	}

	return nil
}

// loadLatestSnapshot loads the most recent snapshot for a repo.
// Returns nil, nil if no snapshot exists.
func loadLatestSnapshot(ctx context.Context, store *Store, repoKey string) (*Snapshot, error) {
	conn, err := store.acquireAGE(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	var snap Snapshot
	var symbolsJSON, edgesJSON []byte

	err = conn.QueryRow(ctx, `
		SELECT repo_key, snapshot_at, symbols, edges
		FROM code_graph_snapshots
		WHERE repo_key = $1
		ORDER BY snapshot_at DESC
		LIMIT 1`, repoKey,
	).Scan(&snap.RepoKey, &snap.SnapshotAt, &symbolsJSON, &edgesJSON)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("query snapshot: %w", err)
	}

	if err := json.Unmarshal(symbolsJSON, &snap.Symbols); err != nil {
		return nil, fmt.Errorf("unmarshal symbols: %w", err)
	}
	if err := json.Unmarshal(edgesJSON, &snap.Edges); err != nil {
		return nil, fmt.Errorf("unmarshal edges: %w", err)
	}

	return &snap, nil
}
