package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/embeddings"
)

type wiringCtxKey struct{}

// recorder captures what startAutoIndex launches, in order.
type recorder struct {
	mu       sync.Mutex
	events   []string
	loopCtx  context.Context
	interval time.Duration
	opts     embeddings.AutoIndexOpts
}

func (r *recorder) runners() autoIndexRunners {
	return autoIndexRunners{
		boot: func(_ *embeddings.Pipeline, _ []string, _ embeddings.RepoKeyFunc, _ embeddings.AutoIndexOpts) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.events = append(r.events, "boot")
		},
		loop: func(ctx context.Context, _ *embeddings.Pipeline, _ []string, _ embeddings.RepoKeyFunc, o embeddings.AutoIndexOpts, interval time.Duration) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.events = append(r.events, "loop")
			r.loopCtx, r.interval, r.opts = ctx, interval, o
		},
	}
}

func wiredConfig(t *testing.T) Config {
	t.Helper()
	t.Setenv("AUTOINDEX_INTERVAL", "")
	t.Setenv("AUTOINDEX_PASS_BUDGET", "")
	t.Setenv("AUTOINDEX_MAX_FILES", "")
	cfg, err := loadConfig() // real config, real defaults
	if err != nil {
		t.Fatal(err)
	}
	cfg.AutoIndexDirs = []string{"/host/src"}
	return cfg
}

func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("startAutoIndex goroutine did not finish")
	}
}

// With the shipped config the periodic loop starts, after the boot pass, with
// the configured interval, the run context and the pass budget / file cap.
// Deleting the loop launch leaves every other test green -- this one is the
// only thing standing between the service and boot-only indexing (the bug).
func TestStartAutoIndex_StartsLoopAfterBoot(t *testing.T) {
	cfg := wiredConfig(t)
	rec := &recorder{}
	ctx := context.WithValue(context.Background(), wiringCtxKey{}, "run")

	waitDone(t, startAutoIndex(ctx, cfg, &embeddings.Pipeline{}, rec.runners()))

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.events) != 2 || rec.events[0] != "boot" || rec.events[1] != "loop" {
		t.Fatalf("launch order = %v; want [boot loop]", rec.events)
	}
	if rec.interval != 15*time.Minute {
		t.Errorf("loop interval = %v; want the configured 15m", rec.interval)
	}
	if rec.loopCtx == nil || rec.loopCtx.Value(wiringCtxKey{}) != "run" {
		t.Error("loop did not receive the run context (it would not stop on shutdown)")
	}
	if rec.opts.PassBudget != 10*time.Minute || rec.opts.MaxFilesPerRepo != 100 {
		t.Errorf("loop opts = budget %v, max files %d; want 10m / 100", rec.opts.PassBudget, rec.opts.MaxFilesPerRepo)
	}
}

// Nothing is launched without a pipeline or without directories to index.
func TestStartAutoIndex_NothingWithoutPipelineOrDirs(t *testing.T) {
	cfg := wiredConfig(t)
	rec := &recorder{}
	waitDone(t, startAutoIndex(context.Background(), cfg, nil, rec.runners()))
	cfg.AutoIndexDirs = nil
	waitDone(t, startAutoIndex(context.Background(), cfg, &embeddings.Pipeline{}, rec.runners()))
	if len(rec.events) != 0 {
		t.Errorf("launched %v without a pipeline / dirs", rec.events)
	}
}
