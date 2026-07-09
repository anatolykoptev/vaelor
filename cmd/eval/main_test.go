// Package main — eval harness tests.
//
// Coverage:
//   - matchExpected: lenient label matching (3 forms)
//   - NDCG10 / RecallAtK / MRR: fixed input → known output
//   - parseSemanticXML: real semantic_search XML envelope
//   - HTTP roundtrip via httptest.Server (mirrors REST bridge shape)
//   - pairedTTest: identical inputs → p≈1, large delta → p<0.05
//   - JSON Report stability (round-trip parse)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ──────────────────── matching ────────────────────

func TestMatchExpected_AllForms(t *testing.T) {
	t.Parallel()
	hit := SearchHit{File: "internal/embeddings/rrf.go", Symbol: "MergeRRF"}
	cases := []struct {
		name   string
		expect []string
		want   bool
	}{
		{"symbol-only matches", []string{"MergeRRF"}, true},
		{"symbol-only mismatch", []string{"OtherFunc"}, false},
		{"file:symbol exact", []string{"internal/embeddings/rrf.go:MergeRRF"}, true},
		{"file:symbol suffix", []string{"rrf.go:MergeRRF"}, true},
		{"file:symbol wrong sym", []string{"rrf.go:OtherFunc"}, false},
		{"empty list", []string{}, false},
		{"empty string skipped", []string{"", "MergeRRF"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := matchExpected(hit, tc.expect)
			if got != tc.want {
				t.Errorf("matchExpected(%v) = %v, want %v", tc.expect, got, tc.want)
			}
		})
	}
}

// ──────────────────── metrics ────────────────────

func mkHits(items ...string) []SearchHit {
	out := make([]SearchHit, 0, len(items))
	for i, s := range items {
		// Each item is "file:symbol" or just "symbol" — split on the FIRST colon.
		file, sym, ok := strings.Cut(s, ":")
		if !ok {
			file, sym = "", s
		}
		out = append(out, SearchHit{Position: i + 1, File: file, Symbol: sym})
	}
	return out
}

func TestNDCG10_PerfectRanking(t *testing.T) {
	t.Parallel()
	hits := mkHits("a.go:Foo", "b.go:Bar", "c.go:Baz")
	expected := []string{"Foo", "Bar", "Bar"} // dedupe-ish; expected_top_3 of 3
	got := NDCG10(hits, expected)
	// DCG = 1/log2(2) + 1/log2(3) + 0 = 1 + 0.6309 = 1.6309
	// IDCG@10 = 1 + 1/log2(3) + 1/log2(4) = 1 + 0.6309 + 0.5 = 2.1309
	want := (1.0 + 1.0/math.Log2(3)) / (1.0 + 1.0/math.Log2(3) + 0.5)
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("NDCG10 = %f, want %f", got, want)
	}
}

func TestNDCG10_NoMatch(t *testing.T) {
	t.Parallel()
	hits := mkHits("a.go:X", "b.go:Y")
	got := NDCG10(hits, []string{"Foo", "Bar"})
	if got != 0 {
		t.Errorf("NDCG10 = %f, want 0", got)
	}
}

func TestNDCG10_EmptyExpected(t *testing.T) {
	t.Parallel()
	got := NDCG10(mkHits("a.go:X"), nil)
	if got != 0 {
		t.Errorf("NDCG10 with no labels = %f, want 0", got)
	}
}

func TestRecallAtK(t *testing.T) {
	t.Parallel()
	hits := mkHits("a:Foo", "b:Bar", "c:Baz", "d:Qux")
	expected := []string{"Foo", "Bar", "Missing"}

	if r := RecallAtK(hits, expected, 10); math.Abs(r-2.0/3.0) > 1e-9 {
		t.Errorf("Recall@10 = %f, want %f", r, 2.0/3.0)
	}
	if r := RecallAtK(hits, expected, 1); math.Abs(r-1.0/3.0) > 1e-9 {
		t.Errorf("Recall@1 = %f, want %f", r, 1.0/3.0)
	}
	if r := RecallAtK(hits, []string{}, 10); r != 0 {
		t.Errorf("Recall with empty expected = %f, want 0", r)
	}
}

func TestMRR(t *testing.T) {
	t.Parallel()
	hits := mkHits("a:Wrong", "b:Foo", "c:Bar")
	if got := MRR(hits, []string{"Foo"}); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("MRR first match at 2 = %f, want 0.5", got)
	}
	if got := MRR(hits, []string{"Missing"}); got != 0 {
		t.Errorf("MRR no match = %f, want 0", got)
	}
}

