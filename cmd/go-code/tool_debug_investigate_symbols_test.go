// cmd/go-code/tool_debug_investigate_symbols_test.go
//
// Unit tests for Phase 3 symbol resolution — specifically the preferred code.*
// OTEL tag path and the fallback OperationToFuncName + FindSymbol path.
package main

import (
	"strings"
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
			"acme_chat::turn_credentials",
			float64(142),
		),
	}

	res := &investigate.InvestigationResult{}
	deps := analyze.Deps{} // no callgraph, no repo resolution
	// Empty repo — Phase 3 uses code.* path, no callgraph needed.
	input := DebugInvestigateInput{Service: "acme-web", Repo: ""}

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

// ────────────────────────────────────────────────────────────────────
// Fix 1: New OTEL attribute names (tracing-opentelemetry v0.32+)
// ────────────────────────────────────────────────────────────────────

// buildTraceWithNewOTELNames returns a trace using the v0.32+ renamed tags:
// code.file.path, code.line.number, code.module.name.
func buildTraceWithNewOTELNames(op, route, method, filepath, namespace string, lineno float64) jaegerclient.Trace {
	return jaegerclient.Trace{
		TraceID: "t2",
		Spans: []jaegerclient.Span{{
			OperationName: op,
			Tags: []jaegerclient.SpanTag{
				{Key: "http.route", Value: route},
				{Key: "http.method", Value: method},
				{Key: "code.file.path", Value: filepath},
				{Key: "code.line.number", Value: lineno},
				{Key: "code.module.name", Value: namespace},
			},
		}},
	}
}

// TestBuildOpsMap_NewOTELNames verifies that the v0.32+ renamed tags
// (code.file.path, code.line.number, code.module.name) are parsed
// identically to the legacy names.
func TestBuildOpsMap_NewOTELNames(t *testing.T) {
	traces := []jaegerclient.Trace{
		buildTraceWithNewOTELNames(
			"request",
			"/api/x",
			"POST",
			"/src/handler.rs",
			"myapp::handler",
			float64(77),
		),
	}
	ops := buildOpsMap(traces)
	info, ok := ops["request"]
	if !ok {
		t.Fatal("operation 'request' not found in ops map")
	}
	if info.CodeFilepath != "/src/handler.rs" {
		t.Errorf("CodeFilepath = %q, want %q", info.CodeFilepath, "/src/handler.rs")
	}
	if info.CodeLineno != 77 {
		t.Errorf("CodeLineno = %d, want 77", info.CodeLineno)
	}
	if info.CodeNamespace != "myapp::handler" {
		t.Errorf("CodeNamespace = %q, want %q", info.CodeNamespace, "myapp::handler")
	}
}

// TestBuildOpsMap_NewOTELNames_DoNotOverwriteExisting verifies first-seen wins:
// if both old and new names appear in the same span (edge case), the old one
// (seen first) is preserved.
func TestBuildOpsMap_NewOTELNames_DoNotOverwriteExisting(t *testing.T) {
	tr := jaegerclient.Trace{
		TraceID: "t3",
		Spans: []jaegerclient.Span{{
			OperationName: "op",
			Tags: []jaegerclient.SpanTag{
				{Key: "code.filepath", Value: "/src/old.rs"},
				{Key: "code.file.path", Value: "/src/new.rs"}, // should NOT overwrite
				{Key: "code.lineno", Value: float64(10)},
				{Key: "code.line.number", Value: float64(20)}, // should NOT overwrite
			},
		}},
	}
	ops := buildOpsMap([]jaegerclient.Trace{tr})
	info := ops["op"]
	if info.CodeFilepath != "/src/old.rs" {
		t.Errorf("CodeFilepath = %q, want %q (first-seen wins)", info.CodeFilepath, "/src/old.rs")
	}
	if info.CodeLineno != 10 {
		t.Errorf("CodeLineno = %d, want 10 (first-seen wins)", info.CodeLineno)
	}
}

// ────────────────────────────────────────────────────────────────────
// Fix 2 & 3: isLibraryPath + library-path filter + http.route fallback
// ────────────────────────────────────────────────────────────────────

