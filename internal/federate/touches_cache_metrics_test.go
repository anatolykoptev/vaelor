package federate

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// federatedCoChangeCacheSizeValue reads the current value of the
// gocode_federated_cochange_cache_size gauge from the default registry.
func federatedCoChangeCacheSizeValue(t *testing.T) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != "gocode_federated_cochange_cache_size" {
			continue
		}
		for _, m := range mf.GetMetric() {
			_ = dto.Metric{}
			return m.GetGauge().GetValue()
		}
	}
	return 0
}

// TestTouchesCacheSizeGauge_MovesOnMutation confirms the
// gocode_federated_cochange_cache_size gauge tracks the live entry count of the
// process-global touchesCache: +1 on set, -1 on TTL-expiry eviction. Drives the
// REAL production cache (globalTouchesCache) via its production methods.
// Falsification: remove the onMutate call in set/get and the gauge stays flat.
func TestTouchesCacheSizeGauge_MovesOnMutation(t *testing.T) {
	// Not parallel: mutates the process-global touchesCache + gauge.
	key := "touches::gauge_test_unique_" + time.Now().Format("150405.000000")

	// Ensure the key is absent to start (deterministic baseline).
	globalTouchesCache.mu.Lock()
	globalTouchesCache.lru.Delete(key)
	globalTouchesCache.mu.Unlock()
	if globalTouchesCache.onMutate != nil {
		globalTouchesCache.mu.Lock()
		globalTouchesCache.onMutate(globalTouchesCache.lru.Len())
		globalTouchesCache.mu.Unlock()
	}
	before := federatedCoChangeCacheSizeValue(t)

	// set → gauge must increase by 1.
	globalTouchesCache.set(key, []RepoTouch{})
	afterSet := federatedCoChangeCacheSizeValue(t)
	if afterSet-before != 1 {
		t.Errorf("gauge delta after set = %v, want 1", afterSet-before)
	}

	// get within TTL → no eviction, gauge unchanged.
	globalTouchesCache.get(key)
	afterGet := federatedCoChangeCacheSizeValue(t)
	if afterGet != afterSet {
		t.Errorf("gauge changed on TTL-hit get: %v -> %v, want unchanged", afterSet, afterGet)
	}

	// Force TTL expiry: overwrite the entry with an expired timestamp, then get
	// → eviction → gauge must decrease by 1.
	globalTouchesCache.mu.Lock()
	globalTouchesCache.lru.Set(key, touchesCacheEntry{
		data: []RepoTouch{},
		at:   time.Now().Add(-touchesCacheTTL - time.Second),
	})
	if globalTouchesCache.onMutate != nil {
		globalTouchesCache.onMutate(globalTouchesCache.lru.Len())
	}
	globalTouchesCache.mu.Unlock()

	globalTouchesCache.get(key) // TTL-expired → delete + onMutate
	afterExpire := federatedCoChangeCacheSizeValue(t)
	if afterSet-afterExpire != 1 {
		t.Errorf("gauge delta after TTL-eviction = %v, want 1 (decrease)", afterSet-afterExpire)
	}
}
