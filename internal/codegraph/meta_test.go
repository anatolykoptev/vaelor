package codegraph

import (
	"math"
	"testing"
	"time"
)

// TestIsFresh verifies the freshness check against TTL.
func TestIsFresh(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		builtAt    time.Time
		ttlSeconds int
		want       bool
	}{
		{
			name:       "fresh: just built",
			builtAt:    time.Now().Add(-10 * time.Second),
			ttlSeconds: 3600,
			want:       true,
		},
		{
			name:       "stale: well past ttl",
			builtAt:    time.Now().Add(-2 * time.Hour),
			ttlSeconds: 3600,
			want:       false,
		},
		{
			name:       "boundary: exactly at ttl is stale",
			builtAt:    time.Now().Add(-time.Hour),
			ttlSeconds: 3600,
			want:       false,
		},
		{
			name:       "zero ttl: always stale",
			builtAt:    time.Now(),
			ttlSeconds: 0,
			want:       false,
		},
		{
			name:       "negative ttl: always stale",
			builtAt:    time.Now(),
			ttlSeconds: -1,
			want:       false,
		},
		{
			name:       "zero time: stale",
			builtAt:    time.Time{},
			ttlSeconds: 3600,
			want:       false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isFresh(tc.builtAt, tc.ttlSeconds)
			if got != tc.want {
				t.Errorf("isFresh(%v, %d) = %v; want %v", tc.builtAt, tc.ttlSeconds, got, tc.want)
			}
		})
	}
}

// TestIsFreshSubSecond checks that an entry 999ms old with a 1s TTL is still fresh.
func TestIsFreshSubSecond(t *testing.T) {
	t.Parallel()
	builtAt := time.Now().Add(-999 * time.Millisecond)
	if !isFresh(builtAt, 1) {
		t.Error("entry 999ms old with 1s TTL should be fresh")
	}
}

// TestIsFreshJustExpired checks that an entry 1001ms old with a 1s TTL is stale.
func TestIsFreshJustExpired(t *testing.T) {
	t.Parallel()
	builtAt := time.Now().Add(-1001 * time.Millisecond)
	if isFresh(builtAt, 1) {
		t.Error("entry 1001ms old with 1s TTL should be stale")
	}
}

// TestIsFreshFarFuture checks that a future builtAt (clock skew) is treated as fresh.
func TestIsFreshFarFuture(t *testing.T) {
	t.Parallel()
	// time.Since(future) is negative → less than any positive duration → fresh.
	builtAt := time.Now().Add(10 * time.Second)
	if !isFresh(builtAt, 3600) {
		t.Error("future builtAt (clock skew) should be treated as fresh")
	}
}

// TestIsFreshMaxTTL checks that a very large TTL (math.MaxInt32 seconds) is fresh.
func TestIsFreshMaxTTL(t *testing.T) {
	t.Parallel()
	builtAt := time.Now().Add(-time.Second)
	if !isFresh(builtAt, math.MaxInt32) {
		t.Error("entry 1s old with MaxInt32 TTL should be fresh")
	}
}

// TestIsFreshOverflowTTL checks that math.MaxInt64 TTL does not panic.
// On 64-bit systems time.Duration(math.MaxInt64)*time.Second overflows to negative,
// making the comparison always stale. The test just verifies no panic occurs and
// records the actual behavior.
func TestIsFreshOverflowTTL(t *testing.T) {
	t.Parallel()
	builtAt := time.Now().Add(-time.Second)
	// Must not panic — result depends on overflow behavior, we just record it.
	_ = isFresh(builtAt, math.MaxInt64)
}

// TestApplyConfigDefaults verifies that zero-value fields receive their defaults.
func TestApplyConfigDefaults(t *testing.T) {
	t.Parallel()
	got := applyConfigDefaults(IndexConfig{})
	if got.TTLLocal != defaultTTLLocal {
		t.Errorf("TTLLocal: got %d, want %d", got.TTLLocal, defaultTTLLocal)
	}
	if got.TTLRemote != defaultTTLRemote {
		t.Errorf("TTLRemote: got %d, want %d", got.TTLRemote, defaultTTLRemote)
	}
	if got.BatchSize != defaultBatchSize {
		t.Errorf("BatchSize: got %d, want %d", got.BatchSize, defaultBatchSize)
	}
}

// TestApplyConfigDefaultsNonZero verifies that explicit non-zero values are preserved.
func TestApplyConfigDefaultsNonZero(t *testing.T) {
	t.Parallel()
	cfg := IndexConfig{TTLLocal: 120, TTLRemote: 600, BatchSize: 10}
	got := applyConfigDefaults(cfg)
	if got.TTLLocal != 120 {
		t.Errorf("TTLLocal: got %d, want 120", got.TTLLocal)
	}
	if got.TTLRemote != 600 {
		t.Errorf("TTLRemote: got %d, want 600", got.TTLRemote)
	}
	if got.BatchSize != 10 {
		t.Errorf("BatchSize: got %d, want 10", got.BatchSize)
	}
}

// TestApplyConfigDefaultsNegative verifies that negative values are replaced with defaults.
func TestApplyConfigDefaultsNegative(t *testing.T) {
	t.Parallel()
	cfg := IndexConfig{TTLLocal: -1, TTLRemote: -100, BatchSize: -5}
	got := applyConfigDefaults(cfg)
	if got.TTLLocal != defaultTTLLocal {
		t.Errorf("TTLLocal: got %d, want %d", got.TTLLocal, defaultTTLLocal)
	}
	if got.TTLRemote != defaultTTLRemote {
		t.Errorf("TTLRemote: got %d, want %d", got.TTLRemote, defaultTTLRemote)
	}
	if got.BatchSize != defaultBatchSize {
		t.Errorf("BatchSize: got %d, want %d", got.BatchSize, defaultBatchSize)
	}
}
