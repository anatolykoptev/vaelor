package main

import (
	"context"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

func TestNormalizeCallTraceDirection(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"", "callees"},
		{"callees", "callees"},
		{"forward", "callees"},
		{"callers", "callers"},
		{"reverse", "callers"},
		{"unknown", "callees"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			got := normalizeCallTraceDirection(tc.input)
			if got != tc.want {
				t.Errorf("normalizeCallTraceDirection(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// TestCallTrace_ColdGraph_ReturnsBuildingStatus verifies that call_trace returns
// an XML building-status response instead of falling back to the synchronous
// callgraph.TraceRepo when the AGE graph is cold.
func TestCallTrace_ColdGraph_ReturnsBuildingStatus(t *testing.T) {
	origCacheStatus := ageGraphCacheStatus
	origIndexRepo := ageGraphIndexRepo
	origTraceFromAGE := callTraceTraceFromAGE
	defer func() {
		ageGraphCacheStatus = origCacheStatus
		ageGraphIndexRepo = origIndexRepo
		callTraceTraceFromAGE = origTraceFromAGE
	}()

	ageGraphCacheStatus = func(context.Context, *codegraph.Store, string) (bool, error) { return false, nil }
	ageGraphIndexRepo = func(context.Context, *codegraph.Store, string, bool, codegraph.IndexConfig) (*codegraph.GraphMeta, error) {
		return nil, nil
	}
	callTraceTraceFromAGE = func(context.Context, *codegraph.Store, string, string, string, int) (*callgraph.TraceResult, error) {
		return nil, codegraph.ErrGraphNotIndexed
	}

	root := t.TempDir()
	input := CallTraceInput{Repo: root, Symbol: "Foo"}
	deps := analyze.Deps{}
	store := &codegraph.Store{}

	res, err := handleCallTrace(context.Background(), input, deps, nil, "", store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res == nil {
		t.Fatal("expected non-nil result")
	}
	if res.IsError {
		t.Fatalf("expected non-error status response, got error: %s", textContentOf(t, res))
	}

	text := textContentOf(t, res)
	var status callTraceStatusXML
	if err := xml.Unmarshal([]byte(text), &status); err != nil {
		t.Fatalf("expected XML status, got %q: %v", text, err)
	}
	if status.Trace.Status != "building" {
		t.Errorf("expected trace status 'building', got %q", status.Trace.Status)
	}
	if !strings.Contains(status.Trace.Message, "retry") {
		t.Errorf("expected retry hint in message, got %q", status.Trace.Message)
	}
	if status.Trace.Symbol != "Foo" {
		t.Errorf("expected symbol %q, got %q", "Foo", status.Trace.Symbol)
	}
}

// Issue #867: an AGE-level ambiguous bare name must return the candidate
// list WITHOUT falling back to the expensive tree-sitter re-parse (which
// would produce the same ambiguity at 2-60s cost).
func TestCallTrace_AGEAmbiguous_ReturnsCandidates(t *testing.T) {
	orig := callTraceTraceFromAGE
	defer func() { callTraceTraceFromAGE = orig }()

	callTraceTraceFromAGE = func(context.Context, *codegraph.Store, string, string, string, int) (*callgraph.TraceResult, error) {
		return &callgraph.TraceResult{
			Ambiguous: []*parser.Symbol{
				{Name: "Close", Kind: parser.KindMethod, Receiver: "DB", File: "/src/db.go", StartLine: 10},
				{Name: "Close", Kind: parser.KindMethod, Receiver: "Cache", File: "/src/cache.go", StartLine: 20},
			},
		}, nil
	}

	root := t.TempDir()
	res, err := handleCallTrace(context.Background(),
		CallTraceInput{Repo: root, Symbol: "Close"}, analyze.Deps{}, nil, "", &codegraph.Store{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := textContentOf(t, res)
	if !strings.Contains(text, "ambiguous") || !strings.Contains(text, "DB") || !strings.Contains(text, "Cache") {
		t.Fatalf("expected ambiguity response with both candidates, got %q", text)
	}
	// A merged call tree must not be produced.
	if strings.Contains(text, `"tree"`) || strings.Contains(text, `"children"`) {
		t.Fatalf("ambiguous response must not contain a call tree, got %q", text)
	}
}

// With focus= set, an AGE-level ambiguity must NOT short-circuit — the
// scoped tree-sitter build may still disambiguate.
func TestCallTrace_AGEAmbiguous_WithFocus_FallsThrough(t *testing.T) {
	origAGE := callTraceTraceFromAGE
	origStatus := ageGraphCacheStatus
	origIndex := ageGraphIndexRepo
	defer func() {
		callTraceTraceFromAGE = origAGE
		ageGraphCacheStatus = origStatus
		ageGraphIndexRepo = origIndex
	}()

	callTraceTraceFromAGE = func(context.Context, *codegraph.Store, string, string, string, int) (*callgraph.TraceResult, error) {
		return &callgraph.TraceResult{
			Ambiguous: []*parser.Symbol{
				{Name: "Close", Kind: parser.KindMethod, Receiver: "DB", File: "/src/db.go"},
				{Name: "Close", Kind: parser.KindMethod, Receiver: "Cache", File: "/src/cache.go"},
			},
		}, nil
	}
	// Cold graph path must not intercept: report the graph as fresh so the
	// code reaches the tree-sitter fallback (which returns not-found on an
	// empty repo — proving the focus gate skipped the AGE short-circuit).
	ageGraphCacheStatus = func(context.Context, *codegraph.Store, string) (bool, error) { return true, nil }

	root := t.TempDir()
	res, err := handleCallTrace(context.Background(),
		CallTraceInput{Repo: root, Symbol: "Close", Focus: "db"}, analyze.Deps{}, nil, "", &codegraph.Store{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	text := textContentOf(t, res)
	// The ambiguity candidates must NOT be echoed — focus means the caller
	// wants the scoped attempt, which here finds nothing in an empty repo.
	if strings.Contains(text, "ambiguous") {
		t.Fatalf("focus set must not short-circuit on AGE ambiguity, got %q", text)
	}
	if !strings.Contains(text, "not found") {
		t.Fatalf("expected not-found from the scoped fallback, got %q", text)
	}
}
