package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/llm"
	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/callgraph"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// A basic-tier answer whose typed pass did not run must say so in the tool
// response itself. Each row drives the real handler; removing one tool's
// TierNote surfacing turns exactly that row RED.
func TestTierNote_SurfacesInUnderstandImpactAndCallTrace(t *testing.T) {
	const target = "transcribe"
	const wantNote = "typed call resolution did not run (python: scip-python was not run"

	basicCG := func(root string) *callgraph.CallGraph {
		return &callgraph.CallGraph{
			Symbols: []*parser.Symbol{
				{Name: target, Kind: parser.KindMethod, Receiver: "OpenAi", Language: "python", File: filepath.Join(root, "open_ai.py"), StartLine: 1, EndLine: 3},
			},
			Tier:         "basic",
			TypedSkipped: []string{"python: scip-python was not run: it executes repo code and this repo is not an operator-trusted checkout"},
		}
	}

	rows := map[string]func(t *testing.T) string{
		"understand": func(t *testing.T) string {
			orig := understandBuildFromRepo
			defer func() { understandBuildFromRepo = orig }()
			understandBuildFromRepo = func(_ context.Context, in callgraph.TraceRepoInput) (*callgraph.CallGraph, error) {
				return basicCG(in.Root), nil
			}
			res, err := handleUnderstand(context.Background(), UnderstandInput{Repo: t.TempDir(), Symbol: "OpenAi." + target}, analyze.Deps{}, nil, nil, "")
			if err != nil {
				t.Fatal(err)
			}
			return textContentOf(t, res)
		},
		"impact_analysis": func(t *testing.T) string {
			orig := impactBuildFromRepo
			defer func() { impactBuildFromRepo = orig }()
			impactBuildFromRepo = func(_ context.Context, in callgraph.TraceRepoInput) (*callgraph.CallGraph, error) {
				return basicCG(in.Root), nil
			}
			res, err := handleImpact(context.Background(), ImpactInput{Repo: t.TempDir(), Symbol: "OpenAi." + target}, analyze.Deps{LLM: llm.NoOp{}}, nil, "")
			if err != nil {
				t.Fatal(err)
			}
			return textContentOf(t, res)
		},
		"call_trace": func(t *testing.T) string {
			orig := callTraceTraceFromAGE
			defer func() { callTraceTraceFromAGE = orig }()
			callTraceTraceFromAGE = func(context.Context, *codegraph.Store, string, string, string, int) (*callgraph.TraceResult, error) {
				root := &parser.Symbol{Name: target, Kind: parser.KindMethod, File: "open_ai.py", StartLine: 1}
				return &callgraph.TraceResult{
					Root: root, Tree: []callgraph.CallChainNode{{Symbol: root}}, TotalNodes: 1, Resolved: 1,
					Tier:     "basic",
					TierNote: callgraph.TierNote(basicCG("/r")),
				}, nil
			}
			res, err := handleCallTrace(context.Background(), CallTraceInput{Repo: t.TempDir(), Symbol: target, Compact: true}, analyze.Deps{LLM: llm.NoOp{}}, nil, "", &codegraph.Store{})
			if err != nil {
				t.Fatal(err)
			}
			return textContentOf(t, res)
		},
	}
	for name, run := range rows {
		t.Run(name, func(t *testing.T) {
			if out := run(t); !strings.Contains(out, wantNote) {
				t.Fatalf("%s response lacks the basic-tier note %q:\n%s", name, wantNote, out)
			}
		})
	}
}
