package goutil

import (
	"context"
	"fmt"
	"runtime"
	"sync"
)

// DAGNode is implemented by any item that knows its ID and dependencies.
type DAGNode interface {
	// NodeID returns the unique identifier for this node.
	NodeID() string
	// NodeDeps returns IDs of nodes that must complete before this one.
	NodeDeps() []string
}

// RunDAG executes nodes in dependency order using a worker pool.
// Nodes whose dependencies are all done become "ready" and are dispatched
// to workers immediately — this mirrors the findAllRunnable pattern from
// go-workflow/engine_dag.go.
//
// fn is called once per node; the first error aborts remaining work.
// workers <= 0 defaults to runtime.NumCPU().
func RunDAG[T DAGNode](ctx context.Context, nodes []T, workers int, fn func(T) error) error {
	if len(nodes) == 0 {
		return nil
	}
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	if err := detectDAGCycle(nodes); err != nil {
		return err
	}

	// Index nodes by ID.
	byID := make(map[string]T, len(nodes))
	for _, n := range nodes {
		byID[n.NodeID()] = n
	}

	var (
		mu      sync.Mutex
		cond    = sync.NewCond(&mu)
		done    = make(map[string]bool, len(nodes))
		pending = make(map[string]bool, len(nodes))
		runErr  error
		inFlight int // nodes dispatched but not yet complete
	)
	for _, n := range nodes {
		pending[n.NodeID()] = true
	}

	// findReady returns pending nodes whose deps are all done (called under mu).
	findReady := func() []T {
		var ready []T
		for id := range pending {
			n := byID[id]
			allDone := true
			for _, dep := range n.NodeDeps() {
				if !done[dep] {
					allDone = false
					break
				}
			}
			if allDone {
				ready = append(ready, n)
			}
		}
		return ready
	}

	work := make(chan T, len(nodes))

	// Dispatcher: wakes on cond, enqueues newly-ready nodes.
	// Closes work channel when all nodes are dispatched or an error occurs.
	go func() {
		mu.Lock()
		defer mu.Unlock()
		for {
			if runErr != nil || (len(pending) == 0 && inFlight == 0) {
				close(work)
				return
			}
			for _, n := range findReady() {
				delete(pending, n.NodeID())
				inFlight++
				work <- n
			}
			cond.Wait()
		}
	}()

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range work {
				if ctx.Err() != nil {
					mu.Lock()
					if runErr == nil {
						runErr = ctx.Err()
					}
					mu.Unlock()
					cond.Signal()
					return
				}
				err := fn(n)
				mu.Lock()
				inFlight--
				if err != nil {
					if runErr == nil {
						runErr = err
					}
				} else {
					done[n.NodeID()] = true
				}
				cond.Signal()
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if runErr != nil {
		return runErr
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

// cycleColor constants mirror dag.go in go-workflow.
const (
	dagColorWhite = 0
	dagColorGray  = 1
	dagColorBlack = 2
)

// detectDAGCycle returns an error if nodes contain a dependency cycle.
func detectDAGCycle[T DAGNode](nodes []T) error {
	deps := make(map[string][]string, len(nodes))
	for _, n := range nodes {
		deps[n.NodeID()] = n.NodeDeps()
	}

	color := make(map[string]int, len(nodes))
	var visit func(id string) string
	visit = func(id string) string {
		color[id] = dagColorGray
		for _, dep := range deps[id] {
			switch color[dep] {
			case dagColorGray:
				return fmt.Sprintf("%s → %s", id, dep)
			case dagColorWhite:
				if cycle := visit(dep); cycle != "" {
					return cycle
				}
			}
		}
		color[id] = dagColorBlack
		return ""
	}

	for _, n := range nodes {
		if color[n.NodeID()] == dagColorWhite {
			if cycle := visit(n.NodeID()); cycle != "" {
				return fmt.Errorf("dependency cycle: %s", cycle)
			}
		}
	}
	return nil
}
