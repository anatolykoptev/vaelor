package codegraph

import (
	"sync"
	"testing"
	"time"
)

// TestGraphExistsCache_HitMiss verifies basic cache semantics.
func TestGraphExistsCache_HitMiss(t *testing.T) {
	c := newGraphExistsCache(30 * time.Second)

	t.Run("miss before mark", func(t *testing.T) {
		if c.Hit("code_abc") {
			t.Error("expected miss before Mark")
		}
	})

	t.Run("hit after mark", func(t *testing.T) {
		c.Mark("code_abc")
		if !c.Hit("code_abc") {
			t.Error("expected hit after Mark")
		}
	})

	t.Run("miss for different graph", func(t *testing.T) {
		if c.Hit("code_def") {
			t.Error("expected miss for unmarked graph")
		}
	})
}

// TestGraphExistsCache_TTLExpiry verifies that positive entries expire after TTL.
func TestGraphExistsCache_TTLExpiry(t *testing.T) {
	ttl := 40 * time.Millisecond
	c := newGraphExistsCache(ttl)

	c.Mark("code_ttl")
	if !c.Hit("code_ttl") {
		t.Fatal("expected hit immediately after Mark")
	}

	// Wait for TTL to expire.
	time.Sleep(ttl + 10*time.Millisecond)

	if c.Hit("code_ttl") {
		t.Error("expected miss after TTL expiry")
	}
}

// TestGraphExistsCache_Forget verifies that Forget removes a cached entry.
func TestGraphExistsCache_Forget(t *testing.T) {
	c := newGraphExistsCache(30 * time.Second)

	c.Mark("code_forget")
	if !c.Hit("code_forget") {
		t.Fatal("expected hit after Mark")
	}

	c.Forget("code_forget")
	if c.Hit("code_forget") {
		t.Error("expected miss after Forget")
	}
}

// TestGraphExistsCache_Concurrent verifies that concurrent Mark+Hit calls are
// race-free (run with go test -race).
func TestGraphExistsCache_Concurrent(t *testing.T) {
	c := newGraphExistsCache(30 * time.Second)
	const goroutines = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(i int) {
			defer wg.Done()
			name := "code_concurrent"
			c.Mark(name)
			_ = c.Hit(name)
			if i%10 == 0 {
				c.Forget(name)
			}
		}(i)
	}
	wg.Wait()
}