// TestIsLibraryPath verifies the library-path heuristic against known patterns.
func TestIsLibraryPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		// library paths → true
		{"/home/user/.cargo/registry/src/tower-http-0.5.2/src/trace/make_span.rs", true},
		{"/root/.rustup/toolchains/stable-x86_64/lib/rustlib/src/core/src/ops.rs", true},
		{"/home/user/go/pkg/mod/github.com/some/dep@v1.2.3/foo.go", true},
		{"/usr/local/go/src/net/http/server.go", true},
		// app paths → false
		{"/src/server/src/turn_credentials.rs", false},
		{"/home/user/myapp/src/handlers/auth.rs", false},
		{"src/lib.rs", false},
		{"", false},
	}
	for _, tc := range cases {
		got := isLibraryPath(tc.path)
		if got != tc.want {
			t.Errorf("isLibraryPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}

// TestPhase3_LibraryPathFiltered verifies that when code.filepath points to
// cargo registry internals (tower-http make_span.rs), Phase 3 treats it as
// absent and falls through to the http.route tier-2 path.
func TestPhase3_LibraryPathFiltered(t *testing.T) {
	traces := []jaegerclient.Trace{
		buildTraceWithCodeTags(
			"request",
			"/api/x",
			"GET",
			"/home/user/.cargo/registry/src/tower-http-0.5.2/src/trace/make_span.rs",
			"tower_http::trace::make_span::DefaultMakeSpan",
			float64(42),
		),
	}

	res := &investigate.InvestigationResult{}
	deps := analyze.Deps{}
	input := DebugInvestigateInput{Service: "axum-svc", Repo: ""}

	runSymbolsPhase(nil, deps, input, traces, 0.5, res) //nolint:staticcheck

	if len(res.Hypotheses) == 0 {
		t.Fatal("expected at least one hypothesis (tier-2 http.route fallback)")
	}
	h := res.Hypotheses[0]
	// Subject must contain the route, not the library file path.
	if !strings.Contains(h.Subject, "/api/x") {
		t.Errorf("Subject = %q, want it to contain route %q", h.Subject, "/api/x")
	}
	// Should have a code_search next_check for the route.
	foundCodeSearch := false
	for _, nc := range h.NextChecks {
		if nc.Tool == "code_search" {
			foundCodeSearch = true
		}
	}
	if !foundCodeSearch {
		t.Errorf("expected code_search NextCheck for tier-2 route fallback; got %v", h.NextChecks)
	}
	// SymbolsTouched must be incremented.
	if res.Diagnostics.SymbolsTouched == 0 {
		t.Errorf("SymbolsTouched = 0, want > 0 for tier-2 path")
	}
}

// TestPhase3_HTTPRouteFallback verifies that when code.* tags are entirely
// absent but http.route is present, Phase 3 emits a tier-2 hypothesis with
// Subject = "<method> <route>" and a code_search next_check.
func TestPhase3_HTTPRouteFallback(t *testing.T) {
	traces := []jaegerclient.Trace{{
		TraceID: "t4",
		Spans: []jaegerclient.Span{{
			OperationName: "HTTP GET /api/x",
			Tags: []jaegerclient.SpanTag{
				{Key: "http.route", Value: "/api/x"},
				{Key: "http.method", Value: "GET"},
				// no code.* tags
			},
		}},
	}}

	res := &investigate.InvestigationResult{}
	deps := analyze.Deps{}
	input := DebugInvestigateInput{Service: "axum-svc", Repo: ""}

	runSymbolsPhase(nil, deps, input, traces, 0.7, res) //nolint:staticcheck

	if len(res.Hypotheses) == 0 {
		t.Fatal("expected at least one hypothesis")
	}
	h := res.Hypotheses[0]
	if !strings.Contains(h.Subject, "GET") || !strings.Contains(h.Subject, "/api/x") {
		t.Errorf("Subject = %q, want it to contain method and route", h.Subject)
	}
	foundCodeSearch := false
	var codeSearchNC investigate.NextCheck
	for _, nc := range h.NextChecks {
		if nc.Tool == "code_search" {
			foundCodeSearch = true
			codeSearchNC = nc
		}
	}
	if !foundCodeSearch {
		t.Errorf("expected code_search NextCheck; got %v", h.NextChecks)
	}
	// Pattern must reference the route for axum Router::route lookup.
	if !strings.Contains(codeSearchNC.Args["pattern"], "/api/x") {
		t.Errorf("code_search pattern = %q, want it to contain route", codeSearchNC.Args["pattern"])
	}
	if res.Diagnostics.SymbolsTouched == 0 {
		t.Errorf("SymbolsTouched = 0, want > 0 for tier-2 path")
	}
}

// TestPhase3_NoCodeNoRoute_FallsBackToOperationToFuncName verifies that with
// no code.* tags and no http.route, the operation falls through entirely to
// the existing frequency-only / callgraph path — resolvedFromCodeTags must
// NOT be set, so a repo-backed PASS 2 would still handle it.
func TestPhase3_NoCodeNoRoute_FallsBackToOperationToFuncName(t *testing.T) {
	traces := []jaegerclient.Trace{{
		TraceID: "t5",
		Spans: []jaegerclient.Span{{
			OperationName: "HandleMessage",
			Tags:          []jaegerclient.SpanTag{
				// no code.*, no http.route — legacy op name only
			},
		}},
	}}

	res := &investigate.InvestigationResult{}
	deps := analyze.Deps{}
	input := DebugInvestigateInput{Service: "legacy-svc", Repo: ""}

	runSymbolsPhase(nil, deps, input, traces, 0.2, res) //nolint:staticcheck

	// Without a callgraph we fall through to frequency-only fallback.
	if len(res.Hypotheses) == 0 {
		t.Fatal("expected at least one hypothesis from fallback")
	}
	// Subject must include the operation name.
	h := res.Hypotheses[0]
	if !strings.Contains(h.Subject, "HandleMessage") {
		t.Errorf("Subject = %q, want it to contain operation name", h.Subject)
	}
	// SymbolsTouched stays 0 — no tier-1 or tier-2 resolution happened.
	if res.Diagnostics.SymbolsTouched != 0 {
		t.Errorf("SymbolsTouched = %d, want 0 (no tier-1/tier-2 resolution)", res.Diagnostics.SymbolsTouched)
	}
}

// ────────────────────────────────────────────────────────────────────
// M1 fix: empty repo must not emit "repo": "" in Tier-2 next_check
// ────────────────────────────────────────────────────────────────────

// TestPhase3_HTTPRouteFallback_EmptyRepo_OmitsRepoArg verifies that when
// input.Repo is empty, the Tier-2 code_search next_check does NOT include
// a "repo": "" entry — that would silently break code_search invocation.
func TestPhase3_HTTPRouteFallback_EmptyRepo_OmitsRepoArg(t *testing.T) {
	traces := []jaegerclient.Trace{{
		TraceID: "t6",
		Spans: []jaegerclient.Span{{
			OperationName: "HTTP GET /api/x",
			Tags: []jaegerclient.SpanTag{
				{Key: "http.route", Value: "/api/x"},
				{Key: "http.method", Value: "GET"},
				// no code.* tags — forces Tier-2 path
			},
		}},
	}}

	res := &investigate.InvestigationResult{}
	deps := analyze.Deps{}
	// Crucially, Repo is empty — some callers don't pass it.
	input := DebugInvestigateInput{Service: "axum-svc", Repo: ""}

	runSymbolsPhase(nil, deps, input, traces, 0.5, res) //nolint:staticcheck

	if len(res.Hypotheses) == 0 {
		t.Fatal("expected at least one hypothesis (tier-2 http.route fallback)")
	}
	h := res.Hypotheses[0]
	var codeSearchNC investigate.NextCheck
	found := false
	for _, nc := range h.NextChecks {
		if nc.Tool == "code_search" {
			codeSearchNC = nc
			found = true
		}
	}
	if !found {
		t.Fatal("expected code_search NextCheck for tier-2 route fallback")
	}
	if _, hasRepo := codeSearchNC.Args["repo"]; hasRepo {
		t.Errorf("code_search NextCheck.Args must not contain \"repo\" key when input.Repo is empty; got args=%v", codeSearchNC.Args)
	}
	if !strings.Contains(codeSearchNC.Args["pattern"], "/api/x") {
		t.Errorf("code_search pattern = %q, want it to contain route", codeSearchNC.Args["pattern"])
	}
}
