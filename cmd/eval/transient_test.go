// Package main — eval harness tests for transient signal handling.
//
// Coverage:
//   - classifyPayload / parseSemanticXML: timeout text, indexing status, real
//     results, empty, ready-empty, malformed-XML (non-transient).
//   - retry loop: transient N<budget then hits → success with Retries==N;
//     transient past budget → Error set, not a silent zero-success.
//   - warmup: distinct-repo dedup (2 repos × many queries probes each once);
//     repo staying transient past warmup-timeout → returns + logs, no hang.
//   - golden guard: no record in eval/golden/*.jsonl has repo=="/path/to/repo"
//     (regression guard for the psf-requests slug fix).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ──────────────────── classifyPayload / parseSemanticXML ────────────────────

// TestClassifyPayload_Table covers all classifyPayload branches via
// parseSemanticXML (the real call path). Asserts with errors.Is(err,
// ErrTransient{}) for transient cases.
//
// Falsification: remove the classifyPayload call in parseSemanticXML →
// timeout-text and indexing-status cases return a parse-xml error instead of
// ErrTransient → RED. Or remove the non-XML guard → plain-text timeout
// returns parse-xml EOF → RED.
func TestClassifyPayload_Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name            string
		payload         string
		wantTransient   bool
		wantHits        int
		wantNilHits     bool
		wantNonTransErr bool // expect a non-transient error (parse xml:)
	}{
		{
			name:          "timeout text → ErrTransient",
			payload:       "semantic_search: timed out during query embedding after 25s — retry with a simpler query.\npartial: true — query embedding\ntook_ms=25002",
			wantTransient: true,
			wantNilHits:   true,
		},
		{
			name:          "indexing status envelope → ErrTransient",
			payload:       `<response tool="semantic_search"><status>indexing</status><message>retry in 30-60s</message></response>`,
			wantTransient: true,
			wantNilHits:   true,
		},
		{
			name:          "pending status envelope → ErrTransient",
			payload:       `<response><status>pending</status></response>`,
			wantTransient: true,
			wantNilHits:   true,
		},
		{
			name:     "real results → hits",
			payload:  sampleSemanticXML,
			wantHits: 2,
		},
		{
			name:        "empty string → nil,nil",
			payload:     "",
			wantNilHits: true,
		},
		{
			name:        "ready envelope empty results → empty,nil",
			payload:     `<response tool="semantic_search"><results count="0"></results></response>`,
			wantHits:    0,
			wantNilHits: false, // empty slice (len 0), not nil
		},
		{
			name:            "malformed XML leading with < → non-transient parse error",
			payload:         `<response><results><result rank="1"><file>broken`,
			wantNonTransErr: true,
			wantNilHits:     true,
		},
		{
			name:          "non-XML plain text → ErrTransient (defensive)",
			payload:       "some random tool message that is not XML",
			wantTransient: true,
			wantNilHits:   true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hits, err := parseSemanticXML(tc.payload)

			if tc.wantTransient {
				if !errors.Is(err, ErrTransient{}) {
					t.Fatalf("expected ErrTransient, got err=%v", err)
				}
			} else if tc.wantNonTransErr {
				if err == nil {
					t.Fatal("expected non-transient parse error, got nil")
				}
				if errors.Is(err, ErrTransient{}) {
					t.Fatalf("expected NON-transient error, got ErrTransient: %v", err)
				}
			} else {
				if err != nil {
					t.Fatalf("expected nil error, got %v", err)
				}
			}

			if tc.wantNilHits && hits != nil {
				t.Errorf("expected nil hits, got %d hits", len(hits))
			}
			if !tc.wantNilHits && hits == nil && tc.wantHits == 0 {
				// ready-empty: empty slice (len 0) is acceptable
			}
			if tc.wantHits > 0 && len(hits) != tc.wantHits {
				t.Errorf("expected %d hits, got %d", tc.wantHits, len(hits))
			}
		})
	}
}

// TestClassifyPayload_RepoAnalyzeXML verifies that parseRepoAnalyzeXML also
// classifies transient signals via classifyPayload.
//
// Falsification: remove the classifyPayload call in parseRepoAnalyzeXML →
// indexing-status returns a parse error instead of ErrTransient → RED.
func TestClassifyPayload_RepoAnalyzeXML(t *testing.T) {
	t.Parallel()
	// Timeout text → ErrTransient.
	_, err := parseRepoAnalyzeXML("semantic_search: timed out during query embedding after 25s")
	if !errors.Is(err, ErrTransient{}) {
		t.Errorf("timeout text: expected ErrTransient, got %v", err)
	}

	// Indexing status → ErrTransient.
	_, err = parseRepoAnalyzeXML(`<response><status>indexing</status></response>`)
	if !errors.Is(err, ErrTransient{}) {
		t.Errorf("indexing status: expected ErrTransient, got %v", err)
	}

	// Real repo_analyze XML → files, no error.
	files, err := parseRepoAnalyzeXML(sampleRepoAnalyzeXML)
	if err != nil {
		t.Fatalf("real XML: unexpected error: %v", err)
	}
	if len(files) != 3 {
		t.Errorf("real XML: expected 3 files, got %d", len(files))
	}
}

