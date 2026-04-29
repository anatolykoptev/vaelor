package compare

import (
	"context"
	"log/slog"
	"math"
	"time"

	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-kit/embed"
)

// semanticMatchThreshold is the minimum cosine similarity for a semantic match.
const semanticMatchThreshold = 0.75

// maxSemanticCandidates limits how many unmatched symbols we embed (cost control).
const maxSemanticCandidates = 30

// semanticTimeout bounds how long embedding-based matching can run.
// Must be short enough to fit within compareTimeout alongside other work.
const semanticTimeout = 15 * time.Second

// EmbeddingClassifier implements LLMClassifier using vector similarity.
type EmbeddingClassifier struct {
	client *embed.Client
	ctx    context.Context
}

// NewEmbeddingClassifier creates a classifier using the given embedding client.
func NewEmbeddingClassifier(ctx context.Context, client *embed.Client) *EmbeddingClassifier {
	return &EmbeddingClassifier{client: client, ctx: ctx}
}

// ClassifySymbols finds semantic matches between unmatched symbols using embeddings.
func (c *EmbeddingClassifier) ClassifySymbols(a, b []*parser.Symbol) ([]SymbolMatch, error) {
	// Bound semantic matching — embedding calls must not block the whole compare.
	ctx, cancel := context.WithTimeout(c.ctx, semanticTimeout)
	defer cancel()

	// 1. Filter to functions/methods with bodies (nothing to embed for types/interfaces).
	candA := filterEmbeddable(a, maxSemanticCandidates)
	candB := filterEmbeddable(b, maxSemanticCandidates)
	if len(candA) == 0 || len(candB) == 0 {
		return nil, nil
	}

	// 2. Build embed texts.
	textsA := make([]string, len(candA))
	for i, sym := range candA {
		textsA[i] = sym.Name + "\n" + sym.Body
	}
	textsB := make([]string, len(candB))
	for i, sym := range candB {
		textsB[i] = sym.Name + "\n" + sym.Body
	}

	// 3. Embed both sets.
	vecsA, err := c.client.Embed(ctx, textsA)
	if err != nil {
		slog.Warn("semantic: embed A failed", "err", err)
		return nil, err
	}
	vecsB, err := c.client.Embed(ctx, textsB)
	if err != nil {
		slog.Warn("semantic: embed B failed", "err", err)
		return nil, err
	}

	// 4. For each symbol in A, find best match in B.
	usedB := make([]bool, len(candB))
	var matches []SymbolMatch

	for i, vecA := range vecsA {
		bestIdx := -1
		bestSim := 0.0

		for j, vecB := range vecsB {
			if usedB[j] {
				continue
			}
			sim := cosineSimilarity(vecA, vecB)
			if sim > bestSim {
				bestSim = sim
				bestIdx = j
			}
		}

		if bestIdx >= 0 && bestSim >= semanticMatchThreshold {
			usedB[bestIdx] = true
			matches = append(matches, SymbolMatch{
				SymbolA:   candA[i],
				SymbolB:   candB[bestIdx],
				MatchType: MatchSemantic,
				Category:  string(candA[i].Kind),
				Score:     bestSim,
			})
		}
	}

	return matches, nil
}

// filterEmbeddable returns symbols with non-empty bodies, limited to max count.
func filterEmbeddable(syms []*parser.Symbol, max int) []*parser.Symbol {
	var result []*parser.Symbol
	for _, s := range syms {
		if s.Body == "" {
			continue
		}
		if s.Kind != "function" && s.Kind != "method" && s.Kind != "class" {
			continue
		}
		result = append(result, s)
		if len(result) >= max {
			break
		}
	}
	return result
}

// cosineSimilarity computes cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
