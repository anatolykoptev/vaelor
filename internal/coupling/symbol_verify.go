package coupling

import (
	"context"
	"sort"
)

// symbolVerifier (T1) proves a shared-contract dependency: two files in
// different repos that reference the same high-signal token (a SCREAMING_SNAKE
// env-var/const, or a structured string-literal protocol field like
// "peer_joined"). Fully offline — extractSignificantSymbols is a byte-scan, no
// CGO, no DB. Catches the acme WebSocket/protocol-constant coupling that has
// no HTTP route (T0 cannot see it). A per-instance cache dedupes token
// extraction for a file appearing in multiple candidate pairs.
type symbolVerifier struct {
	cache map[string]map[string]struct{} // key: root+"\x00"+rel
}

// NewSymbolVerifier returns a fresh T1 symbol verifier. Construct one per tool
// call (the cache is per-call, not shared across calls).
func NewSymbolVerifier() *symbolVerifier {
	return &symbolVerifier{cache: make(map[string]map[string]struct{})}
}

// maxSymbolEvidence caps how many shared tokens we emit per pair: a handful of
// shared contract names is ample proof; more would bloat the response.
const maxSymbolEvidence = 5

// Verify implements Verifier: returns one symbol Evidence per shared significant
// token, sorted for determinism, capped at maxSymbolEvidence. Assumes a and b
// are distinct files in different repos (its only production caller, VerifyPairs
// fed by CrossRepoCoChange, guarantees RepoA != RepoB); a self-pair would report
// all of a file's own tokens as "shared".
func (v *symbolVerifier) Verify(_ context.Context, a, b FilePair) ([]Evidence, error) {
	sa := v.symbolsOf(a)
	sb := v.symbolsOf(b)
	if len(sa) == 0 || len(sb) == 0 {
		return nil, nil
	}
	small, large := sa, sb
	if len(sb) < len(sa) {
		small, large = sb, sa
	}
	var shared []string
	for tok := range small {
		if _, ok := large[tok]; ok {
			shared = append(shared, tok)
		}
	}
	if len(shared) == 0 {
		return nil, nil
	}
	sort.Strings(shared) // deterministic output
	if len(shared) > maxSymbolEvidence {
		shared = shared[:maxSymbolEvidence]
	}
	ev := make([]Evidence, 0, len(shared))
	for _, tok := range shared {
		ev = append(ev, Evidence{Kind: "symbol", Detail: tok, Tier: "offline"})
	}
	return ev, nil
}

// symbolsOf reads + extracts significant tokens for a file, cached per (root, rel).
func (v *symbolVerifier) symbolsOf(f FilePair) map[string]struct{} {
	key := f.Root + "\x00" + f.Rel
	if cached, ok := v.cache[key]; ok {
		return cached
	}
	src, lang := readVerifyFile(f.Root, f.Rel)
	var syms map[string]struct{}
	if lang != "" {
		syms = extractSignificantSymbols(src)
	}
	v.cache[key] = syms
	return syms
}
