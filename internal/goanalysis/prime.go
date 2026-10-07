package goanalysis

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// exportGate admits ONE export-data build at a time, process-wide. A cold build
// compiles every dependency (cmd/compile and cgo children, hundreds of MB each,
// in the server's cgroup but outside the Go heap), so two overlapping cold
// builds are what turns a cache miss into an OOM kill. Warm builds finish in a
// second and queue behind a cold one only while it runs.
var exportGate = make(chan struct{}, 1)

// ExportListArgs is the `go list` argument shape that makes the go command build
// the export data a typed load consumes: -export (build it), -deps (the whole
// import graph), -test (the test variants the load also type-checks), -e (a
// package that does not compile must not abort the rest). The eager prewarm and
// the load-time priming both call it, so what one builds the other finds.
func ExportListArgs(modFlag string) []string {
	return []string{"list", "-e", "-export", "-deps", "-test",
		"-f", "{{if or .Error .DepsErrors}}ERR {{.ImportPath}}{{end}}",
		modFlag, "./..."}
}

// BuildCharge reserves whatever the caller accounts a running build against
// (a memory budget) and returns the function that gives it back.
type BuildCharge func(ctx context.Context) (release func(), err error)

// PrimeExportData builds the export data of dir's import graph under the
// process-wide gate and GoEnv, returning the import paths that produced none.
// It waits for the gate until ctx ends.
//
// charge, when non-nil, is taken AFTER the gate and held only while the build
// runs: a caller queued behind another build must hold nothing, or the idle
// reservations of the queue starve loads that would fit.
func PrimeExportData(ctx context.Context, dir string, charge BuildCharge) (errored []string, err error) {
	select {
	case exportGate <- struct{}{}:
		defer func() { <-exportGate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if charge != nil {
		release, cerr := charge(ctx)
		if cerr != nil {
			return nil, cerr
		}
		defer release()
	}
	cmd := exec.CommandContext(ctx, "go", ExportListArgs(ModFlag(dir))...)
	cmd.Dir = dir
	cmd.Env = GoEnv(dir)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -export: %w", err)
	}
	for _, line := range bytes.Split(out, []byte("\n")) {
		if path, ok := bytes.CutPrefix(line, []byte("ERR ")); ok {
			errored = append(errored, string(path))
		}
	}
	return errored, nil
}
