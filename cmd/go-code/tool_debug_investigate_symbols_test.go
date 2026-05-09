// cmd/go-code/tool_debug_investigate_symbols_test.go
//
// Unit tests for Phase 3 symbol resolution — specifically the preferred code.*
// OTEL tag path and the fallback OperationToFuncName + FindSymbol path.
package main

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/investigate"
	"github.com/anatolykoptev/go-code/internal/jaegerclient"
)

// buildTraceWithCodeTags is a helper that returns a single Trace with one span
// carrying the given OTEL code.* tags plus an http.route/method.
func buildTraceWithCodeTags(op, route, method, filepath, namespace string, lineno float64) jaegerclient.Trace {
	return jaegerclient.Trace{
		TraceID: "t1",
		Spans: []jaegerclient.Span{{
			OperationName: op,
			Tags: []jaegerclient.SpanTag{
				{Key: "http.route", Value: route},
				{Key: "http.method", Value: method},
				{Key: "code.filepath", Value: filepath},
				{Key: "code.lineno", Value: lineno},
				{Key: "code.namespace", Value: namespace},
			},
		}},
	}
}

// TestPhase3_ResolvesViaCodeTags_WithoutCallgraph verifies that when a span
// carries code.filepath + code.lineno, Phase 3 resolves it directly without
// needing a callgraph. SymbolsTouched must be incremented.
func TestPhase3_ResolvesViaCodeTags_WithoutCallgraph(t *testing.T) {
	traces := []jaegerclient.Trace{
		buildTraceWithCodeTags(
			"request",
			"/api/turn-credentials",
			"GET",
			"/src/server/src/turn_credentials.rs",
			"oxpulse_chat::turn_credentials",
			float64(142),
		),
	}

	res := &investigate.InvestigationResult{}
	deps := analyze.Deps{} // no callgraph, no repo resolution
	// Empty repo — Phase 3 uses code.* path, no callgraph needed.
	input := DebugInvestigateInput{Service: "oxpulse-chat", Repo: ""}

	runSymbolsPhase(nil, deps, input, traces, 0.5, res) //nolint:staticcheck // context unused in test path

	if res.Diagnostics.SymbolsTouched != 1 {
		t.Errorf("SymbolsTouched = %d, want 1", res.Diagnostics.SymbolsTouched)
	}
	if len(res.Hypotheses) == 0 {
		t.Fatal("expected at least one hypothesis")
	}
	h := res.Hypotheses[0]
	if h.File == "" {
		t.Errorf("hypothesis File is empty, want code.filepath-derived value")
	}
	if h.Line != 142 {
		t.Errorf("hypothesis Line = %d, want 142", h.Line)
	}
}

// TestPhase3_FallsBackToOperationToFuncName_WhenNoCodeTags verifies that when
// no code.* tags are present, the existing OperationToFuncName logic handles
// the span. For this test we use an empty repo (no callgraph), so the span
// ends up in the frequency-only fallback — SymbolsTouched stays 0.
func TestPhase3_FallsBackToOperationToFuncName_WhenNoCodeTags(t *testing.T) {
	traces := []jaegerclient.Trace{{
		TraceID: "t1",
		Spans: []jaegerclient.Span{{
			OperationName: "/pkg.MyService/DoThing",
			// No code.* tags.
		}},
	}}

	res := &investigate.InvestigationResult{}
	deps := analyze.Deps{}
	input := DebugInvestigateInput{Service: "my-svc", Repo: ""}

	runSymbolsPhase(nil, deps, input, traces, 0.3, res) //nolint:staticcheck

	// Without a callgraph we fall through to frequency-only fallback.
	if len(res.Hypotheses) == 0 {
		t.Fatal("expected at least one hypothesis from fallback")
	}
	// SymbolsTouched is 0 because there's no callgraph to resolve against.
	if res.Diagnostics.SymbolsTouched != 0 {
		t.Errorf("SymbolsTouched = %d, want 0 (no callgraph)", res.Diagnostics.SymbolsTouched)
	}
}

// TestPhase3_NextCheck_FileLineForm verifies that when code.* tags are used,
// the resulting NextCheck carries "file" and "line" args (not "symbol").
func TestPhase3_NextCheck_FileLineForm(t *testing.T) {
	traces := []jaegerclient.Trace{
		buildTraceWithCodeTags(
			"request",
			"/api/health",
			"GET",
			"/src/health.rs",
			"myapp::health",
			float64(10),
		),
	}

	res := &investigate.InvestigationResult{}
	deps := analyze.Deps{}
	input := DebugInvestigateInput{Service: "myapp", Repo: ""}

	runSymbolsPhase(nil, deps, input, traces, 0.1, res) //nolint:staticcheck

	if len(res.Hypotheses) == 0 {
		t.Fatal("expected at least one hypothesis")
	}
	h := res.Hypotheses[0]
	if len(h.NextChecks) == 0 {
		t.Fatal("expected at least one NextCheck")
	}
	nc := h.NextChecks[0]
	if nc.Tool != "understand" {
		t.Errorf("NextCheck.Tool = %q, want %q", nc.Tool, "understand")
	}
	if _, hasFile := nc.Args["file"]; !hasFile {
		t.Errorf("NextCheck.Args missing \"file\" key; got %v", nc.Args)
	}
	if _, hasLine := nc.Args["line"]; !hasLine {
		t.Errorf("NextCheck.Args missing \"line\" key; got %v", nc.Args)
	}
	if _, hasSymbol := nc.Args["symbol"]; hasSymbol {
		t.Errorf("NextCheck.Args must not have \"symbol\" key when using code.* path; got %v", nc.Args)
	}
}
