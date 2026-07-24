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
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// captureSlog swaps slog.Default() with a logger backed by testHandler so
// tests can assert on emitted records. Returns the captured records and a
// cleanup that restores the original default logger.
//
// Tests using this helper MUST NOT call t.Parallel() (global slog.Default).
func captureSlog(t *testing.T) (*testHandler, func()) {
	t.Helper()
	th := &testHandler{}
	orig := slog.Default()
	slog.SetDefault(slog.New(th))
	return th, func() { slog.SetDefault(orig) }
}

// testHandler captures slog records for level/attr assertions.
type testHandler struct {
	records []slog.Record
}

func (h *testHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *testHandler) Handle(_ context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	return nil
}
func (h *testHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *testHandler) WithGroup(_ string) slog.Handler      { return h }

// warnContainsAttr searches captured Warn records for one whose Message
// contains msgSubstr AND has an attr with the given key whose value
// contains valSubstr.
func warnContainsAttr(records []slog.Record, msgSubstr, attrKey, valSubstr string) bool {
	for _, r := range records {
		if r.Level != slog.LevelWarn {
			continue
		}
		if !strings.Contains(r.Message, msgSubstr) {
			continue
		}
		found := false
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == attrKey && strings.Contains(a.Value.String(), valSubstr) {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

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
	if !errors.Is(err, ErrTransient{}) {
		t.Fatalf("expected ErrTransient for indexing status, got hits=%v err=%v", hits, err)
	}
	if hits != nil {
		t.Errorf("expected nil hits on transient status envelope, got %d", len(hits))
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
	hits, err := client.Search(context.Background(), "go-code", "merge rrf", "", 20)
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

// ──────────────────── latency measurement ────────────────────

// TestRunSingle_RecordsLatency verifies that runSingle records a non-zero
// latency for the semantic_search call.
//
// Falsification: remove the `searchStart := time.Now()` / `out.Latency = ...`
// lines in runSingle → Latency and LatencyMS stay zero → RED.
func TestRunSingle_RecordsLatency(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Millisecond) // ensure measurable latency
		resp := restCallToolResp{}
		resp.Content = append(resp.Content, struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: "text", Text: sampleSemanticXML})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	rec := GoldenRecord{Query: "merge rrf", ExpectedTop3: []string{"MergeRRF"}, Repo: "go-code"}
	result := runSingle(context.Background(), NewMCPClient(srv.URL), rec, runnerCfg{TopK: 20})
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Latency <= 0 {
		t.Errorf("Latency = %v, want > 0", result.Latency)
	}
	if result.LatencyMS <= 0 {
		t.Errorf("LatencyMS = %f, want > 0", result.LatencyMS)
	}
	// 5ms sleep + HTTP overhead — should be at least 1ms.
	if result.LatencyMS < 1.0 {
		t.Errorf("LatencyMS = %f, want >= 1.0 (server slept 5ms)", result.LatencyMS)
	}
}

// TestComputeAggregates_LatencyPercentiles verifies p50/p95/mean are computed
// correctly over a known set of per-query latencies.
//
// Falsification: remove the computeLatencyStats call in computeAggregates →
// all latency stats stay zero → RED.
func TestComputeAggregates_LatencyPercentiles(t *testing.T) {
	t.Parallel()
	// 10 queries with latencies 1..10 ms.
	results := make([]QueryResult, 10)
	for i := range results {
		results[i] = QueryResult{
			Repo:      "r",
			Query:     fmt.Sprintf("q%d", i),
			NDCG10:    0.5,
			LatencyMS: float64(i + 1), // 1, 2, ..., 10
		}
	}
	agg := computeAggregates(results)
	// Mean = (1+2+...+10)/10 = 5.5
	if math.Abs(agg.MeanMS-5.5) > 1e-9 {
		t.Errorf("MeanMS = %f, want 5.5", agg.MeanMS)
	}
	// Nearest-rank p50: rank = ceil(0.50 * 10) = 5 → sorted[4] = 5.0
	if math.Abs(agg.P50MS-5.0) > 1e-9 {
		t.Errorf("P50MS = %f, want 5.0", agg.P50MS)
	}
	// Nearest-rank p95: rank = ceil(0.95 * 10) = 10 → sorted[9] = 10.0
	if math.Abs(agg.P95MS-10.0) > 1e-9 {
		t.Errorf("P95MS = %f, want 10.0", agg.P95MS)
	}
	// Nearest-rank p99: rank = ceil(0.99 * 10) = 10 → sorted[9] = 10.0
	if math.Abs(agg.P99MS-10.0) > 1e-9 {
		t.Errorf("P99MS = %f, want 10.0", agg.P99MS)
	}
}

// TestComputeAggregates_LatencyExcludesErrors verifies that error queries are
// excluded from latency percentiles (consistent with relevance metric means).
func TestComputeAggregates_LatencyExcludesErrors(t *testing.T) {
	t.Parallel()
	results := []QueryResult{
		{Repo: "r", Query: "q0", NDCG10: 0.5, LatencyMS: 10.0, Error: "timeout"},
		{Repo: "r", Query: "q1", NDCG10: 0.5, LatencyMS: 5.0},
		{Repo: "r", Query: "q2", NDCG10: 0.5, LatencyMS: 15.0},
	}
	agg := computeAggregates(results)
	if agg.Errors != 1 {
		t.Errorf("Errors = %d, want 1", agg.Errors)
	}
	if agg.Queries != 2 {
		t.Errorf("Queries = %d, want 2", agg.Queries)
	}
	// Mean over non-error: (5 + 15) / 2 = 10.0
	if math.Abs(agg.MeanMS-10.0) > 1e-9 {
		t.Errorf("MeanMS = %f, want 10.0 (error excluded)", agg.MeanMS)
	}
}

// ──────────────────── per-language breakdown ────────────────────

// TestComputePerLanguage_SplitsByLanguage verifies that a 2-language fixture
// splits into 2 buckets + that each bucket has the correct per-bucket mean.
//
// Falsification: make computePerLanguage group everything under "unspecified"
// (remove the `lang := r.Language` assignment) → only 1 bucket → RED.
func TestComputePerLanguage_SplitsByLanguage(t *testing.T) {
	t.Parallel()
	results := []QueryResult{
		{Repo: "r", Query: "q0", Language: "go", NDCG10: 0.8, LatencyMS: 10},
		{Repo: "r", Query: "q1", Language: "go", NDCG10: 0.6, LatencyMS: 20},
		{Repo: "r", Query: "q2", Language: "python", NDCG10: 0.4, LatencyMS: 30},
		{Repo: "r", Query: "q3", Language: "python", NDCG10: 0.2, LatencyMS: 40},
	}
	perLang := computePerLanguage(results)
	if len(perLang) != 2 {
		t.Fatalf("expected 2 language buckets, got %d: %+v", len(perLang), perLang)
	}
	// Alphabetical: "go" first, then "python".
	if perLang[0].Language != "go" {
		t.Errorf("bucket 0 language = %q, want go", perLang[0].Language)
	}
	if perLang[1].Language != "python" {
		t.Errorf("bucket 1 language = %q, want python", perLang[1].Language)
	}
	// go: mean nDCG = (0.8 + 0.6) / 2 = 0.7
	if math.Abs(perLang[0].NDCG10-0.7) > 1e-9 {
		t.Errorf("go nDCG10 = %f, want 0.7", perLang[0].NDCG10)
	}
	// python: mean nDCG = (0.4 + 0.2) / 2 = 0.3
	if math.Abs(perLang[1].NDCG10-0.3) > 1e-9 {
		t.Errorf("python nDCG10 = %f, want 0.3", perLang[1].NDCG10)
	}
	// go: mean latency = (10 + 20) / 2 = 15
	if math.Abs(perLang[0].MeanMS-15.0) > 1e-9 {
		t.Errorf("go MeanMS = %f, want 15.0", perLang[0].MeanMS)
	}
	// python: mean latency = (30 + 40) / 2 = 35
	if math.Abs(perLang[1].MeanMS-35.0) > 1e-9 {
		t.Errorf("python MeanMS = %f, want 35.0", perLang[1].MeanMS)
	}
}

// TestComputePerLanguage_UnspecifiedBucket verifies that records without a
// language field aggregate under "unspecified".
func TestComputePerLanguage_UnspecifiedBucket(t *testing.T) {
	t.Parallel()
	results := []QueryResult{
		{Repo: "r", Query: "q0", NDCG10: 0.5},
		{Repo: "r", Query: "q1", Language: "go", NDCG10: 0.9},
	}
	perLang := computePerLanguage(results)
	if len(perLang) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(perLang))
	}
	// Alphabetical: "go" < "unspecified"
	if perLang[0].Language != "go" {
		t.Errorf("bucket 0 = %q, want go", perLang[0].Language)
	}
	if perLang[1].Language != "unspecified" {
		t.Errorf("bucket 1 = %q, want unspecified", perLang[1].Language)
	}
}

