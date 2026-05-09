// internal/investigate/prompt.go
package investigate

import (
	"fmt"
	"strings"
)

// PromptContext is the ground-truth payload injected into the LLM system prompt.
// Pattern source: Zagalin Grafana plugin — list real metric names and trace
// services so the LLM cannot hallucinate names.
type PromptContext struct {
	Service           string
	AvailableMetrics  []string // truncated to first 80 if longer
	AvailableServices []string
	OperationsSeen    []string // top operations from traces
	FiringAlerts      []string // alert names currently firing for this service
}

const maxMetricsInPrompt = 80
const maxOpsInPrompt = 30
const maxAlertsInPrompt = 20

// BuildSystemPrompt assembles the LLM correlation prompt with hard constraints
// against hallucination. Layout: role + ground truth + reasoning rules + output schema.
func BuildSystemPrompt(c PromptContext) string {
	var b strings.Builder

	b.WriteString("You are a debug-investigation assistant for the go-code MCP server.\n")
	b.WriteString("Goal: given Prometheus metrics + Jaeger traces + code-symbol findings, identify the most likely buggy file:function and rank hypotheses by evidence strength.\n\n")

	fmt.Fprintf(&b, "Service under investigation: %s\n\n", c.Service)

	if len(c.AvailableMetrics) > 0 {
		metrics := c.AvailableMetrics
		if len(metrics) > maxMetricsInPrompt {
			metrics = metrics[:maxMetricsInPrompt]
		}
		b.WriteString("Available Prometheus metric names (DO NOT invent metric names not in this list):\n")
		for _, m := range metrics {
			b.WriteString("  - ")
			b.WriteString(m)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if len(c.AvailableServices) > 0 {
		b.WriteString("Jaeger services seen (DO NOT invent service names):\n")
		for _, s := range c.AvailableServices {
			b.WriteString("  - ")
			b.WriteString(s)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if len(c.OperationsSeen) > 0 {
		ops := c.OperationsSeen
		if len(ops) > maxOpsInPrompt {
			ops = ops[:maxOpsInPrompt]
		}
		b.WriteString("Top operations from failed traces:\n")
		for _, op := range ops {
			b.WriteString("  - ")
			b.WriteString(op)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if len(c.FiringAlerts) > 0 {
		firing := c.FiringAlerts
		if len(firing) > maxAlertsInPrompt {
			firing = firing[:maxAlertsInPrompt]
		}
		b.WriteString("Firing Prometheus alerts for this service (invariant violations — DO NOT dismiss these as noise):\n")
		for _, a := range firing {
			b.WriteString("  - ")
			b.WriteString(a)
			b.WriteString("\n")
		}
		b.WriteString("NOTE: firing alerts signal constant-state invariant violations that spike detection may miss.\n\n")
	}

	b.WriteString(`Reasoning rules:
- three-strike rule: if a hypothesis is invalidated by data three times, drop it.
- Evidence-gated: never propose a root cause without at least one returning signal.
- Span-to-symbol: when a span operation maps to a known symbol via OperationToFuncName,
  the symbol's call_trace and adjacent code are stronger evidence than metric trends alone.
- Confidence calibration: high only when both metric anomaly + matching failed spans + symbol resolution agree.

Output schema (JSON, exactly):
{
  "summary": "<one paragraph>",
  "top_hypothesis": {
    "subject": "<short>",
    "reasoning": "<why this is the leading suspect>",
    "next_checks": ["<call_trace X>", "<code_search Y>"]
  }
}
`)
	return b.String()
}
