package goutil

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
)

// testNode is a simple DAGNode for tests.
type testNode struct {
	id   string
	deps []string
}

func (n testNode) NodeID() string     { return n.id }
func (n testNode) NodeDeps() []string { return n.deps }

func nodes(specs ...string) []testNode {
	var out []testNode
	for _, s := range specs {
		out = append(out, testNode{id: s})
	}
	return out
}

// TestRunDAG_empty does nothing without error.
func TestRunDAG_empty(t *testing.T) {
	err := RunDAG(context.Background(), []testNode{}, 2, func(testNode) error { return nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRunDAG_linear ensures A→B→C run in order.
func TestRunDAG_linear(t *testing.T) {
	ns := []testNode{
		{id: "C", deps: []string{"B"}},
		{id: "A", deps: nil},
		{id: "B", deps: []string{"A"}},
	}

	var mu sync.Mutex
	var order []string

	err := RunDAG(context.Background(), ns, 1, func(n testNode) error {
		mu.Lock()
		order = append(order, n.id)
		mu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// A must come before B, B before C.
	idxA := slices.Index(order, "A")
	idxB := slices.Index(order, "B")
	idxC := slices.Index(order, "C")
	if idxA >= idxB || idxB >= idxC {
		t.Errorf("wrong order: %v", order)
	}
}

// TestRunDAG_parallel verifies independent nodes run (no ordering guarantee).
func TestRunDAG_parallel(t *testing.T) {
	ns := nodes("X", "Y", "Z") // no deps — all independent

	var mu sync.Mutex
	var seen []string

	err := RunDAG(context.Background(), ns, 4, func(n testNode) error {
		mu.Lock()
		seen = append(seen, n.id)
		mu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(seen) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(seen))
	}
}

// TestRunDAG_cycle returns an error instead of hanging.
func TestRunDAG_cycle(t *testing.T) {
	ns := []testNode{
		{id: "A", deps: []string{"B"}},
		{id: "B", deps: []string{"A"}},
	}
	err := RunDAG(context.Background(), ns, 2, func(testNode) error { return nil })
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if !errors.Is(err, err) { // always true, just sanity
		t.Fatal("not an error")
	}
	t.Log("cycle error:", err)
}

// TestRunDAG_error propagates fn error.
func TestRunDAG_error(t *testing.T) {
	ns := nodes("A", "B", "C")
	boom := fmt.Errorf("boom")

	err := RunDAG(context.Background(), ns, 2, func(n testNode) error {
		if n.id == "B" {
			return boom
		}
		return nil
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected boom, got: %v", err)
	}
}

// TestRunDAG_contextCancel stops execution on cancel.
func TestRunDAG_contextCancel(t *testing.T) {
	// Build a long chain A→B→C→...→Z (26 nodes).
	var ns []testNode
	for i := 0; i < 26; i++ {
		id := fmt.Sprintf("n%02d", i)
		var deps []string
		if i > 0 {
			deps = []string{fmt.Sprintf("n%02d", i-1)}
		}
		ns = append(ns, testNode{id: id, deps: deps})
	}

	ctx, cancel := context.WithCancel(context.Background())

	var mu sync.Mutex
	var executed []string

	err := RunDAG(ctx, ns, 1, func(n testNode) error {
		mu.Lock()
		executed = append(executed, n.id)
		if len(executed) == 3 {
			cancel()
		}
		mu.Unlock()
		return nil
	})

	if err == nil {
		t.Fatal("expected context.Canceled or nil with early stop")
	}

	mu.Lock()
	ex := len(executed)
	mu.Unlock()

	// Should have stopped well before all 26.
	if ex >= 26 {
		t.Errorf("expected early stop, got %d nodes executed", ex)
	}
}

// TestDetectDAGCycle directly tests cycle detection.
func TestDetectDAGCycle(t *testing.T) {
	t.Run("no cycle", func(t *testing.T) {
		ns := []testNode{
			{id: "A"},
			{id: "B", deps: []string{"A"}},
			{id: "C", deps: []string{"B"}},
		}
		if err := detectDAGCycle(ns); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	t.Run("direct cycle", func(t *testing.T) {
		ns := []testNode{
			{id: "A", deps: []string{"B"}},
			{id: "B", deps: []string{"A"}},
		}
		if err := detectDAGCycle(ns); err == nil {
			t.Error("expected cycle error")
		}
	})
	t.Run("self loop", func(t *testing.T) {
		ns := []testNode{{id: "A", deps: []string{"A"}}}
		if err := detectDAGCycle(ns); err == nil {
			t.Error("expected self-loop cycle error")
		}
	})
}
