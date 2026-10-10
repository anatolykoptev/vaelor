package goanalysis_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
)

const canaryKey = "VAELOR_CANARY_SECRET"

// trustAll marks every root operator-managed for the test and restores the
// fail-closed default afterwards.
func trustAll(t *testing.T) {
	t.Helper()
	goanalysis.SetRootTrust(func(string) bool { return true })
	t.Cleanup(func() { goanalysis.SetRootTrust(nil) })
}

// envRecorderGo puts a `go` on PATH that appends its whole environment to the
// returned file on every invocation, then execs the real toolchain (or, for the
// export-graph listing, just succeeds).
func envRecorderGo(t *testing.T) (envLog string) {
	t.Helper()
	dir := t.TempDir()
	envLog = filepath.Join(dir, "env.log")
	realGo, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nenv >> " + envLog + "\ncase \" $* \" in *\" -deps \"*) exit 0;; esac\nexec " + realGo + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "go"), []byte(script), 0o700); err != nil { //nolint:gosec // test helper must be executable
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return envLog
}

func goModDir(t *testing.T) string {
	t.Helper()
	return writeFixture(t, map[string]string{
		"go.mod": "module example.com/x\n\ngo 1.22\n",
		"x.go":   "package x\n",
	})
}

func readLog(t *testing.T, p string) string {
	t.Helper()
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("go child never ran: %v", err)
	}
	return string(raw)
}

// (a) A server secret in the process env must not reach the go child of an
// untrusted root, on the prime path and on the packages.Load path alike.
//
// Mutation that must turn it RED: in untrustedGoEnv (env.go) start the slice
// from os.Environ() instead of the allowlist literal.
func TestUntrustedRoot_CanaryEnvDoesNotReachGoChild(t *testing.T) {
	t.Setenv(canaryKey, "s3cr3t-canary")
	t.Setenv("GOAUTH", "netrc")
	t.Setenv("GITHUB_TOKEN", "ghp_canary")

	for name, run := range map[string]func(dir string){
		"prime": func(dir string) { _, _ = goanalysis.PrimeExportData(context.Background(), dir, nil) },
		"load": func(dir string) {
			_, _ = goanalysis.LoadPackages(context.Background(), dir, goanalysis.LoadOpts{})
		},
	} {
		t.Run(name, func(t *testing.T) {
			envLog := envRecorderGo(t)
			run(goModDir(t))
			got := readLog(t, envLog)
			for _, banned := range []string{canaryKey, "GOAUTH=", "GITHUB_TOKEN"} {
				if strings.Contains(got, banned) {
					t.Errorf("%s reached the go child of an untrusted root", banned)
				}
			}
			for _, want := range []string{"PATH=", "CGO_ENABLED=0", "GOTOOLCHAIN=local", "HOME="} {
				if !strings.Contains(got, want) {
					t.Errorf("hardened env lacks %s (the recorder saw a different env than the allowlist):\n%s", want, got)
				}
			}
		})
	}
}

// (d) Positive control: a trusted root keeps the inherited env (private-module
// credentials such as GOAUTH/netrc and GOPRIVATE must still reach go), so (a) is
// not passing because the recorder never sees env at all.
//
// Mutation that must turn it RED: make GoEnv (loader.go) return
// untrustedGoEnv(dir) unconditionally.
func TestTrustedRoot_KeepsInheritedEnv(t *testing.T) {
	trustAll(t)
	t.Setenv(canaryKey, "s3cr3t-canary")
	t.Setenv("GOAUTH", "netrc")

	for name, run := range map[string]func(dir string){
		"prime": func(dir string) { _, _ = goanalysis.PrimeExportData(context.Background(), dir, nil) },
		"load": func(dir string) {
			_, _ = goanalysis.LoadPackages(context.Background(), dir, goanalysis.LoadOpts{})
		},
	} {
		t.Run(name, func(t *testing.T) {
			envLog := envRecorderGo(t)
			run(goModDir(t))
			got := readLog(t, envLog)
			for _, want := range []string{canaryKey + "=s3cr3t-canary", "GOAUTH=netrc"} {
				if !strings.Contains(got, want) {
					t.Errorf("trusted root lost inherited %s", want)
				}
			}
		})
	}
}