// TestRunSingle_PassesLanguageFilter verifies that when a GoldenRecord has a
// language field, it is passed as the `language` arg to semantic_search.
//
// Falsification: remove `Language: rec.Language` from runSingle's QueryResult
// and the `language` param in client.Search → the server sees no language arg
// → the test's arg check fails → RED.
func TestRunSingle_PassesLanguageFilter(t *testing.T) {
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
		}{Type: "text", Text: sampleSemanticXML})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	rec := GoldenRecord{Query: "merge rrf", ExpectedTop3: []string{"MergeRRF"}, Repo: "go-code", Language: "go"}
	result := runSingle(context.Background(), NewMCPClient(srv.URL), rec, runnerCfg{TopK: 20})
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if result.Language != "go" {
		t.Errorf("result.Language = %q, want go", result.Language)
	}
	if seenLanguage != "go" {
		t.Errorf("server received language = %v, want go", seenLanguage)
	}
}

// TestRunSingle_NoLanguageFilterWhenEmpty verifies that when Language is empty,
// no `language` arg is sent (byte-identical to pre-instrumentation behavior).
func TestRunSingle_NoLanguageFilterWhenEmpty(t *testing.T) {
	t.Parallel()
	var hadLanguageKey bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var args map[string]any
		_ = json.NewDecoder(r.Body).Decode(&args)
		_, hadLanguageKey = args["language"]
		resp := restCallToolResp{}
		resp.Content = append(resp.Content, struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: "text", Text: sampleSemanticXML})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	rec := GoldenRecord{Query: "merge rrf", ExpectedTop3: []string{"MergeRRF"}, Repo: "go-code"}
	result := runSingle(context.Background(), NewMCPClient(srv.URL), rec, runnerCfg{TopK: 20})
	if result.Error != "" {
		t.Fatalf("unexpected error: %s", result.Error)
	}
	if hadLanguageKey {
		t.Error("server received language arg for empty-language record; should be omitted")
	}
}

