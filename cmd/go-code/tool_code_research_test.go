package main

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/anatolykoptev/vaelor/internal/research"
)

func TestFormatResearchResultCompact(t *testing.T) {
	in := CodeResearchInput{Repo: "x", Query: "Foo", Compact: true}
	r := &research.Result{
		Seeds: []research.SeedSymbol{{File: "a.go", Name: "Foo", Kind: "function"}},
		Graph: []research.LinkedFile{{RelPath: "b.go", Distance: 1, Symbols: []*parser.Symbol{{Name: "Bar"}}}},
		Map:   "a.go [seed]\n    Foo\n",
		Mode:  "keyword-only",
	}
	out := formatResearchResult(in, "/tmp/workspace", r)
	if strings.Contains(out, "<seeds>") {
		t.Errorf("compact mode must omit <seeds>, got:\n%s", out)
	}
	if strings.Contains(out, "<graph>") {
		t.Errorf("compact mode must omit <graph>, got:\n%s", out)
	}
	if !strings.Contains(out, "<map>") {
		t.Errorf("compact mode must still include <map>, got:\n%s", out)
	}
	if !strings.Contains(out, "<stats") {
		t.Errorf("compact mode must still include <stats>, got:\n%s", out)
	}
}

func TestFormatResearchResultNonCompact(t *testing.T) {
	// Regression: default (Compact=false) must still emit all sections.
	in := CodeResearchInput{Repo: "x", Query: "Foo"}
	r := &research.Result{
		Seeds: []research.SeedSymbol{{File: "a.go", Name: "Foo", Kind: "function"}},
		Graph: []research.LinkedFile{{RelPath: "b.go", Distance: 1}},
		Map:   "a.go [seed]\n    Foo\n",
		Mode:  "keyword-only",
	}
	out := formatResearchResult(in, "/tmp/workspace", r)
	for _, tag := range []string{"<seeds>", "<graph>", "<map>", "<stats"} {
		if !strings.Contains(out, tag) {
			t.Errorf("non-compact must include %s, got:\n%s", tag, out)
		}
	}
}
