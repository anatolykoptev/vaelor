package goanalysis

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/tools/go/packages"
)

const defaultTimeout = 10 * time.Minute

// LoadOpts configures package loading.
type LoadOpts struct {
	Patterns []string      // package patterns to load (default: "./...")
	Timeout  time.Duration // override default 60s timeout
}

// LoadResult contains loaded packages with full type information.
type LoadResult struct {
	Packages []*packages.Package
	Errors   []string // non-fatal errors
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

	cfg := &packages.Config{
		Mode: packages.NeedName |
			packages.NeedTypes |
			packages.NeedSyntax |
			packages.NeedTypesInfo |
			packages.NeedImports |
			packages.NeedDeps,
		Dir:     dir,
		Context: ctx,
		Env:     goEnv(dir),
	}

	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("packages.Load: %w", err)
	}

	result := &LoadResult{}
	for _, pkg := range pkgs {
		for _, e := range pkg.Errors {
			result.Errors = append(result.Errors, e.Error())
		}
		if pkg.TypesInfo != nil {
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
