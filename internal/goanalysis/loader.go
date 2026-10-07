package goanalysis

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/tools/go/packages"
)

const defaultTimeout = 10 * time.Minute

// LoadOpts configures package loading.
type LoadOpts struct {
	Patterns []string      // package patterns to load (default: "./...")
	Timeout  time.Duration // override default 60s timeout
	// Tests also loads each package's test variants ("p [p.test]" and the
	// external "p_test [p.test]"), exposed as LoadResult.TestPackages. Without
	// them test files are never type-checked, so every call made from a _test.go
	// file has to be resolved by name alone.
	Tests bool
	// SourceDeps type-checks every dependency from source too (NeedDeps). The
	// default type-checks only the matched packages from source and reads each
	// dependency from compiler export data, which holds the dependency's types
	// but not its ASTs or per-expression type info: on v1.65.16 with tests that
	// is 311 MB peak RSS instead of 1.29 GB, on a 172-package module 0.9 GB
	// instead of 2.1 GB, with an identical typed edge set. A dependency whose
	// export data cannot be built (does not compile) is type-checked from
	// source by go/packages itself; SourceDeps forces that for all of them and
	// reproduces the old behaviour.
	SourceDeps bool
}

// LoadResult contains loaded packages with full type information.
type LoadResult struct {
	Packages []*packages.Package
	// TestPackages are the test variants of Packages (only with LoadOpts.Tests).
	// They are kept apart on purpose: a variant re-contains every non-test file
	// of its base package, so folding them into Packages would double every
	// consumer that iterates it (call edges, IMPLEMENTS satisfaction).
	TestPackages []*packages.Package
	Errors       []string // non-fatal errors
}

// HasGoModule checks if dir contains a go.mod file.
func HasGoModule(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "go.mod"))
	return err == nil
}

// LoadPackages loads Go packages from dir with full type info.
// Returns error if go.mod missing or context expires.

// goEnv returns env vars for go/packages.Load.
// Uses -mod=vendor when vendor/ dir exists (read-only mounts), else -mod=mod.
func goEnv(dir string) []string {
	flag := "-mod=mod"
	if _, err := os.Stat(filepath.Join(dir, "vendor")); err == nil {
		flag = "-mod=vendor"
	}
	return append(os.Environ(), "GOFLAGS="+flag, "GONOSUMCHECK=*", "GONOSUMDB=*", "GOCACHE=/tmp/go-build-cache", "GOPATH=/tmp/gopath", "GOWORK=off")
}

func LoadPackages(ctx context.Context, dir string, opts LoadOpts) (*LoadResult, error) {
	if !HasGoModule(dir) {
		return nil, fmt.Errorf("no go.mod found in %s", dir)
	}

	timeout := defaultTimeout
	if opts.Timeout > 0 {
		timeout = opts.Timeout
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	patterns := opts.Patterns
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}

	mode := packages.NeedName |
		packages.NeedTypes |
		packages.NeedSyntax |
		packages.NeedTypesInfo |
		packages.NeedImports |
		packages.NeedForTest
	if opts.SourceDeps {
		mode |= packages.NeedDeps
	} else {
		mode |= packages.NeedExportFile
	}
	cfg := &packages.Config{
		Mode:    mode,
		Dir:     dir,
		Tests:   opts.Tests,
		Context: ctx,
		Env:     goEnv(dir),
	}

	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("packages.Load: %w", err)
	}

	// packages.Load materialises the ENTIRE types.Info for every package it
	// touches: NeedTypesInfo is all-or-nothing, so asking for the Defs, Uses
	// and Selections the resolver reads also builds Types, Implicits, Scopes,
	// Instances, InitOrder and FileVersions, which nothing here reads. Types
	// is the largest by far — one entry per expression in the graph.
	//
	// Release them before returning, or they stay reachable for as long as the
	// caller holds the LoadResult.
	releaseUnreadTypeInfo(pkgs)

	result := &LoadResult{}
	for _, pkg := range pkgs {
		for _, e := range pkg.Errors {
			result.Errors = append(result.Errors, e.Error())
		}
		if pkg.TypesInfo == nil {
			continue
		}
		switch {
		case isSyntheticTestMain(pkg):
			// go-generated "p.test" main package: files live in the build cache.
		case isTestVariant(pkg):
			result.TestPackages = append(result.TestPackages, pkg)
		default:
			result.Packages = append(result.Packages, pkg)
		}
	}

	if len(result.Packages) == 0 {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("package loading timed out or cancelled: %w", ctx.Err())
		}
		return nil, fmt.Errorf("no packages with type information loaded from %s", dir)
	}

	return result, nil
}

// isTestVariant reports whether pkg was rebuilt for a test ("p [p.test]" or
// the external "p_test [p.test]"). go/packages states this in ForTest rather
// than leaving callers to parse the build-system ID.
func isTestVariant(pkg *packages.Package) bool {
	return pkg.ForTest != ""
}

// isSyntheticTestMain reports whether pkg is the generated test main
// ("p.test": package main, not itself rebuilt for a test, path ending ".test").
// Its CompiledGoFiles are empty or build-cache paths in this load mode, so the
// generated _testmain.go cannot be used to recognise it.
func isSyntheticTestMain(pkg *packages.Package) bool {
	return pkg.Name == "main" && pkg.ForTest == "" && strings.HasSuffix(pkg.PkgPath, ".test")
}

// releaseUnreadTypeInfo drops the types.Info maps this repo never reads, across
// the whole package graph.
//
// The kept set is Defs, Uses and Selections — the three the resolver consumes
// (internal/goanalysis/resolver.go, resolver_dispatch.go). Everything else is
// a by-product of NeedTypesInfo being a single all-or-nothing Mode flag.
//
// It walks with packages.Visit rather than ranging over the roots, because
// NeedDeps gives every DEPENDENCY its own fully-populated types.Info too. The
// roots are a small fraction of the retained bytes; a roots-only loop looks
// like it works and frees almost nothing, which is why
// TestLoadPackages_ReleasesAcrossDependencyGraph asserts on an imported
// package rather than on the root.
//
// A dropped map reads as empty, not as a panic: indexing a nil Go map returns
// the zero value. So a future caller that starts reading Types would get a
// silent "no entry" for every expression rather than a crash — which is why
// the kept set is pinned by a test instead of left to a comment.
func releaseUnreadTypeInfo(pkgs []*packages.Package) {
	roots := make(map[*packages.Package]bool, len(pkgs))
	for _, p := range pkgs {
		roots[p] = true
	}
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		if !roots[p] {
			// A dependency type-checked from source (SourceDeps): only its
			// *types.Package is ever read (as an import of a root). Its ASTs
			// and per-expression info are the bulk of the arena.
			p.Syntax = nil
			p.TypesInfo = nil
			return
		}
		if p.TypesInfo == nil {
			return
		}
		p.TypesInfo.Types = nil
		p.TypesInfo.Implicits = nil
		p.TypesInfo.Scopes = nil
		p.TypesInfo.Instances = nil
		p.TypesInfo.InitOrder = nil
		p.TypesInfo.FileVersions = nil
	})
}
