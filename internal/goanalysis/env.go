package goanalysis

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
//
// reason: untrusted_root (a trust predicate is installed and rejected the root)
// | trust_unset (no predicate installed: every root is untrusted, a wiring bug
// in production).
var untrustedEnvTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "vaelor_goanalysis_untrusted_env_total",
	Help: "go commands run with the hardened env (allowlisted vars, CGO_ENABLED=0, GOPROXY without direct) because the repo root is not trusted, by reason (untrusted_root, trust_unset).",
}, []string{"reason"})

// Reasons for untrustedEnvTotal.
const (
	reasonUntrustedRoot = "untrusted_root"
	reasonTrustUnset    = "trust_unset"
)

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

// untrustedReason returns why dir gets the hardened env, or "" when it is
// trusted.
func untrustedReason(dir string) string {
	f := rootTrust.Load()
	switch {
	case f == nil:
		return reasonTrustUnset
	case (*f)(dir):
		return ""
	default:
		return reasonUntrustedRoot
	}
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

// scrubHome is an empty HOME for children of untrusted repos, so no ~/.netrc,
// ~/.gitconfig, ~/.config/go or ssh keys of the server user are visible to the
// go command, git or the compiler. It is one fixed per-uid directory, reused
// across loads and restarts, so nothing accumulates and nothing needs a
// shutdown hook. If it cannot be created the path is a non-existent one, which
// is just as empty.
func scrubHome() string {
	scrubHomeOnce.Do(func() {
		d := filepath.Join(os.TempDir(), fmt.Sprintf("vaelor-gohome-%d", os.Getuid()))
		if err := os.MkdirAll(d, 0o700); err != nil {
			slog.Warn("goanalysis: cannot create scrub HOME; using a non-existent one", "dir", d, "err", err)
			d = "/nonexistent-vaelor-home"
		}
		scrubHomeDir = d
	})
	return scrubHomeDir
}

// defaultProxy is the GOPROXY of an untrusted root when the operator set none.
const defaultProxy = "https://proxy.golang.org"

// proxyWithoutDirect returns raw (a GOPROXY value) with every "direct" entry
// removed. "direct" is what lets a repo-chosen module path make the server dial
// an arbitrary host (git/https to internal addresses); a configured proxy host
// is operator-chosen and stays. When nothing is left (unset, or direct-only) the
// fixed public proxy is used: one well-known host, not an SSRF vector, so
// untrusted repos with cold dependencies still load.
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
		return defaultProxy
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
//   - GOPROXY: the operator's proxy without the "direct" fallback, else the
//     fixed public proxy.
//   - SSL_CERT_FILE/SSL_CERT_DIR (when set): the proxy fetch must work on hosts
//     with a custom CA bundle.
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
func untrustedGoEnv(dir, reason string) []string {
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
		"GONOSUMDB=*",
		"GOWORK=off",
		"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1",
	}
	for _, k := range []string{"GOMODCACHE", "SSL_CERT_FILE", "SSL_CERT_DIR"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	untrustedEnvTotal.WithLabelValues(reason).Inc()
	slog.Debug("goanalysis: hardened go env for untrusted root", "root", dir)
	return env
}
