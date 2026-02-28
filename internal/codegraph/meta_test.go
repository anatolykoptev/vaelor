package codegraph

import (
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