func envValue(env []string, key string) (val string, ok bool) {
	for _, kv := range env {
		if v, found := strings.CutPrefix(kv, key+"="); found {
			val, ok = v, true // last wins, as in exec
		}
	}
	return val, ok
}

// (c) The hardened GOPROXY never contains "direct" — that fallback is what lets
// a repo-chosen module path dial an arbitrary host — and is "off" when the
// operator configured nothing.
//
// Mutation that must turn it RED: in proxyWithoutDirect (env.go) return raw.
func TestUntrustedGoEnv_GoproxyHasNoDirect(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"default with direct", "https://proxy.golang.org,direct", "https://proxy.golang.org"},
		{"pipe separated", "https://a.example|https://b.example|direct", "https://a.example,https://b.example"},
		{"direct only", "direct", "https://proxy.golang.org"},
		{"unset", "", "https://proxy.golang.org"},
		{"off stays off", "off", "off"},
		{"direct first", "direct,https://p.example", "https://p.example"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOPROXY", tc.in)
			got, ok := envValue(goanalysis.GoEnv(t.TempDir()), "GOPROXY")
			if !ok {
				t.Fatal("GOPROXY missing from the hardened env")
			}
			if got != tc.want {
				t.Errorf("GOPROXY = %q, want %q", got, tc.want)
			}
			for _, p := range strings.FieldsFunc(got, func(r rune) bool { return r == ',' || r == '|' }) {
				if p == "direct" {
					t.Errorf("GOPROXY %q still falls back to direct", got)
				}
			}
		})
	}
}

// A custom CA bundle must still reach go in the hardened env, or the proxy fetch
// fails on hosts that need one; GONOSUMCHECK (not a real go variable) is gone.
func TestUntrustedGoEnv_PassesCABundleVars(t *testing.T) {
	t.Setenv("SSL_CERT_FILE", "/etc/custom/ca.pem")
	env := goanalysis.GoEnv(t.TempDir())
	if v, _ := envValue(env, "SSL_CERT_FILE"); v != "/etc/custom/ca.pem" {
		t.Errorf("SSL_CERT_FILE = %q, want it passed through", v)
	}
	if _, ok := envValue(env, "GONOSUMCHECK"); ok {
		t.Error("GONOSUMCHECK is not a go variable and must not be set")
	}
}

// The trusted env keeps the ambient GOPROXY untouched (private modules resolve
// through direct/GOPRIVATE there).
func TestTrustedGoEnv_GoproxyUntouched(t *testing.T) {
	trustAll(t)
	t.Setenv("GOPROXY", "https://proxy.golang.org,direct")
	if got, _ := envValue(goanalysis.GoEnv(t.TempDir()), "GOPROXY"); got != "https://proxy.golang.org,direct" {
		t.Errorf("trusted GOPROXY = %q, want the ambient value", got)
	}
}

// Env-builder level (b): runs where no C compiler exists, so the cgo test below
// can skip without leaving CGO_ENABLED unpinned.
//
// Mutation that must turn it RED: delete "CGO_ENABLED=0" from untrustedGoEnv
// (env.go).
func TestUntrustedGoEnv_PinsCgoOff(t *testing.T) {
	t.Setenv("CGO_ENABLED", "1")
	if got, _ := envValue(goanalysis.GoEnv(t.TempDir()), "CGO_ENABLED"); got != "0" {
		t.Errorf("untrusted CGO_ENABLED = %q, want 0", got)
	}
}

