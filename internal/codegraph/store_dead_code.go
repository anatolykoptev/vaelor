package codegraph

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/anatolykoptev/go-kit/rerank"
)

// deadCodeBuildRerankTimeout bounds build-time dead_code scoring. There is no
// user-facing deadline here (runs inside IndexRepo); ~100 docs take 35-40s on ARM.
const deadCodeBuildRerankTimeout = 90 * time.Second

// maxOrphanCandidates is the upper bound on dead_code candidates scored per
// repo. It also sizes the shared rerank client's MaxDocs (rerank.go) so the
// build-time path — which sends ALL candidates, not a pre-filtered 20 — has
// every candidate scored by the server rather than truncated to a fabricated
// zero score. Keep these in sync.
const maxOrphanCandidates = 200

// orphanCandidateLimit returns a repo-relative limit for dead_code scoring.
// Scales with symbol count but stays in [50, maxOrphanCandidates] to bound
// reranker time (~90s max).
func orphanCandidateLimit(symbolCount int) int {
	const minLimit = 50
	l := symbolCount / 60 // ~1 candidate per 60 symbols
	if l < minLimit {
		return minLimit
	}
	if l > maxOrphanCandidates {
		return maxOrphanCandidates
	}
	return l
}

// buildDeadCodeScoringQuery generates a Cypher query that pre-scores orphan
// functions with a composite signal before sending to the CE reranker:
//   - penalizes entrypoints (main), test/benchmark/example functions
//   - penalizes test/evaluation/example file paths
//   - uses complexity as the primary magnitude
//
// This ensures the reranker sees the most interesting production dead code
// regardless of repo size, not a random or complexity-only slice.
func buildDeadCodeScoringQuery(limit int) string {
	return fmt.Sprintf(`
		MATCH (s:Symbol) WHERE s.kind = 'function'
		OPTIONAL MATCH (caller:Symbol)-[:CALLS]->(s)
		WITH s, caller WHERE caller IS NULL
		WITH s,
		  (CASE WHEN s.name = 'main' THEN 0.05
		        WHEN s.name STARTS WITH 'Test' THEN 0.1
		        WHEN s.name STARTS WITH 'test_' THEN 0.1
		        WHEN s.name STARTS WITH 'setup_' OR s.name STARTS WITH 'teardown_' THEN 0.2
		        WHEN s.name STARTS WITH 'example_' OR s.name STARTS WITH 'Example' THEN 0.2
		        WHEN s.name STARTS WITH 'Benchmark' THEN 0.2
		        ELSE 1.0 END) *
		  (CASE WHEN s.file CONTAINS '_test.go' OR s.file CONTAINS '_test.py' THEN 0.1
		        WHEN s.file CONTAINS '/test/' OR s.file CONTAINS '/tests/' THEN 0.15
		        WHEN s.file CONTAINS 'evaluation/' THEN 0.25
		        WHEN s.file CONTAINS 'examples/' THEN 0.25
		        WHEN s.file CONTAINS '/scripts/' THEN 0.3
		        WHEN s.file CONTAINS 'benchmark' THEN 0.2
		        ELSE 1.0 END) *
		  toFloat(s.complexity) AS pre_score
		RETURN s ORDER BY pre_score DESC LIMIT %d`, limit)
}

// ceScoreToProbability converts a raw CE relevance logit to dead-code probability [0..1].
// Sigmoid: higher probability = more likely genuine dead code.
// Example: raw -1.75 -> 0.15 (unlikely), raw -0.5 -> 0.38 (moderate).
func ceScoreToProbability(rawScore float64) float32 {
	return float32(1.0 / (1.0 + math.Exp(-rawScore)))
}

// orphanCandidate pairs a dead_code Cypher row with its parsed complexity.
type orphanCandidate struct {
	row []string
	cx  int
}

// orphansByComplexity parses each row's complexity and returns the candidates
// sorted by complexity DESC. Empty rows are skipped.
func orphansByComplexity(rows [][]string) []orphanCandidate {
	candidates := make([]orphanCandidate, 0, len(rows))
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		candidates = append(candidates, orphanCandidate{row: row, cx: parseIntField(row[0], "complexity")})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].cx > candidates[j].cx
	})
	return candidates
}

