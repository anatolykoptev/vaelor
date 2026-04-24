package codegraph

import (
	"net/http"
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"
)

// deadCodeCypher finds orphan functions (no callers).
const deadCodeCypher = "MATCH (s:Symbol) WHERE s.kind = 'function' " +
	"OPTIONAL MATCH (caller:Symbol)-[:CALLS]->(s) " +
	"WITH s, caller WHERE caller IS NULL RETURN s ORDER BY toFloat(s.complexity) DESC LIMIT 100"

// ScoreDeadCodeCandidates finds orphan functions in the graph, reranks
// them via the CE reranker, and persists scores to code_dead_code_scores.
// Non-fatal: logging only on any error. Scores are available immediately
// for the next dead_code query on this repo.
func (s *Store) ScoreDeadCodeCandidates(ctx context.Context, gname, repoKey string) error {
	// Step 1: Query orphan functions.
	rows, err := s.ExecCypher(ctx, gname, deadCodeCypher, 1)
	if err != nil {
		return fmt.Errorf("query orphans: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}
	slog.Info("codegraph: scoring dead_code candidates",
		slog.String("repo", repoKey), slog.Int("candidates", len(rows)))

	// Step 2: Pre-filter top-20 by complexity (highest complexity first —
	// most interesting dead code targets).
	type rowWithCx struct {
		row []string
		cx  int
	}
	candidates := make([]rowWithCx, 0, len(rows))
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		cx := parseIntField(row[0], "complexity")
		candidates = append(candidates, rowWithCx{row, cx})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].cx > candidates[j].cx
	})
	// No cap: all Cypher candidates go to the reranker.
	// At build time there is no user-facing timeout — 100 docs takes ~10s,
	// well within IndexRepo total. Better coverage -> better ranking quality.

	// Step 3: Build document strings and call reranker with generous timeout —
	// this runs at graph build time, not in a user request.
	docs := make([]string, len(candidates))
	for i, c := range candidates {
		docs[i] = formatDeadCodeDoc(c.row[0])
	}
	// Use a dedicated HTTP client with longer timeout for build-time scoring.
	// rerankHTTPClient has 35s — insufficient for 100 docs (~35-40s on ARM).
	buildClient := &http.Client{Timeout: 90 * time.Second}
	rerankCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	scored, rerankErr := callRerankerWithClient(rerankCtx, buildClient, rerankDeadCodeQuery, docs)
	if rerankErr != nil {
		slog.Warn("codegraph: dead_code reranker unavailable, skipping pre-score",
			slog.String("repo", repoKey), slog.Any("error", rerankErr))
		return nil // non-fatal
	}

	// Step 4: Persist scores to PostgreSQL.
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire: %w", err)
	}
	defer conn.Release()

	// Upsert scores (ON CONFLICT updates the score for re-indexed repos).
	for _, r := range scored.Results {
		if r.Index >= len(candidates) {
			continue
		}
		row0 := candidates[r.Index].row[0]
		name := extractFieldRerank(row0, "name")
		file := extractFieldRerank(row0, "file")
		if name == "" || file == "" {
			continue
		}
		_, uErr := conn.Exec(ctx, `
			INSERT INTO code_dead_code_scores (repo_key, name, file, score, scored_at)
			VALUES ($1, $2, $3, $4, now())
			ON CONFLICT (repo_key, name, file) DO UPDATE
			SET score = EXCLUDED.score, scored_at = EXCLUDED.scored_at`,
			repoKey, name, file, float32(r.RelevanceScore))
		if uErr != nil {
			slog.Warn("codegraph: upsert dead_code score",
				slog.String("name", name), slog.Any("error", uErr))
		}
	}

	slog.Info("codegraph: dead_code scores persisted",
		slog.String("repo", repoKey), slog.Int("scored", len(scored.Results)))
	return nil
}

// LoadDeadCodeScores fetches pre-computed reranker scores for dead_code rows.
// Returns rows sorted by score DESC if scores exist; nil if no scores stored.
func (s *Store) LoadDeadCodeScores(ctx context.Context, repoKey string, rows [][]string) [][]string {
	if len(rows) == 0 {
		return nil
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil
	}
	defer conn.Release()

	// Fetch all scores for this repo.
	scoreRows, err := conn.Query(ctx,
		"SELECT name, file, score FROM code_dead_code_scores WHERE repo_key = $1",
		repoKey)
	if err != nil {
		return nil
	}
	defer scoreRows.Close()

	scoreMap := make(map[string]float32)
	for scoreRows.Next() {
		var name, file string
		var score float32
		if err := scoreRows.Scan(&name, &file, &score); err == nil {
			scoreMap[name+":"+file] = score
		}
	}
	if len(scoreMap) == 0 {
		return nil // no pre-computed scores yet
	}

	// Score each row.
	type rowWithScore struct {
		row   []string
		score float32
		has   bool
	}
	sc := make([]rowWithScore, 0, len(rows))
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		name := extractFieldRerank(row[0], "name")
		file := extractFieldRerank(row[0], "file")
		score, has := scoreMap[name+":"+file]
		sc = append(sc, rowWithScore{row, score, has})
	}

	// Sort: scored rows first (by score DESC), then unscored.
	sort.SliceStable(sc, func(i, j int) bool {
		if sc[i].has != sc[j].has {
			return sc[i].has
		}
		if sc[i].has && sc[j].has {
			return sc[i].score > sc[j].score
		}
		return false
	})

	result := make([][]string, 0, len(sc))
	for _, item := range sc {
		result = append(result, item.row)
	}
	// Return at most rerankTopN rows.
	if len(result) > rerankTopN {
		result = result[:rerankTopN]
	}
	return result
}
