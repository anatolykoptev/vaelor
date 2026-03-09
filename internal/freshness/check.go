package freshness

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Concurrency and timeout constants for freshness checks.
const (
	maxConcurrency   = 10
	perLookupTimeout = 3 * time.Second
	semverParts      = 3
)

// Version comparison result constants.
const (
	verCurrent = "current"
	verMinor   = "minor"
	verMajor   = "major"
	verUnknown = "unknown"
)

// depResult holds the outcome of a single dependency lookup.
type depResult struct {
	outdated *OutdatedDep
	kind     string // "current", "minor", "major", or "" for skipped
}

// CheckFreshness checks all dependencies against their registries concurrently.
func CheckFreshness(ctx context.Context, deps []Dependency, reg *MultiRegistry) *FreshnessResult {
	if len(deps) == 0 {
		return &FreshnessResult{Ratio: 1.0}
	}

	results := make([]depResult, len(deps))
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxConcurrency)

	for i, dep := range deps {
		registry := reg.ForLanguage(dep.Language)
		if registry == nil {
			continue
		}

		wg.Add(1)
		go func(idx int, d Dependency, r Registry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			results[idx] = lookupDep(ctx, d, r)
		}(i, dep, registry)
	}

	wg.Wait()
	return aggregateResults(results)
}

// lookupDep fetches the latest version and compares it to the current.
func lookupDep(ctx context.Context, dep Dependency, reg Registry) depResult {
	lookupCtx, cancel := context.WithTimeout(ctx, perLookupTimeout)
	defer cancel()

	latest, err := reg.Latest(lookupCtx, dep.Name)
	if err != nil {
		return depResult{}
	}

	kind := compareSemver(dep.Version, latest)
	if kind == verUnknown {
		return depResult{}
	}

	res := depResult{kind: kind}
	if kind == verMinor || kind == verMajor {
		res.outdated = &OutdatedDep{
			Name:    dep.Name,
			Current: dep.Version,
			Latest:  latest,
			Kind:    kind,
		}
	}
	return res
}

// aggregateResults computes totals from per-dependency results.
func aggregateResults(results []depResult) *FreshnessResult {
	var fr FreshnessResult
	for _, r := range results {
		switch r.kind {
		case verCurrent:
			fr.Total++
			fr.UpToDate++
		case verMinor:
			fr.Total++
			fr.MinorOutdated++
			if r.outdated != nil {
				fr.Outdated = append(fr.Outdated, *r.outdated)
			}
		case verMajor:
			fr.Total++
			fr.MajorOutdated++
			if r.outdated != nil {
				fr.Outdated = append(fr.Outdated, *r.outdated)
			}
		}
	}

	if fr.Total > 0 {
		fr.Ratio = float64(fr.UpToDate) / float64(fr.Total)
	} else {
		fr.Ratio = 1.0
	}
	return &fr
}

// compareSemver compares two semver strings and returns:
//   - "current" if same version
//   - "minor" if same major but newer minor/patch
//   - "major" if different major
//   - "unknown" if either version can't be parsed
func compareSemver(current, latest string) string {
	curParts := parseSemver(current)
	latParts := parseSemver(latest)
	if curParts == nil || latParts == nil {
		return verUnknown
	}

	if curParts[0] != latParts[0] {
		return verMajor
	}
	if curParts[1] != latParts[1] || curParts[2] != latParts[2] {
		return verMinor
	}
	return verCurrent
}

// parseSemver extracts [major, minor, patch] from a version string.
// Strips leading "v", "^", "~", ">=", ">", "<=", "<", "=" prefixes.
// Returns nil if the version cannot be parsed.
func parseSemver(version string) []int {
	v := stripVersionPrefix(version)
	if v == "" {
		return nil
	}

	parts := [semverParts]int{}
	idx := 0
	for seg := range strings.SplitSeq(v, ".") {
		if idx >= semverParts {
			break
		}
		n, err := strconv.Atoi(seg)
		if err != nil {
			return nil
		}
		parts[idx] = n
		idx++
	}

	if idx == 0 {
		return nil
	}
	result := parts[:]
	return result
}

// stripVersionPrefix removes common version prefixes (v, ^, ~, >=, etc.).
func stripVersionPrefix(s string) string {
	s = strings.TrimSpace(s)
	for _, prefix := range []string{">=", "<=", ">>", "<<", ">", "<", "~=", "~", "^", "=", "v"} {
		if after, found := strings.CutPrefix(s, prefix); found {
			s = after
			break
		}
	}
	return s
}
