package embeddings

import (
	"context"
	"fmt"
	"strings"
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

// extractQueryKeywords splits a natural language query into meaningful search
// terms by removing stopwords and short tokens. Returns lowercase terms >= 3 chars.
func ExtractQueryKeywords(query string) []string {
	stopwords := map[string]bool{
		"the": true, "and": true, "for": true, "that": true, "with": true,
		"this": true, "from": true, "are": true, "not": true, "have": true,
		"function": true, "method": true, "code": true, "file": true,
		"which": true, "where": true, "when": true, "how": true, "what": true,
	}
	var keywords []string
	seen := make(map[string]bool)
	for _, word := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}) {
		if len(word) >= 3 && !stopwords[word] && !seen[word] {
			seen[word] = true
			keywords = append(keywords, word)
		}
	}
	return keywords
}

// SearchBySymbolName searches the embedding index by symbol name and file path
// using keyword pattern matching. Supplements vector search for cases where the
// query keywords directly appear in function names (e.g. "init_llm" for "init LLM").
// Returns SearchResult with Source="keyword_name".
func (s *Store) SearchBySymbolName(
	ctx context.Context,
	repoKey string,
	keywords []string,
	language string,
	limit int,
) ([]SearchResult, error) {
	if len(keywords) == 0 {
		return nil, nil
	}
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}

	// Build ILIKE patterns: each keyword becomes %keyword%.
	patterns := make([]string, len(keywords))
	for i, kw := range keywords {
		patterns[i] = "%" + kw + "%"
	}

	// Match rows where symbol_name OR file_path contains any keyword.
	q := `
		SELECT file_path, symbol_name, symbol_kind, language, start_line
		FROM code_embeddings
		WHERE repo_key = $1
		  AND ($2 = '' OR language = $2)
		  AND (
		    symbol_name ILIKE ANY($3::text[])
		    OR file_path ILIKE ANY($3::text[])
		  )
		ORDER BY symbol_name
		LIMIT $4`

	rows, err := s.pool.Query(ctx, q, repoKey, language, patterns, limit)
	if err != nil {
		return nil, fmt.Errorf("symbol name search: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.FilePath, &r.SymbolName, &r.SymbolKind, &r.Language, &r.StartLine); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		r.RepoKey = repoKey
		r.Distance = 0.5 // nominal distance for keyword hits
		r.Source = "keyword_name"
		results = append(results, r)
	}
	return results, rows.Err()
}