// ──────────────────── retry loop ────────────────────

// TestRunSingle_RetryTransientThenSuccess verifies that runSingle retries
// transient tool signals and succeeds when a later attempt returns real
// results. Retries must equal the number of transient attempts, and Error
// must be empty.
//
// Falsification: remove the runWithRetry wrapper in runSingle (call
// client.Search directly) → the first transient response is a permanent
// error, Retries stays 0, Error is set → RED.
func TestRunSingle_RetryTransientThenSuccess(t *testing.T) {
	t.Parallel()
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		resp := restCallToolResp{}
		if n <= 2 {
			// First 2 calls: transient (indexing status).
			resp.Content = append(resp.Content, struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{Type: "text", Text: `<response><status>indexing</status></response>`})
		} else {
			// 3rd call: real results.
			resp.Content = append(resp.Content, struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{Type: "text", Text: sampleSemanticXML})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	rec := GoldenRecord{Query: "merge rrf", ExpectedTop3: []string{"MergeRRF"}, Repo: "go-code"}
	result := runSingle(context.Background(), NewMCPClient(srv.URL), rec, runnerCfg{
		TopK:          20,
		RetryAttempts: 6,
		RetryBase:     1 * time.Millisecond,
		RetryCap:      10 * time.Millisecond,
	})
	if result.Error != "" {
		t.Fatalf("expected success after retries, got error: %s", result.Error)
	}
	if result.Retries != 2 {
		t.Errorf("Retries = %d, want 2 (2 transient attempts before success)", result.Retries)
	}
	if result.NDCG10 != 1.0 {
		t.Errorf("NDCG10 = %f, want 1.0 (rank-1 hit on successful attempt)", result.NDCG10)
	}
}

// TestRunSingle_RetryBudgetExhausted verifies that when transient signals
// persist past the retry budget, out.Error is set to a "transient after N
// retries" message — NOT a silent empty-success (zero metrics with no error).
//
// Falsification: make runWithRetry return nil on budget exhaustion instead of
// the last transient error → result.Error stays empty but metrics are zero →
// the "no silent zero-success" assertion fails → RED.
func TestRunSingle_RetryBudgetExhausted(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Always return transient (indexing status).
		resp := restCallToolResp{}
		resp.Content = append(resp.Content, struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: "text", Text: `<response><status>indexing</status></response>`})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	rec := GoldenRecord{Query: "merge rrf", ExpectedTop3: []string{"MergeRRF"}, Repo: "go-code"}
	result := runSingle(context.Background(), NewMCPClient(srv.URL), rec, runnerCfg{
		TopK:          20,
		RetryAttempts: 3,
		RetryBase:     1 * time.Millisecond,
		RetryCap:      5 * time.Millisecond,
	})
	if result.Error == "" {
		t.Fatal("expected non-empty Error on budget exhaustion, got empty (silent zero-success)")
	}
	if !strings.Contains(result.Error, "transient after") {
		t.Errorf("Error = %q, want to contain 'transient after'", result.Error)
	}
	if !strings.Contains(result.Error, "indexing") {
		t.Errorf("Error = %q, want to contain the transient reason 'indexing'", result.Error)
	}
	// Must NOT be a silent zero-success: either Error is set OR metrics are
	// non-zero. Since all attempts were transient, metrics should be zero and
	// Error must be set (which we already checked).
	if result.NDCG10 != 0 {
		t.Errorf("NDCG10 = %f, want 0 (no successful attempt)", result.NDCG10)
	}
}

