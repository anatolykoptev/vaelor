package codegraph

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/anatolykoptev/go-kit/rerank"
)

// deadCodeBuildRerankTimeout bounds build-time dead_code scoring. There is no
// user-facing deadline here (runs inside IndexRepo); ~100 docs take 35-40s on ARM.
const deadCodeBuildRerankTimeout = 90 * time.Second

// rerankServerMaxDocs is the per-request document cap enforced by the embed
// server's /v1/rerank endpoint (RERANK_MAX_INPUT_DOCS, default 32, aligned with
// EMBED_MAX_INPUT_ARRAY) to bound cross-encoder attention-scratch allocations.
// Requests above it are rejected with HTTP 400. The build-time path sends ALL
// candidates (up to maxOrphanCandidates), so it splits them into server-sized
// chunks — exactly as the embed client chunks embeddings — and merges the
// scores, which are per-(query,doc) independent and thus comparable across
// chunks under a single query.
const rerankServerMaxDocs = 32

// maxOrphanCandidates is the upper bound on dead_code candidates scored per
// repo. It also sizes the shared rerank client's MaxDocs (rerank.go) so the
// build-time path — which sends ALL candidates, not a pre-filtered 20 — has
// every candidate scored by the server rather than truncated to a fabricated
// zero score. Keep these in sync.
const maxOrphanCandidates = 200

// deadCodeScorePruneChunkSize bounds each positive-IN DELETE chunk in
// pruneStaleDeadCodeScores, mirroring internal/embeddings/store.go's
// intraKeyOrphanChunkSize precedent: the codebase already learned (and
// documented, #201) that an unbounded anti-join / single giant IN-list risks
// statement_timeout on this 4-core ARM box, and that chunking a POSITIVE IN is
// safe (each chunk only targets its own disjoint slice of confirmed-stale
// rows — unlike chunking a NOT-IN anti-join, which would cause cross-chunk
// data loss).
const deadCodeScorePruneChunkSize = 500

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

	// Step 0: prune scores for functions that are no longer orphans (or were
	// deleted). Runs regardless of rerank availability — pruning depends only on
	// the live graph, not on new scoring. Non-fatal.
	if pruned, pErr := s.pruneStaleDeadCodeScores(ctx, gname, repoKey); pErr != nil {
		slog.Warn("codegraph: dead_code prune failed (non-fatal)",
			slog.String("repo", repoKey), slog.Any("error", pErr))
	} else if pruned > 0 {
		deadCodeScoresPrunedTotal.Add(float64(pruned))
		slog.Info("codegraph: pruned stale dead_code scores",
			slog.String("repo", repoKey), slog.Int64("pruned", pruned))
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

	// Step 3: Rerank in server-sized batches (the endpoint caps each request at
	// rerankServerMaxDocs). Build-time scoring is not user-facing, so a generous
	// 90s ctx overrides the shared client's per-call deadline.
	rerankCtx, cancel := context.WithTimeout(context.Background(), deadCodeBuildRerankTimeout)
	defer cancel()
	scored, anyScored := s.rerankCandidateBatches(rerankCtx, repoKey, candidates)
	if !anyScored {
		slog.Warn("codegraph: dead_code reranker unavailable, skipping pre-score",
			slog.String("repo", repoKey))
		return nil // non-fatal
	}

	// Step 4: Persist scores to PostgreSQL.
	if err := s.upsertDeadCodeScores(ctx, repoKey, candidates, scored); err != nil {
		return err
	}

	slog.Info("codegraph: dead_code scores persisted",
		slog.String("repo", repoKey), slog.Int("scored", len(scored)))
	return nil
}

