package compare

import (
	"context"
	"os"
	"path/filepath"

	"github.com/anatolykoptev/vaelor/internal/routes"
)

// RouteDiff summarises the difference between two sets of HTTP routes.
type RouteDiff struct {
	Common     int            `json:"common"`
	OnlyACount int            `json:"onlyACount"`
	OnlyBCount int            `json:"onlyBCount"`
	OnlyA      []routes.Route `json:"onlyA,omitempty"`
	OnlyB      []routes.Route `json:"onlyB,omitempty"`
}

// routeKey returns a canonical key for a route used when matching.
func routeKey(r routes.Route) string {
	return r.Method + " " + routes.NormalizePath(r.Path)
}

// ComputeRouteDiff compares two route slices and returns a RouteDiff.
// Routes are matched by method + normalised path; handler names are ignored.
func ComputeRouteDiff(a, b []routes.Route) RouteDiff {
	keyA := make(map[string]struct{}, len(a))
	for _, r := range a {
		keyA[routeKey(r)] = struct{}{}
	}

	keyB := make(map[string]struct{}, len(b))
	for _, r := range b {
		keyB[routeKey(r)] = struct{}{}
	}

	var diff RouteDiff

	for _, r := range a {
		if _, ok := keyB[routeKey(r)]; ok {
			diff.Common++
		} else {
			diff.OnlyACount++
			diff.OnlyA = append(diff.OnlyA, r)
		}
	}

	for _, r := range b {
		if _, ok := keyA[routeKey(r)]; !ok {
			diff.OnlyBCount++
			diff.OnlyB = append(diff.OnlyB, r)
		}
	}

	return diff
}

// ExtractRoutes walks the snapshot files, reads each source file, and extracts
// HTTP routes using the registered route matchers. Only files whose language
// matches snap.Language are processed. The root parameter is the absolute path
// to the repository root used to build absolute file paths from RelPath.
// Context cancellation is respected between files.
func ExtractRoutes(ctx context.Context, root string, snap *RepoSnapshot) []routes.Route {
	var all []routes.Route

	for _, f := range snap.Files {
		if ctx.Err() != nil {
			break
		}

		if snap.Language != "" && f.Language != snap.Language {
			continue
		}

		absPath := filepath.Join(root, f.RelPath)
		source, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}

		extracted := routes.ExtractAll(f.Language, source)
		for i := range extracted {
			extracted[i].File = f.RelPath
		}

		all = append(all, extracted...)
	}

	return all
}
