package main

import (
	"testing"

	kitllm "github.com/anatolykoptev/go-kit/llm"
	kitmetrics "github.com/anatolykoptev/go-kit/metrics"
)

func TestModelFilterObserver_NoOp_WhenNothingDropped(t *testing.T) {
	reg := kitmetrics.NewRegistry()
	obs := newModelFilterObserver(reg)

	obs(kitllm.ModelFilterEvent{
		BaseURL:   "http://proxy",
		Requested: 3,
		Kept:      3,
		Available: 5,
	})

	if v := reg.Value("llm_models_dropped_total"); v != 0 {
		t.Errorf("llm_models_dropped_total = %d, want 0", v)
	}
	if v := reg.Value("llm_chain_degraded_total"); v != 0 {
		t.Errorf("llm_chain_degraded_total = %d, want 0", v)
	}
}

func TestModelFilterObserver_IncrementsDroppedCounter(t *testing.T) {
	reg := kitmetrics.NewRegistry()
	obs := newModelFilterObserver(reg)

	obs(kitllm.ModelFilterEvent{
		BaseURL:   "http://proxy",
		Requested: 3,
		Kept:      1,
		Dropped:   []string{"model-a", "model-b"},
		Available: 5,
	})

	if v := reg.Value(`llm_models_dropped_total{model=model-a}`); v != 1 {
		t.Errorf("llm_models_dropped_total{model=model-a} = %d, want 1", v)
	}
	if v := reg.Value(`llm_models_dropped_total{model=model-b}`); v != 1 {
		t.Errorf("llm_models_dropped_total{model=model-b} = %d, want 1", v)
	}
	// degraded counter must stay zero when Degraded=false.
	if v := reg.Value("llm_chain_degraded_total"); v != 0 {
		t.Errorf("llm_chain_degraded_total = %d, want 0", v)
	}
}

func TestModelFilterObserver_IncrementsDegradedCounter(t *testing.T) {
	reg := kitmetrics.NewRegistry()
	obs := newModelFilterObserver(reg)

	obs(kitllm.ModelFilterEvent{
		BaseURL:   "http://proxy",
		Requested: 3,
		Kept:      3,
		Available: 0,
		Degraded:  true,
		Reason:    "fetch_failed",
	})

	if v := reg.Value(`llm_chain_degraded_total{reason=fetch_failed}`); v != 1 {
		t.Errorf("llm_chain_degraded_total{reason=fetch_failed} = %d, want 1", v)
	}
	// dropped counter must stay zero when Degraded=true (no models were removed).
	if v := reg.Value("llm_models_dropped_total"); v != 0 {
		t.Errorf("llm_models_dropped_total = %d, want 0", v)
	}
}

func TestModelFilterObserver_DegradedDoesNotIncrDropped(t *testing.T) {
	reg := kitmetrics.NewRegistry()
	obs := newModelFilterObserver(reg)

	// Degraded=true with a non-empty Dropped should only bump degraded, not dropped.
	// (all_filtered reason: the kit may populate Dropped when all were removed, then
	// degrade because emptying the chain is unsafe.)
	obs(kitllm.ModelFilterEvent{
		BaseURL:   "http://proxy",
		Requested: 2,
		Kept:      2,
		Dropped:   []string{"model-x"},
		Available: 4,
		Degraded:  true,
		Reason:    "all_filtered",
	})

	if v := reg.Value(`llm_chain_degraded_total{reason=all_filtered}`); v != 1 {
		t.Errorf("llm_chain_degraded_total{reason=all_filtered} = %d, want 1", v)
	}
	// Observer exits early on Degraded=true; dropped loop never runs.
	if v := reg.Value(`llm_models_dropped_total{model=model-x}`); v != 0 {
		t.Errorf("llm_models_dropped_total{model=model-x} = %d, want 0", v)
	}
}

func TestModelFilterObserver_MultipleDropsAccumulate(t *testing.T) {
	reg := kitmetrics.NewRegistry()
	obs := newModelFilterObserver(reg)

	// Two events both drop the same model — counter accumulates.
	obs(kitllm.ModelFilterEvent{Dropped: []string{"model-z"}})
	obs(kitllm.ModelFilterEvent{Dropped: []string{"model-z"}})

	if v := reg.Value(`llm_models_dropped_total{model=model-z}`); v != 2 {
		t.Errorf("llm_models_dropped_total{model=model-z} = %d, want 2", v)
	}
}
