package forge

import (
	"strings"

	"github.com/anatolykoptev/vaelor/internal/slugparse"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// githubAPICallsTotal counts every GitHub API call made through the forge
// HTTP client, labelled by endpoint, HTTP status, and auth mode.
//
//   - endpoint:  bounded first-path-segment label (pulls, issues, search, app, other, …)
//   - status:    HTTP response code as decimal string (200, 404, 429, …) or "transport_error"
//   - auth_mode: app | app_jwt | pat | none — bounded enum, 4 values
//
// Cardinality: ~15 endpoint × ~11 status × 4 auth_mode ≈ 660 series.
var githubAPICallsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_github_api_calls_total",
		Help: "GitHub API calls by endpoint, HTTP status, and auth mode. Bounded enums only.",
	},
	[]string{"endpoint", "status", "auth_mode"},
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