// ──────────────────── repo-map ────────────────────

// TestParseRepoMap_Valid verifies that a comma-separated key=path string
// parses into the expected map.
func TestParseRepoMap_Valid(t *testing.T) {
	t.Parallel()
	m, err := ParseRepoMap("go-code=/host/src/go-code,MemDB=/host/src/MemDB")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m["go-code"] != "/host/src/go-code" {
		t.Errorf("go-code = %q", m["go-code"])
	}
	if m["MemDB"] != "/host/src/MemDB" {
		t.Errorf("MemDB = %q", m["MemDB"])
	}
}

// TestParseRepoMap_Empty verifies that empty input returns nil (no error).
func TestParseRepoMap_Empty(t *testing.T) {
	t.Parallel()
	m, err := ParseRepoMap("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != nil {
		t.Errorf("expected nil map, got %v", m)
	}
}

// TestParseRepoMap_MissingEquals verifies that an entry without '=' errors.
func TestParseRepoMap_MissingEquals(t *testing.T) {
	t.Parallel()
	if _, err := ParseRepoMap("go-code"); err == nil {
		t.Error("expected error for entry without '='")
	}
}

// TestApplyRepoMap_OverridesPath verifies that ApplyRepoMap replaces the
// record's Repo with the mapped path for the matching repo_key.
//
// Falsification: remove the `g.PerRepo[repoKey][i].Repo = mapped` line in
// ApplyRepoMap → Repo stays "/path/to/repo" → RED.
func TestApplyRepoMap_OverridesPath(t *testing.T) {
	t.Parallel()
	gset := &GoldenSet{PerRepo: map[string][]GoldenRecord{
		"go-code": {
			{Query: "q1", ExpectedTop3: []string{"X"}, Repo: "/path/to/repo"},
			{Query: "q2", ExpectedTop3: []string{"Y"}, Repo: "/path/to/repo"},
		},
		"MemDB": {
			{Query: "q3", ExpectedTop3: []string{"Z"}, Repo: "/path/to/repos/src/MemDB"},
		},
	}}
	repoMap := map[string]string{
		"go-code": "/host/src/go-code",
	}
	gset.ApplyRepoMap(repoMap)

	if gset.PerRepo["go-code"][0].Repo != "/host/src/go-code" {
		t.Errorf("go-code[0].Repo = %q, want /host/src/go-code", gset.PerRepo["go-code"][0].Repo)
	}
	if gset.PerRepo["go-code"][1].Repo != "/host/src/go-code" {
		t.Errorf("go-code[1].Repo = %q, want /host/src/go-code", gset.PerRepo["go-code"][1].Repo)
	}
	// MemDB not in map → falls back to record's own path.
	if gset.PerRepo["MemDB"][0].Repo != "/path/to/repos/src/MemDB" {
		t.Errorf("MemDB[0].Repo = %q, want /path/to/repos/src/MemDB (fallback)", gset.PerRepo["MemDB"][0].Repo)
	}
}

