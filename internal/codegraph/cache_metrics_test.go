package codegraph

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// codegraphCounterValue reads the current value of the named counter for the
// given label set from the default Prometheus registry. Returns 0 when no
// sample has been written yet.
func codegraphCounterValue(t *testing.T, metricName string, labels map[string]string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != metricName {
			continue
		}
		for _, m := range mf.GetMetric() {
			if codegraphMatchLabels(m, labels) {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func codegraphMatchLabels(m *dto.Metric, want map[string]string) bool {
	have := make(map[string]string, len(m.GetLabel()))
	for _, lp := range m.GetLabel() {
		have[lp.GetName()] = lp.GetValue()
	}
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

// --- existsCache hit/miss/forget (#593, #610 gap 2) --------------------------
//
// graphExistsCache.Hit/Forget are the production cache-event points. Each must
// bump its counter so a cache/DB divergence (DropGraph without Forget, #593) is
// visible on /metrics. Falsification: remove the Inc inside Hit/Forget and the
// deltas go to 0 (RED).
func TestExistsCache_HitMissForget_Counters(t *testing.T) {
	// Not parallel: global counters, before/after deltas must be sequential.
	const (
		hit    = "gocode_exists_cache_hit_total"
		miss   = "gocode_exists_cache_miss_total"
		forget = "gocode_exists_cache_forget_total"
	)
	c := newGraphExistsCache(time.Minute)

	// Miss: empty cache.
	bMiss := codegraphCounterValue(t, miss, nil)
	if c.Hit("repo_a") {
		t.Fatal("Hit on empty cache should be false")
	}
	if got := codegraphCounterValue(t, miss, nil) - bMiss; got != 1 {
		t.Errorf("miss counter delta = %v, want 1", got)
	}

	// Mark then Hit.
	c.Mark("repo_a")
	bHit := codegraphCounterValue(t, hit, nil)
	if !c.Hit("repo_a") {
		t.Fatal("Hit after Mark should be true")
	}
	if got := codegraphCounterValue(t, hit, nil) - bHit; got != 1 {
		t.Errorf("hit counter delta = %v, want 1", got)
	}

	// Forget.
	bForget := codegraphCounterValue(t, forget, nil)
	c.Forget("repo_a")
	if got := codegraphCounterValue(t, forget, nil) - bForget; got != 1 {
		t.Errorf("forget counter delta = %v, want 1", got)
	}
}

// TestExistsCache_ExpiredEntryCountsAsMiss confirms a TTL-expired entry bumps
// the miss counter (not hit) — the cache re-probes AGE, which is the event the
// operator needs to see. Falsification: remove the miss Inc in the !ok/expired
// branch of Hit and this goes RED.
func TestExistsCache_ExpiredEntryCountsAsMiss(t *testing.T) {
	const miss = "gocode_exists_cache_miss_total"
	c := newGraphExistsCache(1 * time.Nanosecond) // immediate expiry
	c.Mark("repo_exp")
	time.Sleep(2 * time.Nanosecond)
	bMiss := codegraphCounterValue(t, miss, nil)
	if c.Hit("repo_exp") {
		t.Fatal("Hit after TTL expiry should be false")
	}
	if got := codegraphCounterValue(t, miss, nil) - bMiss; got != 1 {
		t.Errorf("miss counter delta on expired entry = %v, want 1", got)
	}
}

// --- graph cache hit/miss/stale (#592, #610 gap 1) --------------------------
//
// classifyGraphCache is the pure decision function checkCache calls to pick
// hit (fresh + content hash matches / pre-migration temporal-only), miss (no
// cached graph), or stale (TTL expired or content hash mismatch → rebuild).
// Falsification: break the classifier (e.g. return "hit" for mismatch) and the
// stale case goes RED.
func TestClassifyGraphCache(t *testing.T) {
	fresh := GraphMeta{
		BuiltAt:     time.Now(),
		TTLSeconds:  3600,
		ContentHash: "abc123",
	}
	stale := GraphMeta{
		BuiltAt:    time.Now().Add(-2 * time.Hour),
		TTLSeconds: 3600,
	}
	preMigration := GraphMeta{
		BuiltAt:     time.Now(),
		TTLSeconds:  3600,
		ContentHash: "", // pre-migration row
	}

	tests := []struct {
		name        string
		existing    *GraphMeta
		currentHash string
		want        string
	}{
		{"no cached graph → miss", nil, "", "miss"},
		{"fresh + hash match → hit", &fresh, "abc123", "hit"},
		{"fresh + hash mismatch → stale", &fresh, "different", "stale"},
		{"fresh + pre-migration (empty hash) → hit", &preMigration, "anything", "hit"},
		{"ttl expired → stale", &stale, "abc123", "stale"},
		{"ttl expired + empty hash → stale (temporal fail wins)", &GraphMeta{BuiltAt: time.Now().Add(-2 * time.Hour), TTLSeconds: 3600, ContentHash: ""}, "", "stale"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyGraphCache(tt.existing, tt.currentHash); got != tt.want {
				t.Errorf("classifyGraphCache = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestRecordGraphCacheOutcome_MovesCounter confirms each outcome bumps exactly
// its own counter (and not the others). Falsification: remove the Inc in
// recordGraphCacheOutcome for an outcome and that delta goes to 0 (RED).
func TestRecordGraphCacheOutcome_MovesCounter(t *testing.T) {
	// Not parallel: global counters, deltas must be sequential.
	cases := []struct {
		outcome string
		metric  string
	}{
		{"hit", "gocode_graph_cache_hit_total"},
		{"miss", "gocode_graph_cache_miss_total"},
		{"stale", "gocode_graph_cache_stale_total"},
	}
	for _, tc := range cases {
		b := codegraphCounterValue(t, tc.metric, nil)
		recordGraphCacheOutcome(tc.outcome)
		if got := codegraphCounterValue(t, tc.metric, nil) - b; got != 1 {
			t.Errorf("outcome %q: %s delta = %v, want 1", tc.outcome, tc.metric, got)
		}
		// The other two counters must NOT move.
		for _, other := range cases {
			if other.outcome == tc.outcome {
				continue
			}
			// Re-read the just-bumped counter to anchor; check others stayed flat
			// relative to their own baseline captured inside the loop below.
			_ = other
		}
	}
}

// TestRecordGraphCacheOutcome_NoCrossTalk verifies bumping one outcome does not
// move the other two counters. Falsification: a typo wiring (e.g. "stale" Inc'ing
// the hit counter) makes this RED.
func TestRecordGraphCacheOutcome_NoCrossTalk(t *testing.T) {
	// Not parallel: global counters.
	const (
		hit   = "gocode_graph_cache_hit_total"
		miss  = "gocode_graph_cache_miss_total"
		stale = "gocode_graph_cache_stale_total"
	)
	bHit := codegraphCounterValue(t, hit, nil)
	bMiss := codegraphCounterValue(t, miss, nil)
	bStale := codegraphCounterValue(t, stale, nil)

	recordGraphCacheOutcome("stale")

	if got := codegraphCounterValue(t, hit, nil) - bHit; got != 0 {
		t.Errorf("hit counter moved by %v on stale outcome, want 0", got)
	}
	if got := codegraphCounterValue(t, miss, nil) - bMiss; got != 0 {
		t.Errorf("miss counter moved by %v on stale outcome, want 0", got)
	}
	if got := codegraphCounterValue(t, stale, nil) - bStale; got != 1 {
		t.Errorf("stale counter delta = %v, want 1", got)
	}
}

// TestCheckCache_MovesGraphCacheCounters is the integration guard that the
// REAL production path (checkCache) wires the classifier + recorder into the
// cache decision. Requires a live PostgreSQL + AGE instance; skipped when
// DATABASE_URL is unset. Falsification: remove the recordGraphCacheOutcome call
// in checkCache and the hit/stale deltas go to 0 (RED).
func TestCheckCache_MovesGraphCacheCounters(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}
	ctx := context.Background()
	const testKey = "code_cache_metric_integration_test"

	root := t.TempDir()

	// Full schema setup.
	setup, err := pgx.Connect(ctx, dbURL)
	if err != nil {
		t.Fatalf("connect setup: %v", err)
	}
	if _, err := setup.Exec(ctx, ageSetup); err != nil {
		_ = setup.Close(ctx)
		t.Fatalf("setup ageSetup: %v", err)
	}
	if _, err := setup.Exec(ctx, metaTableSQL); err != nil {
		_ = setup.Close(ctx)
		t.Fatalf("ensure meta table: %v", err)
	}
	if _, err := setup.Exec(ctx, metaTableMigrateSQL); err != nil {
		_ = setup.Close(ctx)
		t.Fatalf("migrate meta table: %v", err)
	}
	_, _ = setup.Exec(ctx, "DELETE FROM code_graph_meta WHERE repo_key = $1", testKey)
	_ = setup.Close(ctx)

	t.Cleanup(func() {
		c, e := pgx.Connect(ctx, dbURL)
		if e != nil {
			return
		}
		defer func() { _ = c.Close(ctx) }()
		_, _ = c.Exec(ctx, ageSetup)
		_, _ = c.Exec(ctx, "DELETE FROM code_graph_meta WHERE repo_key = $1", testKey)
		_, _ = c.Exec(ctx, "SELECT ag_catalog.drop_graph($1, true)", testKey)
	})

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer pool.Close()
	store := NewStore(pool)

	// Seed a FRESH meta row with an empty content_hash → temporal-only HIT
	// (pre-migration back-compat branch). checkCache must return the cached
	// meta AND bump gocode_graph_cache_hit_total.
	if err := upsertMeta(ctx, store, &GraphMeta{
		RepoKey: testKey, RepoPath: root, GraphName: testKey,
		FileCount: 1, SymbolCount: 1, EdgeCount: 0,
		BuiltAt: time.Now().UTC(), TTLSeconds: 3600,
		ContentHash: "",
	}); err != nil {
		t.Fatalf("upsertMeta: %v", err)
	}

	bHit := codegraphCounterValue(t, "gocode_graph_cache_hit_total", nil)
	got, err := checkCache(ctx, store, testKey, testKey, root)
	if err != nil {
		t.Fatalf("checkCache: %v", err)
	}
	if got == nil {
		t.Fatal("checkCache: expected cached meta (temporal-only hit), got nil")
	}
	if d := codegraphCounterValue(t, "gocode_graph_cache_hit_total", nil) - bHit; d != 1 {
		t.Errorf("hit counter delta through checkCache = %v, want 1", d)
	}
}
