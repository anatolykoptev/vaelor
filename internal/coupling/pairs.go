package coupling

import (
	"context"
	"sort"

	"github.com/anatolykoptev/vaelor/internal/federate"
)

// VerifiedPair augments a temporal CrossPair with semantic evidence.
type VerifiedPair struct {
	federate.CrossPair            // embedded — temporal signal preserved
	Verified           bool       `json:"verified"`
	LinkedBy           string     `json:"linkedBy,omitempty"`
	Evidence           []Evidence `json:"evidence,omitempty"`
}

// VerifyPairs runs the verifier over each candidate and returns them sorted
// verified-first, then by descending temporal Score within each tier. roots
// maps a repo slug → absolute root for file reads; a pair whose slug is absent
// is returned unverified (cannot read its files).
func VerifyPairs(ctx context.Context, cands []federate.CrossPair, roots map[string]string, v Verifier) []VerifiedPair {
	out := make([]VerifiedPair, 0, len(cands))
	for _, c := range cands {
		vp := VerifiedPair{CrossPair: c}
		rootA, okA := roots[c.RepoA]
		rootB, okB := roots[c.RepoB]
		if okA && okB {
			ev, err := v.Verify(ctx,
				FilePair{Repo: c.RepoA, Root: rootA, Rel: c.FileA},
				FilePair{Repo: c.RepoB, Root: rootB, Rel: c.FileB})
			if err == nil && len(ev) > 0 {
				vp.Verified = true
				vp.Evidence = ev
				vp.LinkedBy = ev[0].Kind + " " + ev[0].Detail
			}
		}
		out = append(out, vp)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Verified != out[j].Verified {
			return out[i].Verified
		}
		return out[i].Score > out[j].Score
	})
	return out
}
