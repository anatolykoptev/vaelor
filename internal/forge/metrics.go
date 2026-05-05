package forge

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// forgeResolveTotal counts every ExtractSlug invocation labelled by the
// detected forge and the parse outcome.
//
//   - forge:   github | gitlab | unknown
//   - outcome: success | invalid_form | reject_unknown_host
//
// Cardinality: 3 × 3 = 9 series.
var forgeResolveTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_forge_resolve_total",
		Help: "ExtractSlug invocations by forge (github, gitlab, unknown) and outcome (success, invalid_form, reject_unknown_host).",
	},
	[]string{"forge", "outcome"},
)

// forgeLabel maps a ForgeKind to its metric label string.
func forgeLabel(k ForgeKind) string {
	switch k {
	case GitHub:
		return "github"
	case GitLab:
		return "gitlab"
	default:
		return "unknown"
	}
}

// resolveOutcome classifies a failed ExtractSlug call into its outcome label.
// It distinguishes the SSH-unknown-host case (git@evil.com:…) from other
// invalid-form rejections.
func resolveOutcome(input string) string {
	// SSH form with an unrecognised host is a distinct security rejection.
	if strings.HasPrefix(input, "git@") && !strings.HasPrefix(input, "git@github.com:") &&
		!strings.HasPrefix(input, "git@gitlab.com:") {
		return "reject_unknown_host"
	}
	return "invalid_form"
}