// TestApplyRepoMap_FallbackWhenAbsent verifies that an empty map leaves all
// records' Repo fields unchanged.
func TestApplyRepoMap_FallbackWhenAbsent(t *testing.T) {
	t.Parallel()
	gset := &GoldenSet{PerRepo: map[string][]GoldenRecord{
		"go-code": {{Query: "q1", ExpectedTop3: []string{"X"}, Repo: "/path/to/repo"}},
	}}
	gset.ApplyRepoMap(nil)
	if gset.PerRepo["go-code"][0].Repo != "/path/to/repo" {
		t.Errorf("Repo = %q, want /path/to/repo (unchanged)", gset.PerRepo["go-code"][0].Repo)
	}
}

// TestApplyRepoMap_SlugKeyFallback verifies that a repo-map keyed by the
// record's Repo slug (not the golden file basename) also maps correctly.
// This covers callers that key the map by the record's Repo field instead
// of the .jsonl file basename — both keying conventions must work.
//
// Falsification: remove the slug-fallback branch in ApplyRepoMap → the
// slug-keyed entry never matches → Repo stays "github.com/.../go-code" → RED.
func TestApplyRepoMap_SlugKeyFallback(t *testing.T) {
	t.Parallel()
	gset := &GoldenSet{PerRepo: map[string][]GoldenRecord{
		"go-code": {
			{Query: "q1", ExpectedTop3: []string{"X"}, Repo: "github.com/anatolykoptev/go-code"},
		},
	}}
	repoMap := map[string]string{
		"github.com/anatolykoptev/go-code": "/host/src/go-code",
	}
	unmatched := gset.ApplyRepoMap(repoMap)

	if gset.PerRepo["go-code"][0].Repo != "/host/src/go-code" {
		t.Errorf("slug-keyed map: Repo = %q, want /host/src/go-code",
			gset.PerRepo["go-code"][0].Repo)
	}
	if len(unmatched) != 0 {
		t.Errorf("slug-keyed map: unmatched = %v, want empty", unmatched)
	}
}