// ScoreDeadCodeCandidates finds orphan functions in the graph, reranks
// them via the CE reranker, and persists scores to code_dead_code_scores.
// Non-fatal: logging only on any error. Scores are available immediately
// for the next dead_code query on this repo.
func (s *Store) ScoreDeadCodeCandidates(ctx context.Context, gname, repoKey string, symbolCount int) error {
	// Preflight: short-circuit when graph was never indexed, avoiding postgres ERROR logs.
	// IsGraphMissingError guard below remains as a race fallback.
	if err := s.EnsureGraphExistsForRead(ctx, gname); err != nil {
		if errors.Is(err, ErrGraphNotIndexed) {
			recordGraphMissing("dead_code")
			slog.Debug("codegraph: dead_code scoring skipped — graph absent (preflight)", slog.String("graph", gname))
			return nil
		}
		return err
	}

	// Step 1: Query orphan functions with pre-scored ordering.
	limit := orphanCandidateLimit(symbolCount)
	query := buildDeadCodeScoringQuery(limit)
	rows, err := s.ExecCypher(ctx, gname, query, 1)
	if err != nil {
		// Graph may have been dropped between EnsureGraph and here; treat as no-op.
		if IsGraphMissingError(err) {
			s.existsCache.Forget(gname)
			recordGraphMissing("dead_code")
			slog.Debug("codegraph: dead_code scoring skipped — graph absent", slog.String("graph", gname))
			return nil
		}
		return fmt.Errorf("query orphans: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}
	slog.Info("codegraph: scoring dead_code candidates",
		slog.String("repo", repoKey), slog.Int("limit", limit), slog.Int("candidates", len(rows)))

	// Step 2: Sort candidates by complexity DESC (highest complexity first —
	// most interesting dead code targets). No cap: all Cypher candidates go to
	// the reranker. At build time there is no user-facing timeout — 100 docs
	// take ~10s, well within IndexRepo total. Better coverage → better ranking.
	candidates := orphansByComplexity(rows)
	if len(candidates) == 0 || !rerankClient.Available() {
		return nil // no reranker configured — nothing to score (non-fatal)
	}

	// Step 3: Build documents and call reranker with a generous timeout —
	// this runs at graph build time, not in a user request, so the shared
	// client's per-call deadline is overridden via a 90s ctx (rerankClient has
	// Timeout=0; 100 docs take ~35-40s on ARM).
	docs := make([]rerank.Doc, len(candidates))
	for i, c := range candidates {
		docs[i] = rerank.Doc{Text: formatDeadCodeDoc(c.row[0])}
	}
	rerankCtx, cancel := context.WithTimeout(context.Background(), deadCodeBuildRerankTimeout)
	defer cancel()
	// RerankWithResult exposes a typed Status so build-time scoring persists
	// ONLY genuine scores — a skipped/degraded call must not write Score=0 rows.
	res, rerankErr := rerankClient.RerankWithResult(rerankCtx, rerankDeadCodeQuery, docs)
	if rerankErr != nil || res == nil ||
		res.Status == rerank.StatusSkipped || res.Status == rerank.StatusDegraded {
		slog.Warn("codegraph: dead_code reranker unavailable, skipping pre-score",
			slog.String("repo", repoKey), slog.Any("error", rerankErr))
		return nil // non-fatal
	}

	// Step 4: Persist scores to PostgreSQL.
	if err := s.upsertDeadCodeScores(ctx, repoKey, candidates, res.Scored); err != nil {
		return err
	}

	slog.Info("codegraph: dead_code scores persisted",
		slog.String("repo", repoKey), slog.Int("scored", len(res.Scored)))
	return nil
}

// upsertDeadCodeScores writes each scored candidate's CE probability to
// code_dead_code_scores (ON CONFLICT updates for re-indexed repos). OrigRank
// maps each scored doc back to its candidate index. Per-row upsert failures are
// logged and skipped; only acquiring the connection is fatal.
func (s *Store) upsertDeadCodeScores(ctx context.Context, repoKey string, candidates []orphanCandidate, scored []rerank.Scored) error {
	conn, err := s.acquireAGE(ctx)
	if err != nil {
		return fmt.Errorf("acquire: %w", err)
	}
	defer conn.Release()

	for _, sc := range scored {
		if sc.OrigRank < 0 || sc.OrigRank >= len(candidates) {
			continue
		}
		row0 := candidates[sc.OrigRank].row[0]
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
			repoKey, name, file, ceScoreToProbability(float64(sc.Score)))
		if uErr != nil {
			slog.Warn("codegraph: upsert dead_code score",
				slog.String("name", name), slog.Any("error", uErr))
		}
	}
	return nil
}

// LoadDeadCodeScore returns the pre-computed CE dead-code probability [0..1].
// Higher = more likely genuine dead code. Returns (0, false) if not scored.
func (s *Store) LoadDeadCodeScore(ctx context.Context, root, name, file string) (float32, bool) {
	// Normalise: full paths must be converted to graph key (sha-prefix hash).
	repoKey := GraphNameFor(root)
	conn, err := s.acquireAGE(ctx)
	if err != nil {
		return 0, false
	}
	defer conn.Release()

	var score float32
	err = conn.QueryRow(ctx,
		"SELECT score FROM code_dead_code_scores WHERE repo_key = $1 AND name = $2 AND file = $3",
		repoKey, name, file).Scan(&score)
	if err != nil {
		return 0, false
	}
	return score, true
}

// LoadDeadCodeScores fetches pre-computed reranker scores for dead_code rows.
// Returns rows sorted by score DESC if scores exist; nil if no scores stored.
func (s *Store) LoadDeadCodeScores(ctx context.Context, repoKey string, rows [][]string) [][]string {
	if len(rows) == 0 {
		return nil
	}

	conn, err := s.acquireAGE(ctx)
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


// DeadCodeCandidate is a high-confidence dead code function from code_dead_code_scores.
type DeadCodeCandidate struct {
	Name  string
	File  string
	Score float32
}

// LoadTopDeadCodeCandidates returns the top N dead-code candidates for a repo
// with CE probability above minScore (0..1).
// Returns nil, nil when no candidates exist (graph not indexed or no orphans).
func (s *Store) LoadTopDeadCodeCandidates(
	ctx context.Context,
	repoKey string,
	minScore float32,
	limit int,
) ([]DeadCodeCandidate, error) {
	conn, err := s.acquireAGE(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire: %w", err)
	}
	defer conn.Release()

	rows, err := conn.Query(ctx, `
		SELECT name, file, score
		FROM code_dead_code_scores
		WHERE repo_key = $1 AND score >= $2
		ORDER BY score DESC
		LIMIT $3`,
		repoKey, minScore, limit)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	defer rows.Close()

	var candidates []DeadCodeCandidate
	for rows.Next() {
		var c DeadCodeCandidate
		if err := rows.Scan(&c.Name, &c.File, &c.Score); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		candidates = append(candidates, c)
	}
	return candidates, rows.Err()
}
