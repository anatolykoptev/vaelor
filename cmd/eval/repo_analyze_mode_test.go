// Package main — eval harness tests for the repo_analyze mode.
//
// Coverage:
//   - parseRepoAnalyzeXML: ranked <file> list extraction (order preserved)
//   - fileLevelExpected: file-portion derivation from expected_top_3 labels
//   - runSingle repo_analyze mode: file-ranking metrics over a fixture
//   - EvaluateFusionGate: REAL gate (deltas + t-test verdict) in repo_analyze mode
//   - run() integration: fusion gate is NOT_EXERCISED in semantic_search mode,
//     REAL in repo_analyze mode; --mode default is semantic_search (byte-identical)
package main

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sampleRepoAnalyzeXML mirrors the repo_analyze tool's XML envelope (schema
// v2.0). The <files> section is emitted in relevance-ranked order
// (BM25F+PageRank fusion); only path attributes matter for file-level scoring.
const sampleRepoAnalyzeXML = `<?xml version="1.0" encoding="UTF-8"?>
<response schemaVersion="2.0">
  <repo name="go-code" language="go" files="3"/>
  <packages><package>main</package></packages>
  <files>
    <file path="internal/embeddings/rrf.go" lang="go" relevance="0.92" importedBy="3"/>
    <file path="internal/embeddings/expander.go" lang="go" relevance="0.71"/>
    <file path="internal/analyze/rank.go" lang="go" relevance="0.55"/>
  </files>
</response>`

// ──────────────────── repo_analyze XML parsing ────────────────────

func TestParseRepoAnalyzeXML(t *testing.T) {
	t.Parallel()
	files, err := parseRepoAnalyzeXML(sampleRepoAnalyzeXML)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	want := []string{
		"internal/embeddings/rrf.go",
		"internal/embeddings/expander.go",
		"internal/analyze/rank.go",
	}
	if len(files) != len(want) {
		t.Fatalf("expected %d files, got %d (%v)", len(want), len(files), files)
	}
	for i, w := range want {
		if files[i] != w {
			t.Errorf("file[%d] = %q, want %q (order must be relevance order)", i, files[i], w)
		}
	}
}

func TestParseRepoAnalyzeXML_NoFilesSection(t *testing.T) {
	t.Parallel()
	body := `<?xml version="1.0" encoding="UTF-8"?>
<response schemaVersion="2.0"><repo name="x" language="go" files="0"/></response>`
	files, err := parseRepoAnalyzeXML(body)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if files != nil && len(files) != 0 {
		t.Errorf("expected empty file list, got %v", files)
	}
}

func TestParseRepoAnalyzeXML_Empty(t *testing.T) {
	t.Parallel()
	files, err := parseRepoAnalyzeXML("")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if files != nil && len(files) != 0 {
		t.Errorf("expected nil/empty for empty payload, got %v", files)
	}
}

// ──────────────────── file-level expected derivation ────────────────────