// ──────────────────── XML parsing ────────────────────

const sampleSemanticXML = `<response tool="semantic_search">
  <query>merge rrf</query>
  <repo>go-code</repo>
  <results count="2">
    <result rank="1" distance="0.2345" source="hybrid">
      <file>internal/embeddings/rrf.go</file>
      <symbol kind="function">MergeRRF</symbol>
      <line>30</line>
      <language>go</language>
    </result>
    <result rank="2" distance="0.4500" source="semantic">
      <file>internal/embeddings/expander.go</file>
      <symbol kind="function">Expand</symbol>
      <line>20</line>
      <language>go</language>
    </result>
  </results>
</response>`

func TestParseSemanticXML(t *testing.T) {
	t.Parallel()
	hits, err := parseSemanticXML(sampleSemanticXML)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	if hits[0].Symbol != "MergeRRF" || hits[0].File != "internal/embeddings/rrf.go" {
		t.Errorf("hit 0 = %+v", hits[0])
	}
	if hits[1].Source != "semantic" {
		t.Errorf("hit 1 source = %s, want semantic", hits[1].Source)
	}
}

func TestParseSemanticXML_StatusEnvelope(t *testing.T) {
	t.Parallel()
	body := `<response tool="semantic_search"><status>indexing</status></response>`
	hits, err := parseSemanticXML(body)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected 0 hits on status envelope, got %d", len(hits))
	}
}

// ──────────────────── HTTP roundtrip ────────────────────

func TestMCPClient_Search_RoundTrip(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != restToolPath {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %s", r.Method)
		}
		// Validate request body has expected fields.
		var args map[string]any
		if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
			t.Errorf("decode body: %v", err)
		}
		if args["repo"] != "go-code" || args["query"] != "merge rrf" {
			t.Errorf("unexpected args %v", args)
		}

		resp := restCallToolResp{}
		resp.Content = append(resp.Content, struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: "text", Text: sampleSemanticXML})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewMCPClient(srv.URL)
	hits, err := client.Search(context.Background(), "go-code", "merge rrf", 20)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(hits) != 2 {
		t.Errorf("expected 2 hits, got %d", len(hits))
	}
}

// ──────────────────── runner end-to-end ────────────────────

func TestRunEval_EndToEnd(t *testing.T) {
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

	gset := &GoldenSet{PerRepo: map[string][]GoldenRecord{
		"go-code": {{
			Query:        "merge rrf",
			ExpectedTop3: []string{"MergeRRF"},
			Repo:         "go-code",
		}},
	}}

	results := runEval(context.Background(), NewMCPClient(srv.URL), gset, runnerCfg{Workers: 2, TopK: 20})
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].NDCG10 != 1.0 {
		t.Errorf("nDCG@10 = %f, want 1.0 (rank-1 hit)", results[0].NDCG10)
	}
	if results[0].MRR != 1.0 {
		t.Errorf("MRR = %f, want 1.0", results[0].MRR)
	}
}

// ──────────────────── paired t-test ────────────────────

func TestPairedTTest_IdenticalInputs(t *testing.T) {
	t.Parallel()
	a := []float64{0.5, 0.6, 0.7, 0.8, 0.9}
	mean, p := pairedTTest(a, a)
	if mean != 0 {
		t.Errorf("delta mean = %f, want 0", mean)
	}
	// All differences are zero → variance is 0 → degenerate but well-defined p=1.
	if p != 1.0 {
		t.Errorf("p = %f, want 1.0", p)
	}
}

func TestPairedTTest_LargeDelta(t *testing.T) {
	t.Parallel()
	// Candidate consistently ~0.1 above baseline → strong significance.
	baseline := []float64{0.50, 0.55, 0.60, 0.45, 0.52, 0.58, 0.51, 0.49, 0.53, 0.57}
	candidate := make([]float64, len(baseline))
	for i := range baseline {
		candidate[i] = baseline[i] + 0.1
	}
	mean, p := pairedTTest(candidate, baseline)
	if math.Abs(mean-0.1) > 1e-9 {
		t.Errorf("delta mean = %f, want 0.1", mean)
	}
	if p > 0.05 {
		t.Errorf("p = %f, expected < 0.05 for +0.1 effect with zero variance in delta", p)
	}
}

