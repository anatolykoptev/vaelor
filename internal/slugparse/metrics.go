package slugparse

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// formInvalid is the metric label used when the input does not match any
// recognised slug form.
const formInvalid = "invalid"

// slugNormalizeTotal counts every call to Parse / ParseWithOptions labelled by
// the recognised input form and whether the call was accepted or rejected.
//
//   - form: bare | github_url | gitlab_url | github_ssh | gitlab_ssh | invalid
//   - kind: accept | reject
//
// Cardinality: 6 × 2 = 12 series.
var slugNormalizeTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "gocode_slug_normalize_total",
		Help: "Slug normalization invocations by recognized input form and outcome (accept/reject).",
	},
	[]string{"form", "kind"},
)

// classifyForm returns the label value for the "form" dimension based on the
// raw input string, before any parsing is attempted.
//
// The classification mirrors the branches inside stripPrefix so that the
// recorded form always reflects the input shape that ParseWithOptions saw.
func classifyForm(input string) string {
	if input == "" || isLocalPath(input) {
		return formInvalid
	}
	// SSH form: git@<host>:…
	if len(input) > 4 && input[:4] == "git@" {
		host, ok := SSHHostKind(input)
		if !ok {
			return formInvalid
		}
		switch host {
		case "github.com":
			return "github_ssh"
		case "gitlab.com":
			return "gitlab_ssh"
		}
		return formInvalid
	}
	// URL or bare-host form.
	for _, pfx := range []string{"https://github.com", "http://github.com", "github.com/"} {
		if len(input) >= len(pfx) && input[:len(pfx)] == pfx {
			return "github_url"
		}
	}
	for _, pfx := range []string{"https://gitlab.com", "http://gitlab.com", "gitlab.com/"} {
		if len(input) >= len(pfx) && input[:len(pfx)] == pfx {
			return "gitlab_url"
		}
	}
	return "bare"
}
