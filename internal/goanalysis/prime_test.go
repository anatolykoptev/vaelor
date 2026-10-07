package goanalysis_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
)

// fakeGo puts a `go` on PATH that records every invocation (argv, GOFLAGS,
// CGO_ENABLED) and fails, so the test sees exactly what each caller asks the go
// command to do without running it.
func fakeGo(t *testing.T) (logPath string) {
	t.Helper()
	dir := t.TempDir()
	logPath = filepath.Join(dir, "calls.log")
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	// Probes (env, version, context) go to the real toolchain; the import-graph
	// listing is recorded and fails.
	script := "#!/bin/sh\necho \"$* ## GOFLAGS=$GOFLAGS CGO_ENABLED=$CGO_ENABLED GOCACHE=$GOCACHE\" >> " + logPath +
		"\ncase \" $* \" in *\" -deps \"*) exit 1;; esac\nexec " + realGo + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(script), 0o700); err != nil { //nolint:gosec // test helper script must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

func listCall(t *testing.T, logPath string) string {
	t.Helper()
	raw, _ := os.ReadFile(logPath)
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(l, "list ") && strings.Contains(l, " -deps") {
			return strings.ReplaceAll(l, "=true", "")
		}
	}
	t.Fatalf("no `go list -deps` call recorded in:\n%s", raw)
	return ""
}

// The eager prewarm and the typed load must ask the go command for the same
// export data under the same environment: -test, CGO_ENABLED and GOFLAGS are
// inputs of the build-cache key, so a prewarm that differs warms nothing the load
// can reuse (measured: CGO_ENABLED=0 prewarm then CGO_ENABLED=1 load rebuilt the
// cgo-sensitive std packages, and test-only dependencies were never built).
//
// Mutation that must turn it RED: in ExportListArgs (prime.go) delete "-test",
// or give PrimeExportData's command an Env other than GoEnv(dir).
func TestPrimeAndLoad_AskForTheSameExportData(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/x\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	logPath := fakeGo(t)
	_, _ = goanalysis.PrimeExportData(context.Background(), dir, nil)
	prime := listCall(t, logPath)

	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	_, _ = goanalysis.LoadPackages(context.Background(), dir, goanalysis.LoadOpts{Tests: true})
	load := listCall(t, logPath)

	for _, flag := range []string{" -e ", " -export ", " -deps ", " -test "} {
		if !strings.Contains(" "+prime+" ", flag) || !strings.Contains(" "+load+" ", flag) {
			t.Errorf("flag %q must be in both invocations:\n prime: %s\n load:  %s", flag, prime, load)
		}
	}
	envOf := func(call string) string { return call[strings.Index(call, "## "):] }
	if envOf(prime) != envOf(load) {
		t.Errorf("prewarm and load run under different environments:\n prime: %s\n load:  %s", envOf(prime), envOf(load))
	}
	if !strings.Contains(envOf(load), "-p=") {
		t.Errorf("compile parallelism must be capped via GOFLAGS -p: %s", envOf(load))
	}
}

func TestGoEnv_CapsCompileParallelismAtHalfTheCores(t *testing.T) {
	want := "-p=" + strconv.Itoa(max(1, runtime.NumCPU()/2))
	for _, kv := range goanalysis.GoEnv(t.TempDir()) {
		if strings.HasPrefix(kv, "GOFLAGS=") && strings.Contains(kv, want) {
			return
		}
	}
	t.Errorf("GOFLAGS must contain %s", want)
}

func TestGoEnv_DisablesCgoWithoutACompiler(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no gcc, clang, cc
	t.Setenv("CC", "")
	var found bool
	for _, kv := range goanalysis.GoEnv(t.TempDir()) {
		if kv == "CGO_ENABLED=0" {
			found = true
		}
	}
	if !found {
		t.Error("without a C compiler cgo cannot work: CGO_ENABLED must be pinned to 0 so std builds as pure Go")
	}
}

// With a C compiler available the ambient CGO setting stands (production: gcc
// present, CGO_ENABLED=1): nothing may be pinned.
//
// Mutation that must turn it RED: make GoEnv append CGO_ENABLED=0 unconditionally.
func TestGoEnv_KeepsAmbientCgoWithACompiler(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gcc"), []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // test helper must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("CGO_ENABLED", "1")
	var last string
	for _, kv := range goanalysis.GoEnv(t.TempDir()) {
		if strings.HasPrefix(kv, "CGO_ENABLED=") {
			last = kv
		}
	}
	if last != "CGO_ENABLED=1" {
		t.Errorf("effective CGO_ENABLED = %q, want the ambient CGO_ENABLED=1 when a compiler exists", last)
	}
}

// Only one export-data build runs at a time, whoever asks: two concurrent primes
// must not overlap.
//
// Mutation that must turn it RED: widen exportGate (prime.go) to
// make(chan struct{}, 100).
func TestPrimeExportData_GateAdmitsOneBuildAtATime(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "builds.log")
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncase \" $* \" in *\" -deps \"*) echo start >> " + logPath +
		"; sleep 0.4; echo end >> " + logPath + "; exit 0;; esac\nexec " + realGo + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(script), 0o700); err != nil { //nolint:gosec // test helper must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	mod := t.TempDir()
	if err := os.WriteFile(filepath.Join(mod, "go.mod"), []byte("module example.com/x\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = goanalysis.PrimeExportData(context.Background(), mod, nil)
		}()
	}
	wg.Wait()

	raw, _ := os.ReadFile(logPath)
	if got, want := strings.Fields(string(raw)), strings.Fields(strings.Repeat("start end ", 3)); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("builds overlapped: %v, want strictly alternating start/end", got)
	}
}

// Charge is taken inside the gate and returned afterwards, and a charge error
// aborts the build without running it.
//
// Mutation that must turn it RED: in PrimeExportData call charge before taking
// the gate.
func TestPrimeExportData_ChargeIsTakenInsideTheGate(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "builds.log")
	realGo, _ := exec.LookPath("go")
	script := "#!/bin/sh\ncase \" $* \" in *\" -deps \"*) echo build >> " + logPath + "; sleep 0.3; exit 0;; esac\nexec " + realGo + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(script), 0o700); err != nil { //nolint:gosec // test helper must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	mod := t.TempDir()
	if err := os.WriteFile(filepath.Join(mod, "go.mod"), []byte("module example.com/x\n\ngo 1.22\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	held, maxHeld := 0, 0
	charge := func(context.Context) (func(), error) {
		mu.Lock()
		held++
		maxHeld = max(maxHeld, held)
		mu.Unlock()
		return func() { mu.Lock(); held--; mu.Unlock() }, nil
	}
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = goanalysis.PrimeExportData(context.Background(), mod, charge) }()
	}
	wg.Wait()
	if maxHeld != 1 {
		t.Errorf("up to %d charges held at once, want 1: a queued prime must hold nothing while it waits for the gate", maxHeld)
	}
	if held != 0 {
		t.Errorf("%d charges never returned", held)
	}

	wantErr := context.DeadlineExceeded
	os.Remove(logPath)
	_, err := goanalysis.PrimeExportData(context.Background(), mod, func(context.Context) (func(), error) { return nil, wantErr })
	if err != wantErr {
		t.Errorf("charge error = %v, want it returned", err)
	}
	if raw, _ := os.ReadFile(logPath); len(raw) != 0 {
		t.Error("the build must not run when the charge is refused")
	}
}