// (b) Real toolchain: a cgo file that #includes a header OUTSIDE the repo must
// not be compiled for an untrusted root. gcc diagnostics are the channel by
// which an included file's content could surface (the header here is an #error
// whose message carries a marker), and the cgo-only function must be absent
// from the type-checked package.
//
// Mutation that must turn it RED: delete "CGO_ENABLED=0" from untrustedGoEnv
// (env.go) — the include is then compiled, the marker shows in the load errors
// and FromC appears.
func TestUntrustedRoot_CgoIncludeOutsideRootIsNotCompiled(t *testing.T) {
	if _, err := exec.LookPath("gcc"); err != nil {
		if _, err := exec.LookPath("cc"); err != nil {
			t.Skip("no C compiler; the CGO_ENABLED=0 pin is covered by TestUntrustedGoEnv_PinsCgoOff")
		}
	}
	const marker = "VAELOR_OUTSIDE_FILE_MARKER"
	outside := writeFixture(t, map[string]string{"secret.h": "#error " + marker + "\n"})
	fixture := map[string]string{
		"go.mod": "module example.com/c\n\ngo 1.22\n",
		"c.go": "package c\n\n/*\n#include \"" + filepath.Join(outside, "secret.h") + "\"\n*/\nimport \"C\"\n\n" +
			"func FromC() int { return int(C.int(1)) }\n",
		"plain.go": "package c\n\nfunc Plain() int { return 1 }\n",
	}

	t.Setenv("CGO_ENABLED", "1") // ambient prod setting

	lr := loadFixture(t, fixture)
	for _, e := range lr.Errors {
		if strings.Contains(e, marker) {
			t.Errorf("untrusted load compiled an out-of-root include; marker leaked into load errors: %s", e)
		}
	}
	for _, p := range lr.Packages {
		if p.Types != nil && p.Types.Scope().Lookup("FromC") != nil {
			t.Error("cgo file of an untrusted repo was type-checked (FromC present): CGO_ENABLED=0 not in effect")
		}
	}

	// Positive control: a trusted root does compile it, so the assertions above
	// can fail at all.
	trustAll(t)
	lr = loadFixture(t, fixture)
	var leaked bool
	for _, e := range lr.Errors {
		leaked = leaked || strings.Contains(e, marker)
	}
	if !leaked {
		t.Fatal("control: trusted root should compile the include and surface the marker; the test cannot detect the leak")
	}
}

func loadFixture(t *testing.T, files map[string]string) *goanalysis.LoadResult {
	t.Helper()
	dir := writeFixture(t, files)
	lr, err := goanalysis.LoadPackages(context.Background(), dir, goanalysis.LoadOpts{})
	if err != nil {
		// A package whose only file failed to compile still counts; but no
		// packages at all means nothing loaded.
		t.Fatalf("LoadPackages: %v", err)
	}
	return lr
}

// The hardened path is observable: each go command run under it bumps
// vaelor_goanalysis_untrusted_env_total.
//
// Mutation that must turn it RED: delete untrustedEnvTotal.Inc() in
// untrustedGoEnv (env.go).
func TestUntrustedGoEnv_IsCounted(t *testing.T) {
	const name = "vaelor_goanalysis_untrusted_env_total"
	unset := map[string]string{"reason": "trust_unset"}
	rejected := map[string]string{"reason": "untrusted_root"}
	beforeUnset := gatherCounterSum(t, name, unset)
	_ = goanalysis.GoEnv(t.TempDir())
	if after := gatherCounterSum(t, name, unset); after != beforeUnset+1 {
		t.Errorf("%s{trust_unset} moved %v -> %v, want +1", name, beforeUnset, after)
	}

	goanalysis.SetRootTrust(func(string) bool { return false })
	t.Cleanup(func() { goanalysis.SetRootTrust(nil) })
	beforeRej := gatherCounterSum(t, name, rejected)
	_ = goanalysis.GoEnv(t.TempDir())
	if after := gatherCounterSum(t, name, rejected); after != beforeRej+1 {
		t.Errorf("%s{untrusted_root} moved %v -> %v, want +1", name, beforeRej, after)
	}

	trustAll(t)
	total := gatherCounterSum(t, name, nil)
	_ = goanalysis.GoEnv(t.TempDir())
	if after := gatherCounterSum(t, name, nil); after != total {
		t.Errorf("trusted root must not bump %s (%v -> %v)", name, total, after)
	}
}
