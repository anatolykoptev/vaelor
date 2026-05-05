package forge

import (
	"strings"

	"github.com/anatolykoptev/go-code/internal/slugparse"
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
//
// Decision tree:
//   - SSH form (git@…) with a colon but an unrecognised host → reject_unknown_host
//   - SSH form without a colon, or any non-SSH input           → invalid_form
func resolveOutcome(input string) string {
	if strings.HasPrefix(input, "git@") {
		// SSHHostKind returns false for both "no colon" and "unknown host".
		// Distinguish them by checking for the colon ourselves.
		_, ok := slugparse.SSHHostKind(input)
		if !ok && strings.Contains(input, ":") {
			// Has colon but host is not in the known-host allowlist.
			return "reject_unknown_host"
		}
		return "invalid_form"
	}
	return "invalid_form"
}
