package main

import (
	"log/slog"

	kitllm "github.com/anatolykoptev/go-kit/llm"
	kitmetrics "github.com/anatolykoptev/go-kit/metrics"
)

// newModelFilterObserver returns a ModelFilterObserver that:
//   - increments gocode_llm_models_dropped_total per dropped model id
//     (label: model — the id removed from the chain)
//   - increments gocode_llm_chain_degraded_total{reason} when filtering was
//     skipped entirely (Degraded=true; reason is the machine-stable token from
//     the kit: no_registry / fetch_failed / empty_set / all_filtered)
//   - emits a slog.Warn for each dropped model so ops can cross-reference logs
//
// The observer must not block — it only increments counters and logs.
// It must not panic — callers are unrecovered per ModelFilterObserver contract.
func newModelFilterObserver(reg *kitmetrics.Registry) kitllm.ModelFilterObserver {
	return func(ev kitllm.ModelFilterEvent) {
		if ev.Degraded {
			// Filtering skipped — chain unvalidated.
			reg.Incr(kitmetrics.Label("llm_chain_degraded_total", "reason", ev.Reason))
			slog.Warn("llm model filter: chain degraded (filtering skipped)",
				slog.String("reason", ev.Reason),
				slog.String("base_url", ev.BaseURL),
				slog.Int("requested", ev.Requested),
				slog.Int("available", ev.Available),
			)
			return
		}

		for _, model := range ev.Dropped {
			// Counter per dropped model id so dashboards can show which model died.
			reg.Incr(kitmetrics.Label("llm_models_dropped_total", "model", model))
			slog.Warn("llm model filter: dead model dropped from chain",
				slog.String("model", model),
				slog.String("base_url", ev.BaseURL),
				slog.Int("requested", ev.Requested),
				slog.Int("kept", ev.Kept),
				slog.Int("available", ev.Available),
			)
		}
	}
}