// TestApplyRepoMap_UnmatchedKeyWarns verifies that a repo-map key matching
// no golden basename AND no record Repo slug is reported as unmatched
// (returned in the unmatched set AND logged as WARN). A silent unused
// mapping key is almost always a typo/mismatch.
//
// Falsification: remove the unmatched-tracking in ApplyRepoMap → unmatched
// is nil and no WARN fires → RED.
func TestApplyRepoMap_UnmatchedKeyWarns(t *testing.T) {
	// NOT parallel — captures global slog.Default.
	th, restore := captureSlog(t)
	defer restore()

	gset := &GoldenSet{PerRepo: map[string][]GoldenRecord{
		"go-code": {
			{Query: "q1", ExpectedTop3: []string{"X"}, Repo: "/path/to/repo"},
		},
	}}
	repoMap := map[string]string{
		"go-code":   "/host/src/go-code", // matches basename
		"typo-repo": "/host/src/typo",    // matches nothing
	}
	unmatched := gset.ApplyRepoMap(repoMap)

	// Basename key still maps (back-compat).
	if gset.PerRepo["go-code"][0].Repo != "/host/src/go-code" {
		t.Errorf("basename key: Repo = %q, want /host/src/go-code",
			gset.PerRepo["go-code"][0].Repo)
	}
	if len(unmatched) != 1 || unmatched[0] != "typo-repo" {
		t.Errorf("unmatched = %v, want [typo-repo]", unmatched)
	}
	if !warnContainsAttr(th.records, "repo-map", "repo_map_key", "typo-repo") {
		t.Error("expected WARN about unmatched repo-map key 'typo-repo', got none")
	}
}

// ──────────────────── keyword-arm gate ────────────────────

// TestEvaluateKeywordArmGate_Pass verifies that a significant nDCG@10
// improvement with non-inferior Recall@20 yields PASS and records the arm.
//
// Falsification: remove the `ndcgSig && r20NonInf` case in evaluateGate →
// verdict never PASS → RED.
func TestEvaluateKeywordArmGate_Pass(t *testing.T) {
	t.Parallel()
	n := 20
	bl := makeResults("r", n, 0.50, 0.60)
	cn := makeResults("r", n, 0.62, 0.65)

	g := EvaluateKeywordArmGate(bl, cn, "bm25f")

	if g.Verdict != GatePass {
		t.Errorf("verdict = %s, want PASS", g.Verdict)
	}
	if g.TestedArm != "bm25f" {
		t.Errorf("TestedArm = %q, want bm25f", g.TestedArm)
	}
	if !strings.Contains(g.RecommendedAction, "KEYWORD_ARM=bm25f") {
		t.Errorf("RecommendedAction = %q, want to contain KEYWORD_ARM=bm25f", g.RecommendedAction)
	}
}

// TestEvaluateKeywordArmGate_Fail verifies that no significant improvement
// yields FAIL with the keyword-arm action text.
func TestEvaluateKeywordArmGate_Fail(t *testing.T) {
	t.Parallel()
	bl := makeResults("r", 10, 0.5, 0.6)
	cn := makeResults("r", 10, 0.5, 0.6) // identical → no improvement

	g := EvaluateKeywordArmGate(bl, cn, "bm25f")

	if g.Verdict != GateFail {
		t.Errorf("verdict = %s, want FAIL", g.Verdict)
	}
	if !strings.Contains(g.RecommendedAction, "KEYWORD_ARM") {
		t.Errorf("RecommendedAction = %q, want to mention KEYWORD_ARM", g.RecommendedAction)
	}
}

