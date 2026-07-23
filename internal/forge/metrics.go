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

// --- GitHub App auth observability (#598, #603, #610 gaps 5+7) --------------
//
// Two silent failure classes in the GitHub App auth path:
//
//   - Stale token served (#598): Token() falls back to a cached token on refresh
//     failure when the cache is still clock-valid. A sustained non-zero rate
//     means every subsequent API call rides an expiring token → 401 cascade.
//   - Auth mode invisible (#603): App vs PAT vs none is decided at forge
//     construction but never surfaced, so a partial App config (key unreadable,
//     install id missing) silently falls back to PAT — the operator can't tell.

// githubAppStaleTokenServedTotal counts every Token() call that served a stale
// cached token because the refresh HTTP call failed (network, 5xx) while the
// cached token was still clock-valid. Should stay at 0 in steady state; a
// sustained rate signals an expiring token + failing refresh → 401 cascade.
var githubAppStaleTokenServedTotal = promauto.NewCounter(
	prometheus.CounterOpts{
		Name: "gocode_github_app_stale_token_served_total",
		Help: "GitHub App installation tokens served stale from cache after a refresh failure (401-cascade risk, #598).",
	},
)

// githubAuthMode is a gauge labelled by the active GitHub auth mode
// (app | pat | none), value 1 for the active mode and 0 for the others.
// Published once at forge construction (production api.github.com base only)
// so the operator can see which auth mode is live without issuing a request.
// Mirrors the gocode_keyword_arm_active pattern.
var githubAuthMode = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "gocode_github_auth_mode",
		Help: "Active GitHub auth mode (1 = active). app = GitHub App installation tokens, pat = static PAT, none = unauthenticated (60 req/h).",
	},
	[]string{"mode"},
)

func init() {
	// Pre-touch all three label values so /metrics always exports every series.
	githubAuthMode.WithLabelValues(authModeApp).Set(0)
	githubAuthMode.WithLabelValues(authModePAT).Set(0)
	githubAuthMode.WithLabelValues(authModeNone).Set(0)
}

// publishGitHubAuthMode sets the auth-mode gauge so the active mode reads 1 and
// the others read 0. Called from newGitHubForgeWithBase for the production
// api.github.com base only (test servers must not mutate the process gauge).
func publishGitHubAuthMode(mode string) {
	githubAuthMode.WithLabelValues(authModeApp).Set(0)
	githubAuthMode.WithLabelValues(authModePAT).Set(0)
	githubAuthMode.WithLabelValues(authModeNone).Set(0)
	githubAuthMode.WithLabelValues(mode).Set(1)
}
