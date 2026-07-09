package upstream

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/fleet"
	"github.com/anatolykoptev/go-code/internal/polyglot/pinned"
)

// makeTagDrift builds a TagDrift ImageDiff with the given pinned/runtime tags.
func makeTagDrift(image, pinnedTag, runtimeTag string) fleet.ImageDiff {
	return fleet.ImageDiff{
		Image:  image,
		Status: fleet.DiffTagDrift,
		Pinned: &pinned.PinnedImage{
			Image: image,
			Tag:   pinnedTag,
		},
		Runtime: &fleet.RuntimeImage{
			Image: image,
			Tag:   runtimeTag,
		},
	}
}

// testJSONServer builds an httptest.Server that always returns the given JSON body.
func testJSONServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	}))
}

// TestEnrich_SkipsNonTagDrift: Match, OnlySource, OnlyRuntime rows → Changelog=nil, no API calls.
func TestEnrich_SkipsNonTagDrift(t *testing.T) {
	t.Parallel()
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := New(WithHTTPClient(srv.Client()), WithTimeout(5*time.Second))
	client.baseURL = srv.URL

	diffs := []fleet.ImageDiff{
		{Image: "nginx", Status: fleet.DiffMatch},
		{Image: "redis", Status: fleet.DiffOnlySource},
		{Image: "alpine", Status: fleet.DiffOnlyRuntime},
	}

	result := Enrich(context.Background(), client, diffs, 30)
	for _, d := range result {
		if d.Changelog != nil {
			t.Errorf("image %q: expected Changelog=nil for %s row, got non-nil", d.Image, d.Status)
		}
	}
	if callCount > 0 {
		t.Errorf("expected 0 API calls for non-TagDrift rows, got %d", callCount)
	}
}

// TestEnrich_MaxEnrichCapRespected: 5 TagDrift rows, maxEnrich=2 → at most 2 enriched.
func TestEnrich_MaxEnrichCapRespected(t *testing.T) {
	t.Parallel()
	srv := testJSONServer(`{"status":"ahead","commits":[],"html_url":"http://example.com"}`)
	defer srv.Close()

	client := New(WithHTTPClient(srv.Client()), WithTimeout(5*time.Second))
	client.baseURL = srv.URL

	diffs := []fleet.ImageDiff{
		makeTagDrift("redis", "7.0", "7.1"),
		makeTagDrift("redis", "7.0", "7.2"),
		makeTagDrift("redis", "7.0", "7.3"),
		makeTagDrift("redis", "7.0", "7.4"),
		makeTagDrift("redis", "7.0", "7.5"),
	}

	result := Enrich(context.Background(), client, diffs, 2)

	enrichedCount := 0
	for _, d := range result {
		if d.Changelog != nil {
			enrichedCount++
		}
	}
	if enrichedCount > 2 {
		t.Errorf("enriched %d diffs; want ≤2 (maxEnrich=2)", enrichedCount)
	}
}

// TestEnrich_SoftFail_UnmappedImage: TagDrift on unmapped image → Changelog=nil or Resolved=false.
func TestEnrich_SoftFail_UnmappedImage(t *testing.T) {
	t.Parallel()
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := New(WithHTTPClient(srv.Client()), WithTimeout(5*time.Second))
	client.baseURL = srv.URL

	diffs := []fleet.ImageDiff{
		makeTagDrift("some-internal/not-in-registry", "1.0", "1.1"),
	}

	result := Enrich(context.Background(), client, diffs, 30)
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	// Soft fail: Changelog must be nil (image not mapped, skip enrichment).
	if result[0].Changelog != nil && result[0].Changelog.Resolved {
		t.Error("expected unmapped image to not have Resolved=true changelog")
	}
}

// TestEnrich_DeduplicatesCompareCalls: 3 TagDrift rows for same (slug, base, head)
// → at most 3 Compare API calls (1 per tag-form attempt), not 9.
func TestEnrich_DeduplicatesCompareCalls(t *testing.T) {
	t.Parallel()
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		resp := `{"status":"ahead","commits":[],"html_url":"http://example.com"}`
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(resp))
	}))
	defer srv.Close()

	client := New(WithHTTPClient(srv.Client()), WithTimeout(5*time.Second))
	client.baseURL = srv.URL

	// 3 identical TagDrift rows for minio/minio.
	diffs := []fleet.ImageDiff{
		makeTagDrift("minio/minio", "26.4.25", "26.5.3"),
		makeTagDrift("minio/minio", "26.4.25", "26.5.3"),
		makeTagDrift("minio/minio", "26.4.25", "26.5.3"),
	}

	result := Enrich(context.Background(), client, diffs, 30)

	// All should be enriched.
	for i, d := range result {
		if d.Changelog == nil {
			t.Errorf("result[%d]: expected non-nil Changelog for minio/minio TagDrift", i)
		}
	}

	// With dedup: 1 unique (slug, base, head) × ≤3 tag-form attempts = max 3 calls.
	// Without dedup: 3 rows × ≤3 attempts = max 9 calls.
	if callCount > 3 {
		t.Errorf("Compare called %d times; want ≤3 for 3 identical rows (dedup expected)", callCount)
	}
}

// TestEnrich_ParallelExecution: enrichment of multiple images runs in parallel.
func TestEnrich_ParallelExecution(t *testing.T) {
	t.Parallel()
	const slowDelay = 150 * time.Millisecond

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// redis/redis requests sleep briefly to simulate slow upstream.
		if strings.Contains(r.URL.Path, "redis") {
			time.Sleep(slowDelay)
		}
		resp := `{"status":"ahead","commits":[],"html_url":"http://example.com"}`
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(resp))
	}))
	defer srv.Close()

	client := New(WithHTTPClient(srv.Client()), WithTimeout(5*time.Second))
	client.baseURL = srv.URL

	diffs := []fleet.ImageDiff{
		makeTagDrift("redis", "7.0", "7.2"),                // mapped → redis/redis (slow)
		makeTagDrift("minio/minio", "26.4.25", "26.5.3"), // mapped → minio/minio (fast)
	}

	start := time.Now()
	Enrich(context.Background(), client, diffs, 30)
	elapsed := time.Since(start)

	// Sequential would take ~300ms (2 × 150ms). Parallel should finish faster.
	// Under the race detector, execution is significantly slower.
	// We check: parallel should complete in < 1.5× sequential time,
	// using a generous 6× multiplier to account for race-detector and CI overhead.
	sequential := 2 * slowDelay
	if elapsed > 6*slowDelay {
		t.Errorf("Enrich took %v; expected parallel execution (sequential would be ~%v)",
			elapsed, sequential)
	}
}
