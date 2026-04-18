package codegraph

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/anatolykoptev/go-code/internal/learnings"
)

// insightTemplates lists template IDs whose outputs are structural insights
// worth persisting. Point queries (who_calls, call_chain, most_connected) are
// omitted — they answer "where", not "what's structurally notable".
//
// NOTE: "community_changes" is not a real template ID. The closest equivalent
// is "graph_diff" whose rows are mixed-format. Only the two templates with
// stable column shapes are listed here.
var insightTemplates = map[string]struct{}{
	TemplateInsightSurprises: {},
	TemplateInsightDeadCode:  {},
}

const (
	// TemplateInsightSurprises is the template that surfaces hidden
	// cross-package coupling scored by community/pagerank distance.
	TemplateInsightSurprises = "surprises"

	// TemplateInsightDeadCode is the template that finds uncalled functions.
	TemplateInsightDeadCode = "dead_code"

	// minSurpriseScoreForPersist is the minimum surprise score required to
	// persist a finding. Edges with score < 5 are common cross-package calls
	// with no anomaly signal.
	minSurpriseScoreForPersist = 5

	// maxDedupeCheck is the number of existing records examined per symbol
	// when deciding whether to skip a duplicate.
	maxDedupeCheck = 10
)

// learningsStore is the narrow interface used by PersistInsights.
// *learnings.Store satisfies it; tests supply a stub.
type learningsStore interface {
	Upsert(ctx context.Context, r learnings.Record) error
	Nearest(ctx context.Context, repo, symbol string, k int) ([]learnings.Record, error)
}

// PersistInsights writes per-symbol learnings Records for structural insights
// surfaced by a QueryResult. It is a no-op when store is nil, the template is
// unsupported, or rows is empty.
//
// Returns the count of Records successfully upserted. All errors are swallowed
// via slog.Debug — the caller's user-visible response must never fail because
// persistence failed.
//
// Supported templates:
//   - "surprises": emits two records per edge (FromName + ToName) for
//     post-processed rows with Score >= minSurpriseScoreForPersist.
//     Post-processed cols: [FromName, FromFile, ToName, ToFile, Score, Reasons]
//   - "dead_code": emits one record per row (symbol col 0, AGE node string).
func PersistInsights(ctx context.Context, store learningsStore, repoKey, template string, rows [][]string) int {
	if store == nil {
		return 0
	}
	if _, ok := insightTemplates[template]; !ok {
		return 0
	}
	if len(rows) == 0 {
		return 0
	}

	switch template {
	case TemplateInsightSurprises:
		return persistSurprises(ctx, store, repoKey, rows)
	case TemplateInsightDeadCode:
		return persistDeadCode(ctx, store, repoKey, rows)
	default:
		return 0
	}
}

// persistSurprises handles the already-post-processed "surprises" rows.
// Row layout (from postProcessSurprises output):
//
//	[0] FromName  [1] FromFile  [2] ToName  [3] ToFile  [4] Score  [5] Reasons
func persistSurprises(ctx context.Context, store learningsStore, repoKey string, rows [][]string) int {
	n := 0
	for _, row := range rows {
		if len(row) < 6 {
			continue
		}
		score, err := strconv.Atoi(row[4])
		if err != nil {
			slog.Debug("persist_insights: parse score",
				slog.String("raw", row[4]), slog.Any("error", err))
			continue
		}
		if score < minSurpriseScoreForPersist {
			continue
		}
		note := row[5]
		for _, sym := range [2]struct{ name, file string }{{row[0], row[1]}, {row[2], row[3]}} {
			if sym.name == "" {
				continue
			}
			if dedupeExists(ctx, store, repoKey, sym.name, "hidden_dep") {
				continue
			}
			if uErr := store.Upsert(ctx, learnings.Record{
				Repo: repoKey, Symbol: sym.name, Flag: "hidden_dep", Note: note,
			}); uErr != nil {
				slog.Debug("persist_insights: upsert", slog.String("symbol", sym.name), slog.Any("error", uErr))
				continue
			}
			n++
		}
	}
	return n
}

// persistDeadCode handles the "dead_code" rows.
// Col 0 is an AGE node string; extractNodeName parses the symbol name from it.
func persistDeadCode(ctx context.Context, store learningsStore, repoKey string, rows [][]string) int {
	n := 0
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		name := extractNodeName(row[0])
		if name == "" {
			continue
		}
		if dedupeExists(ctx, store, repoKey, name, "dead_code_candidate") {
			continue
		}
		if uErr := store.Upsert(ctx, learnings.Record{
			Repo: repoKey, Symbol: name, Flag: "dead_code_candidate",
		}); uErr != nil {
			slog.Debug("persist_insights: upsert", slog.String("symbol", name), slog.Any("error", uErr))
			continue
		}
		n++
	}
	return n
}

// dedupeExists returns true when the store already holds a record for
// (repoKey, symbol) with the given flag. Errors are treated as "no match"
// (safe — it may cause a duplicate write, but never data loss).
func dedupeExists(ctx context.Context, store learningsStore, repoKey, symbol, flag string) bool {
	existing, err := store.Nearest(ctx, repoKey, symbol, maxDedupeCheck)
	if err != nil {
		slog.Debug("persist_insights: nearest", slog.String("symbol", symbol), slog.Any("error", err))
		return false
	}
	for _, r := range existing {
		if r.Flag == flag {
			return true
		}
	}
	return false
}
