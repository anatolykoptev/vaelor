// internal/investigate/prompt_test.go
package investigate

import (
	"fmt"
	"strings"
	"testing"
)

func TestBuildSystemPrompt_IncludesGroundTruth(t *testing.T) {
	ctx := PromptContext{
		Service:           "go-code",
		AvailableMetrics:  []string{"http_requests_total", "http_request_duration_seconds"},
		AvailableServices: []string{"go-code", "memdb-go"},
	}
	out := BuildSystemPrompt(ctx)

	for _, want := range []string{
		"go-code",
		"http_requests_total",
		"DO NOT invent metric names",
		"three-strike rule",
		"evidence",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestBuildSystemPrompt_TruncatesLongMetricList(t *testing.T) {
	metrics := make([]string, 200)
	for i := range metrics {
		metrics[i] = fmt.Sprintf("metric_%03d", i)
	}
	out := BuildSystemPrompt(PromptContext{Service: "x", AvailableMetrics: metrics})

	count := strings.Count(out, "  - metric_")
	if count != 80 {
		t.Errorf("expected exactly 80 metrics in prompt, got %d", count)
	}
}