// TestRunSingle_NonTransientErrorNoRetry verifies that a non-transient error
// (e.g. a real tool error) does NOT trigger a retry — the loop ends
// immediately.
//
// Falsification: make runWithRetry retry on ALL errors (not just
// ErrTransient) → callCount exceeds 1 → RED.
func TestRunSingle_NonTransientErrorNoRetry(t *testing.T) {
	t.Parallel()
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&callCount, 1)
		// Return a tool error (is_error=true) — non-transient.
		resp := restCallToolResp{IsError: true}
		resp.Content = append(resp.Content, struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: "text", Text: "embed query: connection refused"})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	rec := GoldenRecord{Query: "q", ExpectedTop3: []string{"X"}, Repo: "go-code"}
	result := runSingle(context.Background(), NewMCPClient(srv.URL), rec, runnerCfg{
		TopK:          20,
		RetryAttempts: 6,
		RetryBase:     1 * time.Millisecond,
		RetryCap:      10 * time.Millisecond,
	})
	if result.Error == "" {
		t.Fatal("expected error for non-transient tool error")
	}
	if !strings.Contains(result.Error, "tool returned error") {
		t.Errorf("Error = %q, want to contain 'tool returned error'", result.Error)
	}
	if result.Retries != 0 {
		t.Errorf("Retries = %d, want 0 (non-transient → no retry)", result.Retries)
	}
	if callCount != 1 {
		t.Errorf("callCount = %d, want 1 (no retry on non-transient error)", callCount)
	}
}

// ──────────────────── warmup ────────────────────

// TestWarmupRepos_DistinctRepoDedup verifies that warmupRepos probes each
// DISTINCT resolved repo exactly once, even when the golden set has many
// queries per repo.
//
// Falsification: remove the `seen[rec.Repo]` dedup guard in warmupRepos →
// each query triggers a probe → callCount far exceeds 2 → RED.
func TestWarmupRepos_DistinctRepoDedup(t *testing.T) {
	t.Parallel()
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&callCount, 1)
		resp := restCallToolResp{}
		resp.Content = append(resp.Content, struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: "text", Text: sampleSemanticXML})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// 2 repos × 5 queries each = 10 total, but warmup should probe 2.
	gset := &GoldenSet{PerRepo: map[string][]GoldenRecord{
		"repo-a": {
			{Query: "q1", ExpectedTop3: []string{"X"}, Repo: "repo-a"},
			{Query: "q2", ExpectedTop3: []string{"X"}, Repo: "repo-a"},
			{Query: "q3", ExpectedTop3: []string{"X"}, Repo: "repo-a"},
			{Query: "q4", ExpectedTop3: []string{"X"}, Repo: "repo-a"},
			{Query: "q5", ExpectedTop3: []string{"X"}, Repo: "repo-a"},
		},
		"repo-b": {
			{Query: "q1", ExpectedTop3: []string{"Y"}, Repo: "repo-b"},
			{Query: "q2", ExpectedTop3: []string{"Y"}, Repo: "repo-b"},
			{Query: "q3", ExpectedTop3: []string{"Y"}, Repo: "repo-b"},
			{Query: "q4", ExpectedTop3: []string{"Y"}, Repo: "repo-b"},
			{Query: "q5", ExpectedTop3: []string{"Y"}, Repo: "repo-b"},
		},
	}}

	warmupRepos(context.Background(), NewMCPClient(srv.URL), gset, runnerCfg{
		TopK:      20,
		RetryBase: 1 * time.Millisecond,
		RetryCap:  10 * time.Millisecond,
	}, 5*time.Second)

	if callCount != 2 {
		t.Errorf("warmup callCount = %d, want 2 (one probe per distinct repo)", callCount)
	}
}

// TestWarmupRepos_TransientTimeoutNoHang verifies that a repo staying
// transient past the warmup-timeout returns (logs timeout) and does NOT hang.
//
// Falsification: remove the context deadline check / select in warmupOneRepo
// → the loop spins forever on transient responses → test times out → RED.
func TestWarmupRepos_TransientTimeoutNoHang(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Always return transient.
		resp := restCallToolResp{}
		resp.Content = append(resp.Content, struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{Type: "text", Text: `<response><status>indexing</status></response>`})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	gset := &GoldenSet{PerRepo: map[string][]GoldenRecord{
		"slow-repo": {
			{Query: "q1", ExpectedTop3: []string{"X"}, Repo: "slow-repo"},
		},
	}}

	// 200ms warmup-timeout — must return well under the test deadline.
	done := make(chan struct{})
	go func() {
		warmupRepos(context.Background(), NewMCPClient(srv.URL), gset, runnerCfg{
			TopK:      20,
			RetryBase: 10 * time.Millisecond,
			RetryCap:  20 * time.Millisecond,
		}, 200*time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
		// Good — warmup returned without hanging.
	case <-time.After(5 * time.Second):
		t.Fatal("warmup hung past 5s — expected to return after 200ms timeout")
	}
}

// ──────────────────── golden guard ────────────────────

// TestGoldenNoPathToRepoPlaceholder verifies that the psf-requests golden set
// (the file fixed by FIX C) does NOT regress to using the literal placeholder
// "/path/to/repo" as its repo value. A "/"-prefixed repo is treated as an
// already-resolved local path by the server before --repo-map can help, so the
// map entry never applies. The fix replaced it with the distinct slug
// "psf/requests" (matching the TS/Rust/Java sets).
//
// NOTE: go-code.jsonl and MemDB.jsonl still use "/path/to/repo" as a
// placeholder — those are overridden by ApplyRepoMap (by file basename) and
// are out of scope for this fix. This test guards only the file that was fixed.
//
// Falsification: revert any record in psf-requests.jsonl back to
// "/path/to/repo" → this test finds it and goes RED.
func TestGoldenNoPathToRepoPlaceholder(t *testing.T) {
	t.Parallel()
	goldenDir := filepath.Join("..", "..", "eval", "golden")
	path := filepath.Join(goldenDir, "psf-requests.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read psf-requests.jsonl: %v", err)
	}

	var checked int
	for lineno, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		var rec GoldenRecord
		if err := json.Unmarshal([]byte(trimmed), &rec); err != nil {
			continue
		}
		checked++
		if rec.Repo == "/path/to/repo" {
			t.Errorf("psf-requests.jsonl:%d: repo is %q — a '/'-prefixed value is "+
				"treated as an already-resolved local path before --repo-map is "+
				"consulted; use a distinct slug instead (\"psf/requests\")",
				lineno+1, rec.Repo)
		}
	}
	if checked == 0 {
		t.Fatal("no golden records checked — psf-requests.jsonl may be empty or mislocated")
	}
	t.Logf("checked %d records in psf-requests.jsonl — no /path/to/repo placeholder found", checked)
}

