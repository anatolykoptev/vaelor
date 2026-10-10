package goanalysis

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// untrustedEnvTotal counts go commands (typed load, export-data prime, prewarm)
// run under the hardened environment. On those repos cgo is off, so cgo-only
// packages are type-checked without their C-backed files, and module resolution
// is proxy-only: the typed tier is degraded on purpose, and this is how an
// operator sees it.
var untrustedEnvTotal = promauto.NewCounter(prometheus.CounterOpts{
	Name: "vaelor_goanalysis_untrusted_env_total",
	Help: "go commands run with the hardened env (allowlisted vars, CGO_ENABLED=0, GOPROXY without direct) because the repo root is not trusted.",
})

// rootTrust decides whether a repo root is an operator-managed checkout. It is
// injected rather than imported: internal/scip owns the trust list and already
// imports this package. Unset means no root is trusted (fail closed).
var rootTrust atomic.Pointer[func(root string) bool]

// SetRootTrust installs the predicate that marks a root as operator-managed.
// Call it at startup, next to the trust list it reads.
func SetRootTrust(f func(root string) bool) {
	if f == nil {
		rootTrust.Store(nil)
		return
	}
	rootTrust.Store(&f)
}

func isTrustedRoot(dir string) bool {
	f := rootTrust.Load()
	return f != nil && (*f)(dir)
}

// Cache and GOPATH locations shared by the trusted and the hardened env: both
// reuse the same warmed caches.
const (
	goCacheDir = "/tmp/go-build-cache"
	goPathDir  = "/tmp/gopath"
)

var (
	scrubHomeOnce sync.Once
	scrubHomeDir  string
)

// scrubHome is an empty per-process HOME for children of untrusted repos, so
// no ~/.netrc, ~/.gitconfig, ~/.config/go or ssh keys of the server user are
// visible to the go command, git or the compiler. If it cannot be created the
// path is a non-existent one, which is just as empty.
func scrubHome() string {
	scrubHomeOnce.Do(func() {
		d, err := os.MkdirTemp("", "vaelor-gohome-")
		if err != nil {
			slog.Warn("goanalysis: cannot create scrub HOME; using a non-existent one", "err", err)
			d = "/nonexistent-vaelor-home"
		}
		scrubHomeDir = d
	})
	return scrubHomeDir
}

// proxyWithoutDirect returns raw (a GOPROXY value) with every "direct" entry
// removed, "off" when nothing is left or raw is empty. "direct" is what lets a
// repo-chosen module path make the server dial an arbitrary host (git/https to
// internal addresses); a configured proxy host is operator-chosen and stays.
func proxyWithoutDirect(raw string) string {
	var keep []string
	for _, p := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '|' }) {
		p = strings.TrimSpace(p)
		if p == "" || p == "direct" {
			continue
		}
		keep = append(keep, p)
	}
	if len(keep) == 0 {
		return "off"
	}
	return strings.Join(keep, ",")
}

// untrustedGoEnv builds the go command environment from an allowlist, never
// from os.Environ(): the server's secrets (tokens, DSNs, API keys) must not
// reach go, gcc or git running over repo-controlled content. Each entry:
//
//   - PATH: to find go, gcc, git.
//   - HOME=scrubHome: hides the server user's credentials/config files.
//   - GOENV=off: ignore any go env file.
//   - GOCACHE/GOPATH/GOMODCACHE: the existing warm caches (GOMODCACHE only when
//     the operator set it; otherwise it derives from GOPATH).
//   - GOFLAGS: ours only; -buildvcs=false keeps go from running git inside the
//     repo (a repo .git/config can name commands, e.g. core.fsmonitor).
//   - GOPROXY: the operator's proxy without the "direct" fallback, else off.
//   - GOVCS=*:off: no VCS fetch even if a direct source slipped through.
//   - GOTOOLCHAIN=local: never download a toolchain a repo go.mod names.
//   - CGO_ENABLED=0: gcc never sees repo C code (#include / .incbin can read any
//     file the process can).
//   - GONOSUMDB=*: unchanged from the trusted env; repo-chosen modules cannot
//     be verified by the public sum DB and the lookup is one more outbound call.
//   - GOWORK=off, GIT_TERMINAL_PROMPT=0, GIT_CONFIG_NOSYSTEM=1: no workspace
//     file outside the repo, no prompt, no system git config.
//
// GOAUTH, GOPRIVATE, GONOPROXY, GOINSECURE and every other variable are absent.
func untrustedGoEnv(dir string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + scrubHome(),
		"GOENV=off",
		"GOCACHE=" + goCacheDir,
		"GOPATH=" + goPathDir,
		fmt.Sprintf("GOFLAGS=%s -p=%d -trimpath -buildvcs=false", ModFlag(dir), buildParallelism()),
		"GOPROXY=" + proxyWithoutDirect(os.Getenv("GOPROXY")),
		"GOVCS=*:off",
		"GOTOOLCHAIN=local",
		"CGO_ENABLED=0",
		"GONOSUMCHECK=*", "GONOSUMDB=*",
		"GOWORK=off",
		"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1",
	}
	if mc := os.Getenv("GOMODCACHE"); mc != "" {
		env = append(env, "GOMODCACHE="+mc)
	}
	untrustedEnvTotal.Inc()
	slog.Debug("goanalysis: hardened go env for untrusted root", "root", dir)
	return env
}