// ──────────────────── fusion-mode skip ────────────────────

// TestFusionSkipResult_NotExercised verifies that the fusion-mode gate
// reports NOT_EXERCISED with the mandated message.
//
// Falsification: change FusionSkipResult to return GatePass → verdict
// mismatch → RED. Or remove the "semantic_search only" phrase → RED.
func TestFusionSkipResult_NotExercised(t *testing.T) {
	t.Parallel()
	g := FusionSkipResult("rrf")
	if g.Verdict != GateNotExercised {
		t.Errorf("verdict = %s, want NOT_EXERCISED", g.Verdict)
	}
	if g.TestedFusionMode != "rrf" {
		t.Errorf("TestedFusionMode = %q, want rrf", g.TestedFusionMode)
	}
	if !strings.Contains(g.Explanation, "fusion mode not exercised") {
		t.Errorf("Explanation = %q, want to contain 'fusion mode not exercised'", g.Explanation)
	}
	if !strings.Contains(g.Explanation, "semantic_search only") {
		t.Errorf("Explanation = %q, want to contain 'semantic_search only'", g.Explanation)
	}
	if !strings.Contains(g.Explanation, "repo_analyze") {
		t.Errorf("Explanation = %q, want to mention repo_analyze", g.Explanation)
	}
}

// ──────────────────── flag parsing + run integration ────────────────────

