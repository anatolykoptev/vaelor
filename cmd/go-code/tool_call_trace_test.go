package main

import (
	"context"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/codegraph"
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
