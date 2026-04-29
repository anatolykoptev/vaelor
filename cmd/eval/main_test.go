// Package main — eval harness tests.
//
// Coverage:
//   - matchExpected: lenient label matching (3 forms)
//   - NDCG10 / RecallAtK / MRR: fixed input → known output
//   - parseSemanticXML: real semantic_search XML envelope
//   - HTTP roundtrip via httptest.Server (mirrors REST bridge shape)
//   - JSON Report stability (round-trip parse)
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

// ──────────────────── matching ────────────────────

func TestMatchExpected_AllForms(t *testing.T) {
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
	hits := mkHits("a.go:X", "b.go:Y")
	got := NDCG10(hits, []string{"Foo", "Bar"})
	if got != 0 {
		t.Errorf("NDCG10 = %f, want 0", got)
	}
}

func TestNDCG10_EmptyExpected(t *testing.T) {
	got := NDCG10(mkHits("a.go:X"), nil)
	if got != 0 {
		t.Errorf("NDCG10 with no labels = %f, want 0", got)
	}
}

func TestRecallAtK(t *testing.T) {
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

// ──────────────────── JSON output stability ────────────────────

func TestReport_RoundTrip(t *testing.T) {
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
	dir := t.TempDir()
	bad := `{"query": "", "expected_top_3": ["X"]}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "x.jsonl"), []byte(bad), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadGolden(dir); err == nil {
		t.Error("expected error for empty query")
	}
}
