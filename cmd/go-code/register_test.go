package main

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/graphx"
)

// TestBuildGraphDeps_NoStore verifies that buildGraphDeps returns graphx.Noop{}
// for both Analytics and CrossRefs when no store is available (empty DATABASE_URL).
func TestBuildGraphDeps_NoStore(t *testing.T) {
	analytics, refs := buildGraphDeps(nil)

	if analytics == nil {
		t.Fatal("Graph must be non-nil")
	}
	if refs == nil {
		t.Fatal("Refs must be non-nil")
	}

	if _, ok := analytics.(graphx.Noop); !ok {
		t.Errorf("Graph: expected graphx.Noop, got %T", analytics)
	}
	if _, ok := refs.(graphx.Noop); !ok {
		t.Errorf("Refs: expected graphx.Noop, got %T", refs)
	}
}
