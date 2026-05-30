package coupling

import (
	"context"
	"os"
	"path/filepath"

	"github.com/anatolykoptev/go-code/internal/parser"
	"github.com/anatolykoptev/go-code/internal/routes"
)

// maxVerifyFileBytes bounds a file read during verification. Routes never live
// in huge files; skip anything larger to keep stage-2 cheap on the ARM box.
const maxVerifyFileBytes = 512 * 1024

// routeVerifier (T0) proves a provider↔consumer HTTP dependency: a server route
// in one file matched to a client call to the same method + normalized path in
// the other. Fully offline (routes.ExtractAll is pure regex). A per-instance
// cache dedupes reads of a file that appears in multiple candidate pairs.
type routeVerifier struct {
	cache map[string][]routes.Route // key: root+"\x00"+rel
}

// NewRouteVerifier returns a fresh T0 route verifier. Construct one per tool
// call (the cache is per-call, not shared across calls).
func NewRouteVerifier() *routeVerifier {
	return &routeVerifier{cache: make(map[string][]routes.Route)}
}

// Verify implements Verifier: returns route Evidence when a server route in one
// file matches a client route in the other by method + normalized path.
func (v *routeVerifier) Verify(_ context.Context, a, b FilePair) ([]Evidence, error) {
	ra := v.routesOf(a)
	rb := v.routesOf(b)
	if len(ra) == 0 || len(rb) == 0 {
		return nil, nil
	}
	var ev []Evidence
	seen := make(map[string]bool)
	for _, x := range ra {
		for _, y := range rb {
			if !((x.Side == "server" && y.Side == "client") ||
				(x.Side == "client" && y.Side == "server")) {
				continue
			}
			if routeKey(x) != routeKey(y) {
				continue
			}
			k := routeKey(x)
			if seen[k] {
				continue
			}
			seen[k] = true
			ev = append(ev, Evidence{Kind: "route", Detail: k, Tier: "offline"})
		}
	}
	return ev, nil
}

// routeKey is method + normalized path, matching the compare.routeKey convention.
// NormalizePath collapses :id/{id} → * and strips host.
func routeKey(r routes.Route) string {
	return r.Method + " " + routes.NormalizePath(r.Path)
}

// routesOf reads + extracts routes for a file, cached per (root, rel).
func (v *routeVerifier) routesOf(f FilePair) []routes.Route {
	key := f.Root + "\x00" + f.Rel
	if cached, ok := v.cache[key]; ok {
		return cached
	}
	rs := extractFileRoutes(f.Root, f.Rel)
	v.cache[key] = rs
	return rs
}

func extractFileRoutes(root, rel string) []routes.Route {
	full := filepath.Join(root, rel)
	info, err := os.Stat(full)
	if err != nil || info.IsDir() || info.Size() > maxVerifyFileBytes {
		return nil
	}
	src, err := os.ReadFile(full) //nolint:gosec // root+rel are trusted local paths from ResolveRepos
	if err != nil {
		return nil
	}
	lang := parser.DetectLanguageFromPath(rel)
	if lang == "" {
		return nil // no matcher (markdown, lockfiles, etc.)
	}
	rs := routes.ExtractAll(lang, src)
	for i := range rs {
		rs[i].File = rel
	}
	return rs
}
