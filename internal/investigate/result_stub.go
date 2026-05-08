// internal/investigate/result_stub.go
//
// THIS FILE IS A TEMPORARY STUB. The full InvestigationResult type is being
// authored in parallel on branch feat/investigate-correlate-result (Task 6).
// At merge time this file is DELETED and replaced by result.go from that
// branch. Do not extend it — only InvestigationResult is needed here so
// lifecycle.go compiles in isolation.
package investigate

// InvestigationResult is a stub. Real definition lands with Task 6.
type InvestigationResult struct{}

// Compile-time signal: this stub must be replaced when the real type lands.
var _ stubMarker = stubMarker{}

type stubMarker struct{}
