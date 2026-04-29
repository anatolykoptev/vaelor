// Package main — eval harness for go-code retrieval quality.
//
// This file: nDCG@10, Recall@K, MRR over a single (golden, retrieved) pair.
// All inputs are 1-based ranked lists; relevance is binary (1 if a retrieved
// hit matches any expected symbol, else 0).
package main

import (
	"math"
	"strings"
)

// matchExpected returns true when the hit corresponds to one of the expected
// labels. Matching is lenient on three forms:
//
//  1. Exact equality on "<file>:<symbol>"
//  2. Exact equality on "<symbol>" (when label has no ':')
//  3. Suffix match on the file path (lets labelers write "rrf.go:MergeRRF"
//     and have it match "internal/embeddings/rrf.go:MergeRRF").
//
// Forgiving matching keeps the labeling cost low — the harness fights to find
// a real-world match instead of demanding exact paths the labeler may not know.
func matchExpected(hit SearchHit, expected []string) bool {
	hitKey := hit.File + ":" + hit.Symbol
	for _, raw := range expected {
		exp := strings.TrimSpace(raw)
		if exp == "" {
			continue
		}
		// Case 2: symbol-only label.
		if !strings.Contains(exp, ":") {
			if exp == hit.Symbol {
				return true
			}
			continue
		}
		// Case 1: exact file:symbol.
		if exp == hitKey {
			return true
		}
		// Case 3: suffix match on file portion.
		fp, sym, ok := strings.Cut(exp, ":")
		if !ok {
			continue
		}
		if sym == hit.Symbol && (strings.HasSuffix(hit.File, fp) || strings.HasSuffix(hit.File, "/"+fp)) {
			return true
		}
	}
	return false
}

// NDCG10 computes nDCG@10 with binary relevance. Definition:
//
//	DCG@k = Σ_{i=1..k} rel_i / log2(i+1)
//	IDCG@k = DCG of the ideal ranking (all relevant items first)
//	nDCG@k = DCG@k / IDCG@k
//
// rel_i = 1 if hit at position i matches an expected label, else 0. When no
// expected labels exist, returns 0. When IDCG is 0 (e.g. expected is empty),
// returns 0 to avoid divide-by-zero.
func NDCG10(hits []SearchHit, expected []string) float64 {
	const k = 10
	if len(expected) == 0 {
		return 0
	}

	dcg := 0.0
	cap := minInt(k, len(hits))
	for i := 0; i < cap; i++ {
		if matchExpected(hits[i], expected) {
			// log2(i+2) since i is 0-based; rank position = i+1.
			dcg += 1.0 / math.Log2(float64(i)+2.0)
		}
	}

	idealHits := minInt(k, len(expected))
	idcg := 0.0
	for i := 0; i < idealHits; i++ {
		idcg += 1.0 / math.Log2(float64(i)+2.0)
	}
	if idcg == 0 {
		return 0
	}
	return dcg / idcg
}

// RecallAtK returns |expected ∩ hits[:k]| / |expected|. Each expected label is
// counted at most once, even if multiple hits map to it.
func RecallAtK(hits []SearchHit, expected []string, k int) float64 {
	if len(expected) == 0 {
		return 0
	}
	cap := minInt(k, len(hits))
	matched := make(map[string]bool, len(expected))
	for i := 0; i < cap; i++ {
		for _, exp := range expected {
			if matched[exp] {
				continue
			}
			if matchExpected(hits[i], []string{exp}) {
				matched[exp] = true
			}
		}
	}
	return float64(len(matched)) / float64(len(expected))
}

// MRR returns 1/rank of the first relevant hit (rank is 1-based), or 0 when
// no relevant hit appears. Standard reciprocal rank — when reported as "Mean"
// it's an aggregate across queries; per-query it's a reciprocal rank.
func MRR(hits []SearchHit, expected []string) float64 {
	if len(expected) == 0 {
		return 0
	}
	for i, h := range hits {
		if matchExpected(h, expected) {
			return 1.0 / float64(i+1)
		}
	}
	return 0
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