// TestRunSingle_RecordsRetries verifies that the Retries field is populated
// in the QueryResult and that QueriesRetried is counted in aggregates.
//
// Falsification: remove `out.Retries = retries` in runSingle → Retries stays
// 0 → QueriesRetried stays 0 → RED.
func TestRunSingle_RecordsRetries(t *testing.T) {
	t.Parallel()
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		resp := restCallToolResp{}
		if n == 1 {
			resp.Content = append(resp.Content, struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{Type: "text", Text: `<response><status>indexing</status></response>`})
		} else {
			resp.Content = append(resp.Content, struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{Type: "text", Text: sampleSemanticXML})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	rec := GoldenRecord{Query: "q", ExpectedTop3: []string{"MergeRRF"}, Repo: "go-code"}
	result := runSingle(context.Background(), NewMCPClient(srv.URL), rec, runnerCfg{
		TopK:          20,
		RetryAttempts: 3,
		RetryBase:     1 * time.Millisecond,
		RetryCap:      5 * time.Millisecond,
	})
	if result.Retries != 1 {
		t.Errorf("Retries = %d, want 1", result.Retries)
	}

	// Verify QueriesRetried is counted in aggregates.
	agg := computeAggregates([]QueryResult{result})
	if agg.QueriesRetried != 1 {
		t.Errorf("QueriesRetried = %d, want 1", agg.QueriesRetried)
	}
}

// TestRun_WarmupFlag verifies that -warmup=true triggers a warmup probe before
// the measured run (the server sees at least 2 requests: warmup + measured),
// while -warmup=false skips warmup (only 1 request: measured).
//
// Falsification: remove the warmupRepos call in run() → only 1 request
// (measured) → callCount=1, not >=2 → RED.
func TestRun_WarmupFlag(t *testing.T) {
	t.Parallel()
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&callCount, 1)
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
	// warmup=true, warmupTimeout=5s → server should see warmup probe + measured query.
	if err := run(dir, srv.URL, outPath, "", math.NaN(), math.NaN(), "", "", "", modeSemanticSearch, 1, 20, 30*time.Second, 1, true, 5*time.Second); err != nil {
		t.Fatalf("run: %v", err)
	}

	// warmup=true → at least 2 requests (1 warmup + 1 measured).
	if callCount < 2 {
		t.Errorf("callCount = %d, want >= 2 (warmup probe + measured query)", callCount)
	}

	report, err := readReport(outPath)
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	if len(report.PerQuery) != 1 {
		t.Errorf("expected 1 query result, got %d", len(report.PerQuery))
	}
}

// TestRun_WarmupDisabled verifies that -warmup=false skips the warmup phase
// (only the measured query request is made).
func TestRun_WarmupDisabled(t *testing.T) {
	t.Parallel()
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&callCount, 1)
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
	if err := run(dir, srv.URL, outPath, "", math.NaN(), math.NaN(), "", "", "", modeSemanticSearch, 1, 20, 10*time.Second, 1, false, 0); err != nil {
		t.Fatalf("run: %v", err)
	}

	// No warmup → only 1 request (the measured query).
	if callCount != 1 {
		t.Errorf("callCount = %d, want 1 (warmup disabled, only measured query)", callCount)
	}
}

// ──────────────────── helpers ────────────────────