// rerankCandidateBatches scores all candidates in rerankServerMaxDocs-sized
// chunks (the embed server rejects larger /v1/rerank requests with HTTP 400) and
// returns the merged Scored slice with OrigRank globalised back into candidates.
// A failed/degraded batch is logged, counted (outcome="skipped"), and skipped
// without aborting the rest; anyScored reports whether at least one batch
// produced genuine scores.
//
// Partial-coverage note: persistence is ON CONFLICT DO UPDATE with no prior-row
// cleanup, so a skipped batch leaves its candidates' PRIOR scores in place while
// the rest refresh. This is intentional (stale ranking hint ≥ none); the
// code_dead_code_rerank_batch_total{outcome="skipped"} counter makes such
// partial-coverage indexes observable rather than silent.
func (s *Store) rerankCandidateBatches(ctx context.Context, repoKey string, candidates []orphanCandidate) ([]rerank.Scored, bool) {
	scored := make([]rerank.Scored, 0, len(candidates))
	anyScored := false
	for start := 0; start < len(candidates); start += rerankServerMaxDocs {
		end := min(start+rerankServerMaxDocs, len(candidates))
		chunk := candidates[start:end]

		docs := make([]rerank.Doc, len(chunk))
		for i, c := range chunk {
			docs[i] = rerank.Doc{Text: formatDeadCodeDoc(c.row[0])}
		}

		// RerankWithResult exposes a typed Status so build-time scoring persists
		// ONLY genuine scores — a skipped/degraded batch must not write Score=0.
		res, err := rerankClient.RerankWithResult(ctx, rerankDeadCodeQuery, docs)
		if err != nil || res == nil ||
			res.Status == rerank.StatusSkipped || res.Status == rerank.StatusDegraded {
			recordRerankBatch("skipped")
			slog.Warn("codegraph: dead_code rerank batch failed, skipping batch",
				slog.String("repo", repoKey), slog.Int("batch_start", start), slog.Any("error", err))
			continue
		}
		recordRerankBatch("ok")
		anyScored = true
		for _, sc := range res.Scored {
			if sc.OrigRank < 0 || sc.OrigRank >= len(chunk) {
				continue
			}
			sc.OrigRank += start // globalise the index back into candidates
			scored = append(scored, sc)
		}
	}
	return scored, anyScored
}

// pruneStaleDeadCodeScores deletes code_dead_code_scores rows whose function is
// no longer a current orphan in the live graph — a function that gained an
// incoming CALLS edge (real refactor OR the typed-enrichment fix, BUG A) or was
// deleted outright. Without this, code_dead_code_scores only ever grows: a
// once-orphan function's stale score survives forever and code_health keeps
// counting it as dead (the code_health over-count residual the P2b canary
// caught). Rows for functions that are STILL orphans are kept — including ones
// whose rerank batch was skipped this round — so this does not weaken the
// skipped-batch resilience rerankCandidateBatches documents. Returns rows
// deleted. Non-fatal to the caller.
//
// Design (pr-review-council BLOCKED #295 remediation): the original version
// computed toDelete via an UNBOUNDED anti-join keyed on a best-effort-parsed
// keep-set — any orphan vertex whose name/file failed extraction silently
// dropped OUT of the keep-set, so its still-live row got deleted; an
// all-fail/empty result wiped the WHOLE repo's score history silently. This
// version instead: (1) FAILS CLOSED — any unparseable orphan vertex aborts the
// entire prune (no DELETE at all) rather than narrowing the keep-set, because
// here a skipped/dropped keep-set entry is destructive (contrast
// upsertDeadCodeScores, where skipping a candidate is merely "not inserted" —
// benign); (2) computes toDelete = (stored keys) MINUS (current-orphan keys)
// in Go, then issues a bounded, CHUNKED POSITIVE-IN delete — the same pattern
// internal/embeddings/store.go's DeleteExplicitOrphans/deleteExplicitOrphanChunk
// already established and documented (intraKeyOrphanChunkSize) after learning
// that an unbounded repo-wide anti-join risks statement_timeout at scale.
func (s *Store) pruneStaleDeadCodeScores(ctx context.Context, gname, repoKey string) (int64, error) {
	// 1. Current orphan keys — light scalar RETURN (name,file), not the full
	//    vertex blob (cheaper to transfer and parse at scale than RETURN s).
	const orphanKeysQuery = `
		MATCH (s:Symbol) WHERE s.kind = 'function'
		OPTIONAL MATCH (caller:Symbol)-[:CALLS]->(s)
		WITH s, caller WHERE caller IS NULL
		RETURN s.name, s.file`
	rows, err := s.ExecCypher(ctx, gname, orphanKeysQuery, 2)
	if err != nil {
		if IsGraphMissingError(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("query orphan keys: %w", err)
	}

	// 2. Build the protected keep-set. FAIL CLOSED: the keep-set is what
	//    protects still-orphan rows from deletion, so any unparseable orphan
	//    vertex means we CANNOT trust it — abort the whole prune (no DELETE)
	//    rather than silently narrowing the set and deleting a live orphan's
	//    row. A legitimately EMPTY orphan set (zero rows returned — every
	//    function has a caller) is NOT a parse failure and does not abort:
	//    it correctly flows through to step 4 as "delete everything stored"
	//    (see TestPruneStaleDeadCodeScores_ZeroOrphansWipesRepoRows).
	const sep = "\x00" // in-Go map key only — never sent to Postgres as text
	orphanSet := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if len(row) < 2 {
			deadCodeScorePruneAbortedTotal.Inc()
			slog.Warn("codegraph: dead_code prune aborted — malformed orphan row",
				slog.String("repo", repoKey))
			return 0, nil
		}
		name := strings.Trim(row[0], `"`)
		file := strings.Trim(row[1], `"`)
		if name == "" || file == "" {
			deadCodeScorePruneAbortedTotal.Inc()
			slog.Warn("codegraph: dead_code prune aborted — unparseable orphan vertex (name/file empty)",
				slog.String("repo", repoKey))
			return 0, nil
		}
		orphanSet[name+sep+file] = struct{}{}
	}

	conn, err := s.acquireAGE(ctx)
	if err != nil {
		return 0, fmt.Errorf("acquire: %w", err)
	}
	defer conn.Release()

	// 3. Load this repo's stored score keys.
	stored, err := conn.Query(ctx,
		`SELECT name, file FROM code_dead_code_scores WHERE repo_key = $1`, repoKey)
	if err != nil {
		return 0, fmt.Errorf("load stored keys: %w", err)
	}
	type nf struct{ name, file string }
	var storedKeys []nf
	for stored.Next() {
		var n, f string
		if scanErr := stored.Scan(&n, &f); scanErr != nil {
			stored.Close()
			return 0, fmt.Errorf("scan stored key: %w", scanErr)
		}
		storedKeys = append(storedKeys, nf{n, f})
	}
	stored.Close()
	if err := stored.Err(); err != nil {
		return 0, fmt.Errorf("iterate stored keys: %w", err)
	}

	// 4. toDelete = stored keys whose function is no longer a current orphan.
	var delNames, delFiles []string
	for _, k := range storedKeys {
		if _, stillOrphan := orphanSet[k.name+sep+k.file]; !stillOrphan {
			delNames = append(delNames, k.name)
			delFiles = append(delFiles, k.file)
		}
	}
	if len(delNames) == 0 {
		return 0, nil
	}
	// Observability: a full-table wipe for a repo is legitimate (repo has zero
	// current orphans) but rare — never let it be silent.
	if len(delNames) == len(storedKeys) {
		slog.Warn("codegraph: dead_code prune removing ALL stored scores for repo (zero current orphans)",
			slog.String("repo", repoKey), slog.Int("rows", len(delNames)))
	}

	// 5. Chunked positive-IN delete (mirror embeddings intraKeyOrphanChunkSize).
	var total int64
	for start := 0; start < len(delNames); start += deadCodeScorePruneChunkSize {
		end := min(start+deadCodeScorePruneChunkSize, len(delNames))
		n, dErr := s.deleteStaleScoreChunk(ctx, conn, repoKey, delNames[start:end], delFiles[start:end])
		if dErr != nil {
			return total, dErr
		}
		total += n
	}
	return total, nil
}

