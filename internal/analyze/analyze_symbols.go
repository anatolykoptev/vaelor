package analyze

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// Symbol scoring constants.
const (
	// Match quality weights (Signal 1).
	matchExact   = 100
	matchPrefix  = 50
	matchContain = 25
	matchFuzzy   = 10

	// Visibility weights (Signal 2).
	visExported   = 30
	visUnexported = 10

	// Kind weights (Signal 3).
	kindStructWeight = 25
	kindFuncWeight   = 20
	kindMethodWeight = 15
	kindConstWeight  = 10
	kindOtherWeight  = 5
)

// scoreSymbol computes a per-symbol relevance score from three cheap signals:
// match quality (exact > prefix > contains > fuzzy), visibility (exported > unexported),
// and kind weight (struct/interface > func/type > method > const > other).
// isWildcard should be true for "*" or "" queries to skip match quality scoring.
func scoreSymbol(sym *parser.Symbol, query string, isWildcard bool) float64 {
	var score float64

	if !isWildcard {
		lowerName := strings.ToLower(sym.Name)
		lowerQuery := strings.ToLower(query)
		switch {
		case lowerName == lowerQuery:
			score += matchExact
		case strings.HasPrefix(lowerName, lowerQuery):
			score += matchPrefix
		case strings.Contains(lowerName, lowerQuery):
			score += matchContain
		default:
			score += matchFuzzy
		}
	}

	if len(sym.Name) > 0 && unicode.IsUpper(rune(sym.Name[0])) {
		score += visExported
	} else {
		score += visUnexported
	}

	switch sym.Kind {
	case parser.KindStruct, parser.KindInterface, parser.KindClass:
		score += kindStructWeight
	case parser.KindFunction, parser.KindType:
		score += kindFuncWeight
	case parser.KindMethod:
		score += kindMethodWeight
	case parser.KindConst:
		score += kindConstWeight
	default:
		score += kindOtherWeight
	}

	return score
}

const defaultSymbolLimit = 100
const maxSymbolLimit = 500

// SearchSymbols searches for symbols matching the query across the repository.
// Files are ranked by fusion scoring (BM25F + PageRank + exact match) so that
// symbols from structurally important files appear first when results are truncated.
func SearchSymbols(ctx context.Context, input SymbolSearchInput) ([]*parser.Symbol, error) {
	var langs []string
	if input.Language != "" {
		langs = []string{input.Language}
	}

	ingestResult, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         input.Root,
		Languages:    langs,
		MaxFileBytes: defaultMaxFileBytes,
		ExcludeTests: true,
	})
	if err != nil {
		return nil, fmt.Errorf("ingest repo: %w", err)
	}

	parseResults := parseFilesParallel(ctx, ingestResult.Files, input.IncludeBody, nil)

	pattern, err := wildcardToRegexp(input.Query)
	if err != nil {
		return nil, fmt.Errorf("invalid query pattern: %w", err)
	}

	limit := input.Limit
	if limit <= 0 {
		limit = defaultSymbolLimit
	}
	if limit > maxSymbolLimit {
		limit = maxSymbolLimit
	}

	queryTerms := extractQueryTerms(input.Query)
	rankedFiles, fileScores := prioritizeFilesWithScores(input.Root, ingestResult.Files, parseResults, queryTerms)

	parseByPath := make(map[string]fileParseResult, len(parseResults))
	for _, pr := range parseResults {
		if pr.file != nil {
			parseByPath[pr.file.RelPath] = pr
		}
	}

	isWildcard := input.Query == "" || input.Query == "*"
	hardcap := limit * 5
	type scoredSymbol struct {
		sym       *parser.Symbol
		fileScore float64
		symScore  float64
	}
	candidates := make([]scoredSymbol, 0, hardcap)
	for _, f := range rankedFiles {
		pr, ok := parseByPath[f.RelPath]
		if !ok || pr.result == nil {
			continue
		}
		fs := fileScores[f.RelPath]
		for _, sym := range pr.result.Symbols {
			if !matchesSymbol(sym, pattern, input.Kind) {
				continue
			}
			candidates = append(candidates, scoredSymbol{
				sym:       sym,
				fileScore: fs,
				symScore:  scoreSymbol(sym, input.Query, isWildcard),
			})
			if len(candidates) >= hardcap {
				break
			}
		}
		if len(candidates) >= hardcap {
			break
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].fileScore != candidates[j].fileScore {
			return candidates[i].fileScore > candidates[j].fileScore
		}
		return candidates[i].symScore > candidates[j].symScore
	})

	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	matched := make([]*parser.Symbol, len(candidates))
	for i, c := range candidates {
		matched[i] = c.sym
	}
	return matched, nil
}