func TestFileLevelExpected(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "file:symbol labels → file portions",
			in:   []string{"internal/embeddings/rrf.go:MergeRRF", "rank.go:sortByScores"},
			want: []string{"internal/embeddings/rrf.go:", "rank.go:"},
		},
		{
			name: "symbol-only labels dropped (no file info)",
			in:   []string{"MergeRRF", "sortByScores"},
			want: nil,
		},
		{
			name: "mixed: symbol-only dropped, file:symbol kept",
			in:   []string{"MergeRRF", "rrf.go:MergeRRF"},
			want: []string{"rrf.go:"},
		},
		{
			name: "dedup same file",
			in:   []string{"rrf.go:MergeRRF", "rrf.go:Expand"},
			want: []string{"rrf.go:"},
		},
		{
			name: "empty input",
			in:   nil,
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := fileLevelExpected(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("[%d] got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// ──────────────────── runSingle repo_analyze mode metrics ────────────────────

// TestRunSingle_RepoAnalyzeMode_FileMetrics verifies that repo_analyze mode
// computes file-ranking metrics (nDCG@10 / Recall@10 / Recall@20 / MRR) over
// the ranked file list against the file-relevance target derived from
// expected_top_3.
//
// Fixture: server returns 3 ranked files; golden expects symbols in
// rrf.go and rank.go → relevant file set = {rrf.go, rank.go}.
// Ranked: [rrf.go, expander.go, rank.go].
//   - nDCG@10: rel at pos 1 and 3 → DCG = 1/log2(2) + 1/log2(4) = 1 + 0.5 = 1.5
//     IDCG (2 relevant) = 1/log2(2) + 1/log2(3) = 1 + 0.6309 = 1.6309
//     nDCG = 1.5 / 1.6309 ≈ 0.9196
//   - Recall@10: 2 of 2 relevant files retrieved → 1.0
//   - Recall@20: 1.0
//   - MRR: first relevant at rank 1 → 1.0
//
// Falsification: revert runSingle's repo_analyze branch to call client.Search
// (semantic_search) instead of client.RepoAnalyze → the server's
// /api/tools/repo_analyze handler isn't hit, parseRepoAnalyzeXML never runs,
// and the file-level metrics stay zero → RED.
func TestRunSingle_RepoAnalyzeMode_FileMetrics(t *testing.T) {
	t.Parallel()
	var sawRepoAnalyze bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tools/repo_analyze" {
			sawRepoAnalyze = true
			resp := restCallToolResp{}
			resp.Content = append(resp.Content, struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{Type: "text", Text: sampleRepoAnalyzeXML})
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		// Any other path is unexpected in repo_analyze mode.
		t.Errorf("unexpected request path %s in repo_analyze mode", r.URL.Path)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	rec := GoldenRecord{
		Query: "merge rrf",
		// Two relevant files: rrf.go (suffix of internal/embeddings/rrf.go) and rank.go.
		ExpectedTop3: []string{"internal/embeddings/rrf.go:MergeRRF", "rank.go:sortByScores"},
		Repo:         "go-code",
	}
	result := runSingle(context.Background(), NewMCPClient(srv.URL), rec, runnerCfg{Mode: modeRepoAnalyze, TopK: 20})
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if !sawRepoAnalyze {
		t.Fatal("server never received /api/tools/repo_analyze call")
	}
	// nDCG@10 ≈ 0.9196 (1.5 / 1.6309).
	wantNDCG := 1.5 / (1.0 + 1.0/math.Log2(3))
	if math.Abs(result.NDCG10-wantNDCG) > 1e-9 {
		t.Errorf("NDCG10 = %f, want %f", result.NDCG10, wantNDCG)
	}
	if math.Abs(result.Recall10-1.0) > 1e-9 {
		t.Errorf("Recall10 = %f, want 1.0 (both relevant files in top 10)", result.Recall10)
	}
	if math.Abs(result.Recall20-1.0) > 1e-9 {
		t.Errorf("Recall20 = %f, want 1.0", result.Recall20)
	}
	if math.Abs(result.MRR-1.0) > 1e-9 {
		t.Errorf("MRR = %f, want 1.0 (first relevant file at rank 1)", result.MRR)
	}
	// Retrieved should be the ranked file paths (capped at 20).
	if len(result.Retrieved) != 3 || result.Retrieved[0] != "internal/embeddings/rrf.go" {
		t.Errorf("Retrieved = %v, want 3 ranked file paths starting with internal/embeddings/rrf.go", result.Retrieved)
	}
	if result.LatencyMS <= 0 {
		t.Errorf("LatencyMS = %f, want > 0 (latency must be recorded in repo_analyze mode too)", result.LatencyMS)
	}
}

// TestRunSingle_RepoAnalyzeMode_PassesLanguageFilter verifies the language arg
// is forwarded to repo_analyze when the golden record has a language field.
func TestRunSingle_RepoAnalyzeMode_PassesLanguageFilter(t *testing.T) {
	t.Parallel()
	var seenLanguage any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var args map[string]any
		_ = json.NewDecoder(r.Body).Decode(&args)
		seenLanguage = args["language"]
		resp := restCallToolResp{}
		resp.Content = append(resp.Content, struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: "text", Text: sampleRepoAnalyzeXML})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	rec := GoldenRecord{
		Query:        "merge rrf",
		ExpectedTop3: []string{"rrf.go:MergeRRF"},
		Repo:         "go-code",
		Language:     "go",
	}
	result := runSingle(context.Background(), NewMCPClient(srv.URL), rec, runnerCfg{Mode: modeRepoAnalyze, TopK: 20})
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if seenLanguage != "go" {
		t.Errorf("repo_analyze received language = %v, want go", seenLanguage)
	}
	if result.Language != "go" {
		t.Errorf("result.Language = %q, want go", result.Language)
	}
}

// ──────────────────── fusion gate: REAL in repo_analyze mode ────────────────────

// TestEvaluateFusionGate_Real verifies the fusion gate produces a REAL
// verdict (PASS/FAIL/INSUFFICIENT_DATA — not NOT_EXERCISED) with non-zero
// deltas and a t-test p-value when given paired query results.
//
// Falsification: revert EvaluateFusionGate to return FusionSkipResult →
// verdict becomes NOT_EXERCISED and deltas are zero → RED.
func TestEvaluateFusionGate_Real(t *testing.T) {
	t.Parallel()
	n := 20
	bl := makeResults("r", n, 0.50, 0.60)
	cn := makeResults("r", n, 0.62, 0.65) // significant nDCG improvement, non-inferior recall

	g := EvaluateFusionGate(bl, cn, "rrf")

	if g.Verdict == GateNotExercised {
		t.Fatal("verdict = NOT_EXERCISED, want a REAL verdict (PASS/FAIL/INSUFFICIENT_DATA)")
	}
	if g.TestedFusionMode != "rrf" {
		t.Errorf("TestedFusionMode = %q, want rrf", g.TestedFusionMode)
	}
	if g.PairedQueries != n {
		t.Errorf("PairedQueries = %d, want %d", g.PairedQueries, n)
	}
	if g.NDCG10Delta == 0 {
		t.Error("NDCG10Delta = 0, want non-zero (real paired t-test ran)")
	}
	if g.NDCG10P == 0 || g.NDCG10P == 1 {
		t.Errorf("NDCG10P = %f, want a real p-value in (0,1)", g.NDCG10P)
	}
	if g.Verdict != GatePass {
		t.Errorf("verdict = %s, want PASS (significant nDCG gain + non-inferior recall); "+
			"ndcg_delta=%.4f p=%.4f recall20_delta=%.4f p=%.4f",
			g.Verdict, g.NDCG10Delta, g.NDCG10P, g.Recall20Delta, g.Recall20P)
	}
	if !strings.Contains(g.RecommendedAction, "ANALYZE_RANK_FUSION_MODE=rrf") {
		t.Errorf("RecommendedAction = %q, want to contain ANALYZE_RANK_FUSION_MODE=rrf", g.RecommendedAction)
	}
}

// TestEvaluateFusionGate_Fail verifies a non-significant improvement yields FAIL.
func TestEvaluateFusionGate_Fail(t *testing.T) {
	t.Parallel()
	bl := makeResults("r", 10, 0.5, 0.6)
	cn := makeResults("r", 10, 0.5, 0.6) // identical → no improvement

	g := EvaluateFusionGate(bl, cn, "rrf")

	if g.Verdict != GateFail {
		t.Errorf("verdict = %s, want FAIL (no significant improvement)", g.Verdict)
	}
	if g.Verdict == GateNotExercised {
		t.Error("verdict = NOT_EXERCISED, want a REAL FAIL verdict")
	}
}

// ──────────────────── run() integration: mode dispatch ────────────────────

// TestRun_ModeDefault_SemanticSearch verifies the default mode is
// semantic_search and produces byte-identical output to the pre-mode harness
// (no `mode` field in metadata, semantic_search endpoint hit, fusion gate
// stays NOT_EXERCISED).
//
// Falsification: change the mode flag default to repo_analyze → the server
// receives /api/tools/repo_analyze instead of semantic_search → the test's
// path assertion fails → RED.
func TestRun_ModeDefault_SemanticSearch(t *testing.T) {
	t.Parallel()
	var seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		resp := restCallToolResp{}
		resp.Content = append(resp.Content, struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: "text", Text: sampleSemanticXML})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	dir := t.TempDir()
	content := `{"query": "q1", "expected_top_3": ["MergeRRF"], "repo": "go-code"}
`
	if err := os.WriteFile(filepath.Join(dir, "go-code.jsonl"), []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	outPath := filepath.Join(t.TempDir(), "out.json")
	// Pass modeSemanticSearch explicitly (the flag default).
	if err := run(dir, srv.URL, outPath, "", math.NaN(), math.NaN(), "", "", "", modeSemanticSearch, 1, 20, 10*time.Second); err != nil {
		t.Fatalf("run: %v", err)
	}
	if seenPath != restToolPath {
		t.Errorf("default mode hit path %q, want %q (semantic_search)", seenPath, restToolPath)
	}
	report, err := readReport(outPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	// Default mode must NOT add a `mode` metadata field (byte-identical).
	if report.Metadata.Mode != "" {
		t.Errorf("metadata.Mode = %q, want empty (default mode must be byte-identical)", report.Metadata.Mode)
	}
}

// TestRun_RepoAnalyzeMode_RealFusionGate verifies that in repo_analyze mode
// with --baseline + --fusion-mode, the report carries a REAL fusion gate
// (verdict != NOT_EXERCISED, with deltas), and that the repo_analyze endpoint
// is hit.
//
// Falsification: revert the repo_analyze-mode fusion gate branch to emit
// FusionSkipResult → verdict becomes NOT_EXERCISED → RED.
func TestRun_RepoAnalyzeMode_RealFusionGate(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Both baseline and candidate runs hit repo_analyze in repo_analyze mode.
		resp := restCallToolResp{}
		resp.Content = append(resp.Content, struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: "text", Text: sampleRepoAnalyzeXML})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// 2 queries so the gate has enough pairs for a t-test.
	dir := t.TempDir()
	content := `{"query": "q1", "expected_top_3": ["rrf.go:MergeRRF"], "repo": "go-code"}
{"query": "q2", "expected_top_3": ["rank.go:sortByScores"], "repo": "go-code"}
`
	if err := os.WriteFile(filepath.Join(dir, "go-code.jsonl"), []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// Baseline run (minmax on the server).
	baselinePath := filepath.Join(t.TempDir(), "baseline.json")
	if err := run(dir, srv.URL, baselinePath, "", math.NaN(), math.NaN(), "", "", "", modeRepoAnalyze, 2, 20, 30*time.Second); err != nil {
		t.Fatalf("baseline run: %v", err)
	}

	// Candidate run with --fusion-mode=rrf.
	outPath := filepath.Join(t.TempDir(), "cand.json")
	if err := run(dir, srv.URL, outPath, baselinePath, math.NaN(), math.NaN(), "", "rrf", "", modeRepoAnalyze, 2, 20, 30*time.Second); err != nil {
		t.Fatalf("candidate run: %v", err)
	}

	report, err := readReport(outPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if report.Metadata.Mode != modeRepoAnalyze {
		t.Errorf("metadata.Mode = %q, want %q", report.Metadata.Mode, modeRepoAnalyze)
	}
	if report.Metadata.FusionMode != "rrf" {
		t.Errorf("metadata.FusionMode = %q, want rrf", report.Metadata.FusionMode)
	}
	if report.FusionGate == nil {
		t.Fatal("FusionGate is nil, want non-nil real gate")
	}
	if report.FusionGate.Verdict == GateNotExercised {
		t.Fatal("FusionGate.Verdict = NOT_EXERCISED, want a REAL verdict in repo_analyze mode")
	}
	if report.FusionGate.TestedFusionMode != "rrf" {
		t.Errorf("FusionGate.TestedFusionMode = %q, want rrf", report.FusionGate.TestedFusionMode)
	}
	// Both runs hit the same fake server returning the same file list →
	// identical metrics → zero deltas → FAIL (no significant improvement),
	// but the gate must still be REAL (not NOT_EXERCISED).
	if report.FusionGate.Verdict != GateFail {
		t.Errorf("FusionGate.Verdict = %s, want FAIL (identical baseline/candidate → no improvement)",
			report.FusionGate.Verdict)
	}
}

// TestRun_SemanticSearchMode_FusionNotExercised verifies that in semantic_search
// mode the fusion gate stays NOT_EXERCISED even with --baseline + --fusion-mode
// (fusion doesn't affect the semantic_search path).
//
// Falsification: make the semantic_search mode emit a real fusion gate →
// verdict becomes PASS/FAIL → RED.
func TestRun_SemanticSearchMode_FusionNotExercised(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := restCallToolResp{}
		resp.Content = append(resp.Content, struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: "text", Text: sampleSemanticXML})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	dir := t.TempDir()
	content := `{"query": "q1", "expected_top_3": ["MergeRRF"], "repo": "go-code"}
{"query": "q2", "expected_top_3": ["MergeRRF"], "repo": "go-code"}
`
	if err := os.WriteFile(filepath.Join(dir, "go-code.jsonl"), []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	baselinePath := filepath.Join(t.TempDir(), "baseline.json")
	if err := run(dir, srv.URL, baselinePath, "", math.NaN(), math.NaN(), "", "", "", modeSemanticSearch, 2, 20, 30*time.Second); err != nil {
		t.Fatalf("baseline run: %v", err)
	}
	outPath := filepath.Join(t.TempDir(), "cand.json")
	if err := run(dir, srv.URL, outPath, baselinePath, math.NaN(), math.NaN(), "", "rrf", "", modeSemanticSearch, 2, 20, 30*time.Second); err != nil {
		t.Fatalf("candidate run: %v", err)
	}
	report, err := readReport(outPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if report.FusionGate == nil {
		t.Fatal("FusionGate is nil, want NOT_EXERCISED gate")
	}
	if report.FusionGate.Verdict != GateNotExercised {
		t.Errorf("FusionGate.Verdict = %s, want NOT_EXERCISED in semantic_search mode", report.FusionGate.Verdict)
	}
}

// TestRun_RepoAnalyzeMode_FusionNoBaseline_Insufficient verifies that
// repo_analyze mode + --fusion-mode WITHOUT --baseline emits an
// INSUFFICIENT_DATA fusion gate (the t-test needs a paired baseline).
func TestRun_RepoAnalyzeMode_FusionNoBaseline_Insufficient(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := restCallToolResp{}
		resp.Content = append(resp.Content, struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: "text", Text: sampleRepoAnalyzeXML})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	dir := t.TempDir()
	content := `{"query": "q1", "expected_top_3": ["rrf.go:MergeRRF"], "repo": "go-code"}
`
	if err := os.WriteFile(filepath.Join(dir, "go-code.jsonl"), []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	outPath := filepath.Join(t.TempDir(), "cand.json")
	if err := run(dir, srv.URL, outPath, "", math.NaN(), math.NaN(), "", "rrf", "", modeRepoAnalyze, 1, 20, 10*time.Second); err != nil {
		t.Fatalf("run: %v", err)
	}
	report, err := readReport(outPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if report.FusionGate == nil {
		t.Fatal("FusionGate is nil, want INSUFFICIENT_DATA gate")
	}
	if report.FusionGate.Verdict != GateInsufficient {
		t.Errorf("FusionGate.Verdict = %s, want INSUFFICIENT_DATA (no --baseline)", report.FusionGate.Verdict)
	}
	if report.FusionGate.TestedFusionMode != "rrf" {
		t.Errorf("FusionGate.TestedFusionMode = %q, want rrf", report.FusionGate.TestedFusionMode)
	}
}

// TestRun_InvalidMode verifies an unknown --mode value errors.
func TestRun_InvalidMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	content := `{"query": "q1", "expected_top_3": ["X"], "repo": "go-code"}
`
	if err := os.WriteFile(filepath.Join(dir, "go-code.jsonl"), []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	err := run(dir, "http://127.0.0.1:1", "", "", math.NaN(), math.NaN(), "", "", "", "bogus", 1, 20, 1*time.Second)
	if err == nil {
		t.Fatal("expected error for invalid mode, got nil")
	}
	if !strings.Contains(err.Error(), "mode") {
		t.Errorf("error = %v, want it to mention 'mode'", err)
	}
}