// deleteStaleScoreChunk issues one positive-IN DELETE for a single chunk of up
// to deadCodeScorePruneChunkSize confirmed-stale (name, file) pairs — mirrors
// internal/embeddings/store.go's deleteExplicitOrphanChunk. Unlike a NOT-IN
// anti-join (which deletes everything NOT in the chunk — cross-chunk data loss
// once total rows exceed the chunk size), this DELETE targets ONLY the rows
// whose keys appear in the supplied slice, so chunking is safe: each chunk
// deletes a disjoint subset of the full stale set; no live row is ever
// collateral. Takes the already-acquired AGE connection (code_dead_code_scores
// lives in ag_catalog, reachable only via a connection with the AGE
// search_path applied — acquireAGE already did that in the caller).
//
// SQL shape:
//
//	DELETE FROM code_dead_code_scores
//	WHERE repo_key = $1 AND (name, file) IN (VALUES ($2,$3), ($4,$5), ...)
func (s *Store) deleteStaleScoreChunk(ctx context.Context, conn *pgxpool.Conn, repoKey string, names, files []string) (int64, error) {
	n := len(names)
	if n == 0 {
		return 0, nil
	}
	var b strings.Builder
	b.WriteString("DELETE FROM code_dead_code_scores WHERE repo_key = $1 AND (name, file) IN (VALUES ")
	args := make([]any, 0, 1+n*2)
	args = append(args, repoKey)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		p1 := 2 + i*2
		p2 := p1 + 1
		fmt.Fprintf(&b, "($%d,$%d)", p1, p2)
		args = append(args, names[i], files[i])
	}
	b.WriteByte(')')
	tag, err := conn.Exec(ctx, b.String(), args...)
	if err != nil {
		return 0, fmt.Errorf("deleteStaleScoreChunk: %w", err)
	}
	return tag.RowsAffected(), nil
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
