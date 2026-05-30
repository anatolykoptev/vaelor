package coupling

import "context"

// compositeVerifier runs several verifiers over a pair and concatenates their
// evidence in order. A sub-verifier returning an error still contributes any
// partial evidence it found — a future I/O tier may return (evidence, soft-error)
// and that evidence must not be lost. One signal failing must not hide another's
// result nor its own partial result.
// This is how T0 (route) and T1 (symbol) verification run together: VerifyPairs
// takes a single Verifier, so the composite presents many as one.
type compositeVerifier struct {
	vs []Verifier
}

// NewCompositeVerifier returns a Verifier that fans out to vs in order. Evidence
// ordering follows verifier order, so list route before symbol to keep the
// human-facing LinkedBy (ev[0]) preferring the more specific route proof.
func NewCompositeVerifier(vs ...Verifier) *compositeVerifier {
	return &compositeVerifier{vs: vs}
}

// Verify implements Verifier: concatenated evidence from every sub-verifier.
func (c *compositeVerifier) Verify(ctx context.Context, a, b FilePair) ([]Evidence, error) {
	var all []Evidence
	for _, v := range c.vs {
		ev, err := v.Verify(ctx, a, b)
		all = append(all, ev...) // keep any partial evidence even if this verifier also errored
		if err != nil {
			continue // a failing signal must not sink the others — nor its own partial result
		}
	}
	return all, nil
}
