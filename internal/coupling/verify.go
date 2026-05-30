// Package coupling provides stage-2 semantic verification for federated
// cross-repo co-change candidates: it proves (or fails to prove) that two files
// in different repos share a real dependency — starting with an offline HTTP
// route provider↔consumer match. Verified pairs are explainable and float above
// unverified temporal-only candidates.
package coupling

import "context"

// Evidence is one proof that a cross-repo pair shares a real dependency.
type Evidence struct {
	Kind   string `json:"kind"`   // "route" (later: "symbol" | "graph_route" | "embedding")
	Detail string `json:"detail"` // e.g. "POST /api/partner/register"
	Tier   string `json:"tier"`   // "offline" | "age" | "embeddings"
}

// FilePair is a candidate file the verifier inspects. Root+Rel locate the bytes.
type FilePair struct {
	Repo string // slug, for labels
	Root string // absolute repo root
	Rel  string // repo-relative path
}

// Verifier proves a dependency between two files in different repos. Returns ALL
// evidence found (a pair may be linked by more than one signal).
type Verifier interface {
	Verify(ctx context.Context, a, b FilePair) ([]Evidence, error)
}
