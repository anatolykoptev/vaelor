// Package jeff provides a client for jeff System One decision services —
// the open-source, self-hostable implementation of the TypeSafe Jev
// contract (POST /v1/systemone).
//
// A jeff service answers bounded typed questions about a serialized state:
//
//   - noul   — yes/no, returns a probability (0..1)
//   - choice — pick one of labelled options, returns label + confidence
//   - score  — rate on an ordered scale, returns the probability-weighted
//     mean level + per-level probabilities (Answer.Level gives the argmax)
//
// Typical use is a cheap local gate in front of an expensive action —
// e.g. validating a remembered browser action before replaying it —
// where a full LLM call would be too slow and too costly.
//
// Latency grows with the number of labels scored, not only with the state:
// on the reference deployment (GLiFormer-large, ONNX int8, 2 ARM vCPUs) a
// 3-question request takes ~0.9s and one 31-option choice ~4.1s. Size the
// timeout to the label count.
//
// Construction:
//
//	c, err := jeff.NewClient("https://jeff.example.com",
//	    jeff.WithTimeout(5*time.Second))
//	prob, err := c.AskNoul(ctx, state, "is replaying this action safe?")
//
// Auth: the bearer token resolves from WithToken or the JEFF_TOKEN env var.
// With WithRequireAuth a missing token fails construction with ErrNoToken
// instead of surfacing as per-call 401s.
package jeff
