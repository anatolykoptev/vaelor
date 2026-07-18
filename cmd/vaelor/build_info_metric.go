package main

import (
	"runtime/debug"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// buildInfoGitSHA is the VCS revision extracted from the binary's embedded
// build info (runtime/debug.ReadBuildInfo → vcs.revision setting). Falls back
// to the ldflags version var, then to "unknown" when neither is available.
// Populated once in init() so it is set before any test or main startup.
var buildInfoGitSHA string

func init() {
	buildInfoGitSHA = resolveBuildSHA()
	// gocode_build_info exposes the running binary's git SHA as a Prometheus
	// gauge (value always 1). Labelled by git_sha for deploy provenance:
	// Grafana / alertmanager rules can correlate metric gaps with specific SHAs
	// without parsing dozor logs.
	//
	// Set once at startup; never changes during the process lifetime.
	promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "gocode_build_info",
		Help: "Always 1. Labels carry build provenance (git_sha). Set once at startup.",
	}, []string{"git_sha"}).WithLabelValues(buildInfoGitSHA).Set(1)
}

// resolveBuildSHA returns the git revision embedded in the binary by the Go
// toolchain (vcs.revision build setting), falling back to the version ldflags
// var, then to "unknown".
func resolveBuildSHA() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" && s.Value != "" {
				return s.Value
			}
		}
	}
	if version != "" && version != "dev" {
		return version
	}
	return "unknown"
}