func TestPairedTTest_NoEffect(t *testing.T) {
	t.Parallel()
	// Symmetric noise around zero → small effect, p > 0.05.
	a := []float64{0.5, 0.55, 0.45, 0.50, 0.52}
	b := []float64{0.51, 0.54, 0.46, 0.49, 0.53}
	_, p := pairedTTest(a, b)
	if p < 0.05 {
		t.Errorf("p = %f, expected > 0.05 for tiny noise effect", p)
	}
}

func TestPairedTTest_StudentTReference(t *testing.T) {
	t.Parallel()
	// Spot-check Student-t two-tailed CDF against a published reference value:
	// t = 2.776, df = 4 → two-tailed p ≈ 0.05. (Standard t-table critical value.)
	got := studentTTwoTailed(2.776, 4)
	if math.Abs(got-0.05) > 1e-3 {
		t.Errorf("studentTTwoTailed(2.776, 4) = %f, want ≈0.05", got)
	}
}

// ──────────────────── JSON output stability ────────────────────

func TestReport_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	in := Report{
		Metadata: Metadata{
			Timestamp: time.Date(2026, 4, 29, 12, 0, 0, 0, time.UTC),
			TargetURL: "http://x",
			GitSHA:    "deadbeef",
			GoldenDir: "eval/golden",
			TopK:      20,
		},
		PerQuery: []QueryResult{
			{Repo: "go-code", Query: "q", Expected: []string{"X"}, NDCG10: 0.5, Recall10: 0.5, MRR: 0.5},
		},
		PerRepo: []PerRepoAggregate{
			{Repo: "go-code", Aggregates: Aggregates{NDCG10: 0.5, Queries: 1}},
		},
		Aggregates: Aggregates{NDCG10: 0.5, Queries: 1},
	}
	if err := writeReport(path, in); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := readReport(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if out.Aggregates.NDCG10 != 0.5 || len(out.PerQuery) != 1 {
		t.Errorf("round-trip mismatch: %+v", out)
	}
}

// ──────────────────── golden loader ────────────────────

