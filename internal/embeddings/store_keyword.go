package embeddings

import (
	"context"
	"fmt"
)

// FileLineHit is a file path + line number from keyword search.
type FileLineHit struct {
	FilePath string
	Line     int
}

const matchKeywordQuery = `
SELECT symbol_name, start_line
FROM code_embeddings
WHERE repo_key = $1 AND file_path = $2 AND start_line <= $3
ORDER BY start_line DESC
LIMIT 1`

// MatchKeywordHits maps keyword search hits to the nearest indexed symbol.
// For each hit, finds the symbol in the same file with start_line <= hit.Line
// (i.e., the function containing that line). Returns deduplicated KeywordHit results.
func (s *Store) MatchKeywordHits(ctx context.Context, repoKey string, hits []FileLineHit) ([]KeywordHit, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(hits))
	results := make([]KeywordHit, 0, len(hits))

	for _, hit := range hits {
		symbolName, startLine, ok, err := s.matchOneHit(ctx, repoKey, hit)
		if err != nil {
			return nil, fmt.Errorf("match keyword hit %s:%d: %w", hit.FilePath, hit.Line, err)
		}
		if !ok {
			continue
		}

		key := hit.FilePath + ":" + symbolName
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		results = append(results, KeywordHit{
			FilePath:   hit.FilePath,
			SymbolName: symbolName,
			Line:       startLine,
		})
	}

	return results, nil
}

// matchOneHit finds the nearest enclosing symbol for a single file:line hit.
// Returns (symbolName, startLine, found, error).
func (s *Store) matchOneHit(ctx context.Context, repoKey string, hit FileLineHit) (string, int, bool, error) {
	row := s.pool.QueryRow(ctx, matchKeywordQuery, repoKey, hit.FilePath, hit.Line)

	var symbolName string
	var startLine int
	if err := row.Scan(&symbolName, &startLine); err != nil {
		// pgx returns pgx.ErrNoRows when no row matches — treat as "not found".
		return "", 0, false, nil //nolint:nilerr // no match is not an error
	}
	return symbolName, startLine, true, nil
}
