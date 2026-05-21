// internal/fleet/upstream/registry.go
package upstream

import "strings"

// defaultRegistry maps a docker image name (registry+repo, no tag) to a GitHub
// "owner/repo" identifier for the Compare API.
//
// Keys are the exact image strings as they appear in Dockerfiles / compose files.
// Case-sensitive.
//
// Note: "nginx/nginx-content" appears in the spec as-written; the canonical
// upstream is nginx/nginx but the spec lists the content mirror. Flagged in
// DONE_WITH_CONCERNS.
var defaultRegistry = map[string]string{
	"redis":                            "redis/redis",
	"nginx":                            "nginx/nginx",
	"alpine":                           "alpinelinux/aports",
	"golang":                           "golang/go",
	"node":                             "nodejs/node",
	"python":                           "python/cpython",
	"rust":                             "rust-lang/rust",
	"grafana/grafana":                  "grafana/grafana",
	"grafana/loki":                     "grafana/loki",
	"grafana/promtail":                 "grafana/loki",
	"jaegertracing/jaeger":             "jaegertracing/jaeger",
	"prom/prometheus":                  "prometheus/prometheus",
	"prom/alertmanager":                "prometheus/alertmanager",
	"prom/blackbox-exporter":           "prometheus/blackbox_exporter",
	"prom/pushgateway":                 "prometheus/pushgateway",
	"quay.io/prometheus/node-exporter": "prometheus/node_exporter",
	"qdrant/qdrant":                    "qdrant/qdrant",
	"teddysun/xray":                    "XTLS/Xray-core",
	"dperson/torproxy":                 "dperson/torproxy",
	"registry":                         "distribution/distribution",
	"pgvector/pgvector":                "pgvector/pgvector",
	"searxng/searxng":                  "searxng/searxng",
	"eceasy/cli-proxy-api":             "luispater/CLIProxyAPI",
}

// Resolve maps a docker image name (registry+repo, no tag) to a GitHub
// "owner/repo" identifier suitable for the Compare API. Returns ok=false
// when no mapping is known and no ghcr.io-style auto-detect applies.
//
// Order of resolution:
//  1. Exact match in the hardcoded registry.
//  2. Prefix "ghcr.io/" → strip and return first two path segments as owner/repo.
//     Requires exactly 2+ path segments after the prefix; returns ok=false otherwise.
//  3. Anything else: ok=false.
func Resolve(image string) (githubSlug string, ok bool) {
	// 1. Exact hardcoded match.
	if slug, found := defaultRegistry[image]; found {
		return slug, true
	}

	// 2. ghcr.io heuristic.
	const ghcrPrefix = "ghcr.io/"
	if strings.HasPrefix(image, ghcrPrefix) {
		rest := image[len(ghcrPrefix):]
		// rest must have at least owner/repo (two non-empty segments).
		parts := strings.SplitN(rest, "/", 3)
		if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
			return parts[0] + "/" + parts[1], true
		}
		return "", false
	}

	// 3. No mapping.
	return "", false
}