// TestRun_KeywordArmAndFusionModeFlags verifies that --keyword-arm and
// --fusion-mode flags are parsed, recorded in metadata, and emit the
// correct gate fields in the report.
//
// Falsification: remove the keyword-arm gate emission in run() →
// report.KeywordArmGate is nil → RED. Or remove the fusion gate emission →
// report.FusionGate is nil → RED.
func TestRun_KeywordArmAndFusionModeFlags(t *testing.T) {
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

	// Create a golden dir with 2 queries so the gate has enough pairs.
	dir := t.TempDir()
	content := `{"query": "q1", "expected_top_3": ["MergeRRF"], "repo": "go-code"}
{"query": "q2", "expected_top_3": ["MergeRRF"], "repo": "go-code"}
`
	if err := os.WriteFile(filepath.Join(dir, "go-code.jsonl"), []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	// First run: baseline (no gates).
	baselinePath := filepath.Join(t.TempDir(), "baseline.json")
	if err := run(dir, srv.URL, baselinePath, "", math.NaN(), math.NaN(), "", "", "", modeSemanticSearch, 2, 20, 30*time.Second, 1, false, 0); err != nil {
		t.Fatalf("baseline run: %v", err)
	}

	// Second run: candidate with --keyword-arm=bm25f and --fusion-mode=rrf.
	outPath := filepath.Join(t.TempDir(), "cand.json")
	if err := run(dir, srv.URL, outPath, baselinePath, math.NaN(), math.NaN(), "bm25f", "rrf", "", modeSemanticSearch, 2, 20, 30*time.Second, 1, false, 0); err != nil {
		t.Fatalf("candidate run: %v", err)
	}

	report, err := readReport(outPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if report.Metadata.KeywordArm != "bm25f" {
		t.Errorf("metadata.KeywordArm = %q, want bm25f", report.Metadata.KeywordArm)
	}
	if report.Metadata.FusionMode != "rrf" {
		t.Errorf("metadata.FusionMode = %q, want rrf", report.Metadata.FusionMode)
	}
	if report.KeywordArmGate == nil {
		t.Error("KeywordArmGate is nil, want non-nil")
	} else if report.KeywordArmGate.TestedArm != "bm25f" {
		t.Errorf("KeywordArmGate.TestedArm = %q, want bm25f", report.KeywordArmGate.TestedArm)
	}
	if report.FusionGate == nil {
		t.Error("FusionGate is nil, want non-nil")
	} else if report.FusionGate.Verdict != GateNotExercised {
		t.Errorf("FusionGate.Verdict = %s, want NOT_EXERCISED", report.FusionGate.Verdict)
	}
	// Per-language should be present (all "unspecified" since no language field).
	if len(report.PerLanguage) == 0 {
		t.Error("PerLanguage is empty, want at least 1 bucket")
	}
}

// TestRun_RepoMapFlag verifies that --repo-map resolves placeholder paths
// and that the resolved path is sent to the server.
//
// Falsification: remove the `golden.ApplyRepoMap(repoMap)` call in run() →
// the server sees "/path/to/repo" instead of the mapped path → RED.
func TestRun_RepoMapFlag(t *testing.T) {
	t.Parallel()
	var seenRepo string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var args map[string]any
		_ = json.NewDecoder(r.Body).Decode(&args)
		if r, ok := args["repo"].(string); ok {
			seenRepo = r
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

	dir := t.TempDir()
	content := `{"query": "q1", "expected_top_3": ["MergeRRF"], "repo": "/path/to/repo"}
`
	if err := os.WriteFile(filepath.Join(dir, "go-code.jsonl"), []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	outPath := filepath.Join(t.TempDir(), "out.json")
	repoMap := "go-code=/host/src/go-code"
	if err := run(dir, srv.URL, outPath, "", math.NaN(), math.NaN(), "", "", repoMap, modeSemanticSearch, 1, 20, 10*time.Second, 1, false, 0); err != nil {
		t.Fatalf("run: %v", err)
	}

	if seenRepo != "/host/src/go-code" {
		t.Errorf("server received repo = %q, want /host/src/go-code", seenRepo)
	}
	report, err := readReport(outPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if report.Metadata.RepoMapPath != repoMap {
		t.Errorf("metadata.RepoMapPath = %q, want %q", report.Metadata.RepoMapPath, repoMap)
	}
}

// TestRun_RepoMapFallback verifies that when no --repo-map is given, the
// record's own path is used (fallback).
func TestRun_RepoMapFallback(t *testing.T) {
	t.Parallel()
	var seenRepo string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var args map[string]any
		_ = json.NewDecoder(r.Body).Decode(&args)
		if r, ok := args["repo"].(string); ok {
			seenRepo = r
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

	dir := t.TempDir()
	content := `{"query": "q1", "expected_top_3": ["MergeRRF"], "repo": "/path/to/repo"}
`
	if err := os.WriteFile(filepath.Join(dir, "go-code.jsonl"), []byte(content), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	outPath := filepath.Join(t.TempDir(), "out.json")
	if err := run(dir, srv.URL, outPath, "", math.NaN(), math.NaN(), "", "", "", modeSemanticSearch, 1, 20, 10*time.Second, 1, false, 0); err != nil {
		t.Fatalf("run: %v", err)
	}

	if seenRepo != "/path/to/repo" {
		t.Errorf("server received repo = %q, want /path/to/repo (fallback)", seenRepo)
	}
}

// TestComputeDelta_IncludesLatency verifies that the delta block includes a
// latency delta string.
func TestComputeDelta_IncludesLatency(t *testing.T) {
	t.Parallel()
	baseline := []QueryResult{
		{Repo: "x", Query: "q1", NDCG10: 0.5, LatencyMS: 10.0},
		{Repo: "x", Query: "q2", NDCG10: 0.5, LatencyMS: 20.0},
	}
	candidate := []QueryResult{
		{Repo: "x", Query: "q1", NDCG10: 0.5, LatencyMS: 15.0},
		{Repo: "x", Query: "q2", NDCG10: 0.5, LatencyMS: 25.0},
	}
	delta := computeDelta(baseline, candidate)
	if delta == nil {
		t.Fatal("delta is nil")
	}
	if delta.LatencyMS == "" {
		t.Error("LatencyMS delta is empty, want non-empty")
	}
	if !strings.Contains(delta.LatencyMS, "ms") {
		t.Errorf("LatencyMS delta = %q, want to contain 'ms'", delta.LatencyMS)
	}
}