func TestLoadGolden(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	content := `# comment line, skipped
{"query": "q1", "expected_top_3": ["A"]}

{"query": "q2", "expected_top_3": ["B", "C"], "notes": "labeled"}
`
	if err := os.WriteFile(filepath.Join(dir, "go-code.jsonl"), []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	g, err := LoadGolden(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(g.PerRepo["go-code"]) != 2 {
		t.Errorf("expected 2 records, got %d", len(g.PerRepo["go-code"]))
	}
	if g.PerRepo["go-code"][0].Repo != "go-code" {
		t.Errorf("expected repo injected, got %q", g.PerRepo["go-code"][0].Repo)
	}
}

func TestLoadGolden_RejectsBadRecords(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bad := `{"query": "", "expected_top_3": ["X"]}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "x.jsonl"), []byte(bad), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadGolden(dir); err == nil {
		t.Error("expected error for empty query")
	}
}

// ──────────────────── SPLADE A/B gate ────────────────────

// makeResults builds a synthetic []QueryResult slice for gate tests.
// Each query q_N gets nDCG10=ndcgBase+ndcgDelta and Recall20=r20Base+r20Delta.
func makeResults(repo string, n int, ndcg, r20 float64) []QueryResult {
	out := make([]QueryResult, n)
	for i := range n {
		out[i] = QueryResult{
			Repo:     repo,
			Query:    fmt.Sprintf("q%d", i),
			NDCG10:   ndcg,
			Recall20: r20,
		}
	}
	return out
}

// TestEvaluateGate_Pass verifies that a statistically significant nDCG@10
// improvement with non-inferior Recall@20 yields GatePass.
//
// Falsification: remove the ndcgSig condition in EvaluateGate (always false)
// → verdict becomes GateFail and this test goes RED.
func TestEvaluateGate_Pass(t *testing.T) {
	t.Parallel()
	// Candidate consistently +0.12 on nDCG, +0.05 on Recall20.
	// With 20 pairs and zero within-pair variance this is maximally significant.
	n := 20
	bl := makeResults("r", n, 0.50, 0.60)
	cn := makeResults("r", n, 0.62, 0.65)

	g := EvaluateGate(bl, cn, 0.3)

	if g.Verdict != GatePass {
		t.Errorf("verdict = %s, want PASS; ndcg_delta=%.4f p=%.4f recall20_delta=%.4f p=%.4f",
			g.Verdict, g.NDCG10Delta, g.NDCG10P, g.Recall20Delta, g.Recall20P)
	}
	if !g.NDCGSignificant {
		t.Error("NDCGSignificant = false, want true")
	}
	if !g.Recall20NonInferior {
		t.Error("Recall20NonInferior = false, want true")
	}
	if g.PairedQueries != n {
		t.Errorf("PairedQueries = %d, want %d", g.PairedQueries, n)
	}
	if g.TestedWeight != 0.3 {
		t.Errorf("TestedWeight = %f, want 0.3", g.TestedWeight)
	}
}

// TestEvaluateGate_Fail_NoSignificance verifies that a small, noisy nDCG
// improvement that does not clear p<0.05 yields GateFail.
//
// Falsification: lower pAlpha to 0.5 in abgate.go (always "significant") →
// verdict flips to PASS and this test goes RED.
func TestEvaluateGate_Fail_NoSignificance(t *testing.T) {
	t.Parallel()
	// Symmetric noise: tiny alternating effect, zero net improvement.
	// pairedTTest will return p≈1.
	bl := []QueryResult{
		{Repo: "r", Query: "q0", NDCG10: 0.50, Recall20: 0.60},
		{Repo: "r", Query: "q1", NDCG10: 0.55, Recall20: 0.65},
		{Repo: "r", Query: "q2", NDCG10: 0.45, Recall20: 0.55},
		{Repo: "r", Query: "q3", NDCG10: 0.52, Recall20: 0.62},
		{Repo: "r", Query: "q4", NDCG10: 0.48, Recall20: 0.58},
	}
	// Candidate differs by +0.01/-0.01 alternating → near-zero mean delta, high p.
	cn := []QueryResult{
		{Repo: "r", Query: "q0", NDCG10: 0.51, Recall20: 0.61},
		{Repo: "r", Query: "q1", NDCG10: 0.54, Recall20: 0.64},
		{Repo: "r", Query: "q2", NDCG10: 0.46, Recall20: 0.56},
		{Repo: "r", Query: "q3", NDCG10: 0.51, Recall20: 0.61},
		{Repo: "r", Query: "q4", NDCG10: 0.49, Recall20: 0.59},
	}

	g := EvaluateGate(bl, cn, 1.0)

	if g.Verdict != GateFail {
		t.Errorf("verdict = %s, want FAIL; ndcg_delta=%.4f p=%.4f",
			g.Verdict, g.NDCG10Delta, g.NDCG10P)
	}
	if g.NDCGSignificant {
		t.Errorf("NDCGSignificant = true, want false (p=%.4f should be > 0.05)", g.NDCG10P)
	}
}

// TestEvaluateGate_Fail_Recall20Regression verifies that a significant nDCG
// gain paired with a significant Recall@20 regression yields GateFail.
//
// Falsification: remove the !r20NonInf branch in EvaluateGate (collapse to
// single ndcgSig check) → verdict flips to PASS and this test goes RED.
func TestEvaluateGate_Fail_Recall20Regression(t *testing.T) {
	t.Parallel()
	// nDCG improves significantly (+0.15 with zero variance → p≈0).
	// Recall@20 regresses significantly (-0.05, beyond nonInferiorMargin=0.02).
	n := 15
	bl := makeResults("r", n, 0.50, 0.70)
	cn := makeResults("r", n, 0.65, 0.65) // R20 drops 0.05 > margin 0.02

	g := EvaluateGate(bl, cn, 0.5)

	if g.Verdict != GateFail {
		t.Errorf("verdict = %s, want FAIL (Recall@20 regressed beyond margin); "+
			"ndcg_delta=%.4f ndcg_p=%.4f recall20_delta=%.4f recall20_p=%.4f",
			g.Verdict, g.NDCG10Delta, g.NDCG10P, g.Recall20Delta, g.Recall20P)
	}
	if g.Recall20NonInferior {
		t.Errorf("Recall20NonInferior = true, want false (delta=%.4f < -%.2f margin)",
			g.Recall20Delta, nonInferiorMargin)
	}
}

// TestEvaluateGate_InsufficientData verifies the INSUFFICIENT_DATA verdict
// when fewer than 2 paired queries exist.
//
// Falsification: lower the pairs<2 threshold in EvaluateGate to pairs<0 →
// the insufficient branch never fires and verdict becomes PASS/FAIL instead
// of INSUFFICIENT_DATA; test goes RED.
func TestEvaluateGate_InsufficientData(t *testing.T) {
	t.Parallel()
	// Only 1 query in baseline → 1 pair at most.
	bl := []QueryResult{{Repo: "r", Query: "q0", NDCG10: 0.5, Recall20: 0.6}}
	cn := []QueryResult{{Repo: "r", Query: "q0", NDCG10: 0.7, Recall20: 0.7}}

	g := EvaluateGate(bl, cn, 0.3)

	if g.Verdict != GateInsufficient {
		t.Errorf("verdict = %s, want INSUFFICIENT_DATA", g.Verdict)
	}
}

// TestEvaluateGate_TestedWeightRecorded verifies the tested weight is carried
// through to GateResult regardless of verdict.
func TestEvaluateGate_TestedWeightRecorded(t *testing.T) {
	t.Parallel()
	bl := makeResults("r", 10, 0.5, 0.6)
	cn := makeResults("r", 10, 0.5, 0.6) // identical → FAIL (no sig improvement)

	g := EvaluateGate(bl, cn, 0.42)

	if g.TestedWeight != 0.42 {
		t.Errorf("TestedWeight = %f, want 0.42", g.TestedWeight)
	}
}

// TestEvaluateGate_ErrorQueriesExcluded verifies that queries with a non-empty
// Error field are excluded from pairing (error on one side → pair is skipped).
//
// Falsification: remove the `if r.Error != ""` guards in EvaluateGate →
// the errored pair is included, the pair count goes from 1 to 2, and since
// the errored query has zero-value metrics the aggregate shifts; for the
// INSUFFICIENT_DATA case (only 1 healthy pair) the verdict may flip to
// GateFail which would red-fail this test.
func TestEvaluateGate_ErrorQueriesExcluded(t *testing.T) {
	t.Parallel()
	bl := []QueryResult{
		{Repo: "r", Query: "ok", NDCG10: 0.5, Recall20: 0.6},
		{Repo: "r", Query: "bad", NDCG10: 0.9, Recall20: 0.9, Error: "embed timeout"},
	}
	cn := []QueryResult{
		{Repo: "r", Query: "ok", NDCG10: 0.7, Recall20: 0.7},
		{Repo: "r", Query: "bad", NDCG10: 0.1, Recall20: 0.1, Error: "embed timeout"},
	}

	g := EvaluateGate(bl, cn, 0.3)

	// Only 1 healthy pair → INSUFFICIENT_DATA (not PASS/FAIL from polluted metrics).
	if g.Verdict != GateInsufficient {
		t.Errorf("verdict = %s, want INSUFFICIENT_DATA (1 healthy pair); "+
			"paired=%d", g.Verdict, g.PairedQueries)
	}
	if g.PairedQueries != 1 {
		t.Errorf("PairedQueries = %d, want 1", g.PairedQueries)
	}
}

// ──────────────────── delta computation ────────────────────

func TestComputeDelta_PairedQueriesOnly(t *testing.T) {
	t.Parallel()
	baseline := []QueryResult{
		{Repo: "x", Query: "q1", NDCG10: 0.4, Recall10: 0.4, Recall20: 0.4, MRR: 0.4},
		{Repo: "x", Query: "q2", NDCG10: 0.5, Recall10: 0.5, Recall20: 0.5, MRR: 0.5},
		{Repo: "x", Query: "q3", NDCG10: 0.6, Recall10: 0.6, Recall20: 0.6, MRR: 0.6},
		{Repo: "x", Query: "q4", NDCG10: 0.7, Recall10: 0.7, Recall20: 0.7, MRR: 0.7},
	}
	candidate := []QueryResult{
		{Repo: "x", Query: "q1", NDCG10: 0.5, Recall10: 0.5, Recall20: 0.5, MRR: 0.5},
		{Repo: "x", Query: "q2", NDCG10: 0.6, Recall10: 0.6, Recall20: 0.6, MRR: 0.6},
		{Repo: "x", Query: "q3", NDCG10: 0.7, Recall10: 0.7, Recall20: 0.7, MRR: 0.7},
		{Repo: "x", Query: "q4", NDCG10: 0.8, Recall10: 0.8, Recall20: 0.8, MRR: 0.8},
	}
	delta := computeDelta(baseline, candidate)
	if delta == nil {
		t.Fatal("delta is nil")
	}
	if !strings.HasPrefix(delta.NDCG10, "+0.1000") {
		t.Errorf("expected +0.1000 nDCG delta prefix, got %q", delta.NDCG10)
	}
}
