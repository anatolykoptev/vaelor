# ADR 0002: Environment Detect & Verify (static detection → sandboxed execution)

- **Status:** Phase 0 **Accepted** — ready to ship independently. Phase 1
  **BLOCK-UNTIL-RESOLVED** per `architecture-security-cost` review
  2026-07-04 — see "Security-Cost Review" section below for the closing
  conditions.
- **Date:** 2026-07-02 (review appended 2026-07-04)
- **Arc:** TBD (krolik-canonical plan store — plan not yet cut; this ADR is the
  design input to that plan and to the security-cost review that gates Phase 1)

## Context

go-code today is a purely *static* analyzer: tree-sitter AST
(`internal/parser`), manifest parsing (`internal/freshness/discover.go`),
persistent Apache AGE graph (`internal/codegraph`). It **never runs a target
repository's own build/test/install commands**. The one place it shells out
against a cloned repo — `goanalysis.LoadPackages` (`internal/goanalysis/loader.go:46`)
— only invokes the trusted `go` toolchain, with `context.WithTimeout` at
`loader.go:56` and `GOFLAGS`/`GOCACHE=/tmp/...`/`GOPATH=/tmp/gopath`/`GOWORK=off`
set via `goEnv` (`loader.go:43`). **Correction (security-cost review,
2026-07-04):** `goEnv` builds its env via `append(os.Environ(), …)` — it
**inherits the full process environment, including secrets** (`DATABASE_URL`,
`GITHUB_TOKEN`, `LLM_API_KEY`); it is not "sanitized," only *additively
configured*. The argument this ADR draws from it still holds — that path only
compiles trusted `go` toolchain code, it runs **no** `go:generate`, no tests, no
repo-authored scripts (modulo a pre-existing latent surface: cgo compiles
repo-authored C with that same inherited env present, out of scope here) — but
it is **not a precedent for hygienic env handling**, only for "invoke a fixed
trusted compiler, arbitrary secrets or not." Phase 1 must not repeat this
pattern: the verifier's env must be *provably* empty of go-code's secrets, not
merely additively configured. `npm` lifecycle hooks (`postinstall`), Cargo
`build.rs`, `Makefile` targets and `docker build` all execute **arbitrary
repo-authored code** — a qualitatively different threat class from "invoke a
fixed trusted compiler".

This produces a persistent gap between what go-code *statically claims* and what
it has *verified by running*. Concretely:

- `code_health` grades an A–F score across 14 sub-scores
  (`cmd/go-code/tool_code_health.go`) — but "does this repo actually build?" is
  not one of them. A repo can score well on complexity/docs/freshness and not
  compile.
- `dead_code` and other confidence-bearing signals infer reachability
  statically; a symbol the build wires in via codegen or a build tag can be
  mislabeled with no way to cross-check against a real build.
- `explore` reports detected deps and health but cannot tell an agent *how* to
  build/test the repo it just summarized — the single most common next question.

The decision drivers: (1) close the claim-vs-verified gap starting with the
cheapest, safest slice (detection); (2) do **zero** new security surface for the
detection slice; (3) make the execution slice **opt-in, off by default, and
physically unable to leak go-code's own secrets** (`DATABASE_URL`,
`GITHUB_TOKEN`, `LLM_API_KEY`, Redis creds all live in the main process env);
(4) respect the host as a hard constraint — a 4-core ARM Neoverse-N1 / 24 GiB
box that must never run heavy parallel builds (`~/AGENTS.md`).

## Considered options

### O1 — Do nothing (static-only forever)

Zero risk, zero cost, zero new surface. Rejected as the *end state*: it leaves
the claim-vs-verified gap permanently open and every downstream confidence level
stays unfalsifiable against ground truth. Kept as the honest baseline the other
options must beat.

### O2 — Phase 0 only, never do Phase 1 (detect, never execute)

Ship environment **detection** (which toolchain / which commands), never
execute. This is a *real, defensible terminal option*, not just a stepping
stone: it delivers most of the day-to-day agent value (`explore` can now answer
"how do I build/test this?") at **zero execution risk**. Honest tradeoff: it
still only *claims* the commands are correct — a `Makefile` target that no longer
exists, or a `test` script that was renamed, is reported as authoritative
without ever being run. If the security-cost review concludes the Phase 1 blast
radius is not worth it for this deployment, **O2 is the accepted resting
state**, not a failure.

### O3 — Phase 0 + Phase 1 in-process execution

Detect, then execute the detected commands **inside the go-code process** (or a
child process sharing its env). Rejected outright: any repo-authored build step
would run with `DATABASE_URL`/`GITHUB_TOKEN`/`LLM_API_KEY`/Redis creds in its
environment and could exfiltrate all of them in one `postinstall`. This is a
non-starter regardless of timeout/output caps — caps limit *blast size*, not
*secret access*.

### O4 — Phase 0 + Phase 1 execution in an isolated, minimal-privilege sidecar

**Chosen.** Detect statically (Phase 0), then execute detected commands only via
a separate execution path that owns its own Docker access, runs each command in
a locked-down ephemeral container, and is handed **nothing** from go-code's
secret env. Gated off by default. Details below.

## Decisions

### 1. Phase 0 lives in a new peer package `internal/envdetect`, pure-static

`envdetect` is a peer of `compare`/`analyze`/`callgraph` — it imports the
**public** API of `freshness` and `polyglot` and nothing of their internals, and
those packages do not import it back (mirrors the "compare/analyze/callgraph are
peers, none imports the others" rule, CLAUDE.md Conventions). It performs **zero
execution** and adds **zero new security surface** — it only reads files already
on disk in the clone.

Inputs it consumes:

- `freshness.DiscoverManifests(root) []ManifestInfo` (`discover.go:30`) — the
  already-parsed manifest set (`ManifestInfo{Language, RuntimeVersion,
  Dependencies, ManifestPath}`, `freshness.go:13`).
- `polyglot.DetectStructure(files) *RepoStructure` (`detect.go:40`) — the
  `Layer{Name, Role, RootDir, Language, Files}` grouping (`detect.go:19`), the
  existing "which directory needs which toolchain" primitive for monorepos.

**Ground-truth-first, not convention-guessing.** Critical nuance verified in
source: `freshness.ManifestInfo` does **not** carry the npm `scripts` field or
`Makefile` target names — it stops at deps + language + runtime version. So
`envdetect` must do its **own** ground-truth extraction from the raw manifest
bytes (re-reading `package.json` for its `scripts` map, scanning `Makefile` for
target lines, reading `pyproject.toml` `[tool.poetry.scripts]` /
`[project.scripts]`), and only **fall back** to per-manifest-type convention
defaults when no ground truth exists. A command sourced from the manifest is
tagged `Source: "manifest"`; a convention guess is tagged `Source: "convention"`
so callers (and Phase 1) can distinguish "the repo told us" from "we guessed".

Proposed interface (design only — no implementation in this ADR):

```go
package envdetect

// CommandSource records how a command was derived, so callers can weight
// ground-truth (manifest-declared) over convention (guessed defaults).
type CommandSource string

const (
    SourceManifest   CommandSource = "manifest"   // e.g. package.json "scripts"
    SourceConvention CommandSource = "convention" // e.g. `go build ./...`
)

// CommandKind classifies a candidate command by lifecycle phase.
type CommandKind string

const (
    KindInstall CommandKind = "install"
    KindBuild   CommandKind = "build"
    KindTest    CommandKind = "test"
    KindLint    CommandKind = "lint"
)

// Command is a single candidate command as an argv slice — NEVER a shell
// string. Phase 1 executes argv slices only (no `sh -c`), mirroring the
// fleet/ssh driver's slice-args discipline (driver.go:7-8).
type Command struct {
    Kind    CommandKind   `json:"kind"`
    Argv    []string      `json:"argv"`    // e.g. ["npm","run","build"]
    Source  CommandSource `json:"source"`
    WorkDir string        `json:"workDir"` // relative to repo root (a Layer.RootDir)
}

// Toolchain is the detected environment for one language layer.
type Toolchain struct {
    Language       string    `json:"language"`
    RuntimeVersion string    `json:"runtimeVersion,omitempty"` // from ManifestInfo
    Manager        string    `json:"manager"`                   // "npm"|"cargo"|"go"|"pip"|...
    ManifestPath   string    `json:"manifestPath"`
    WorkDir        string    `json:"workDir"`                   // Layer.RootDir
    Commands       []Command `json:"commands"`
}

// Environment is the whole-repo detection result: one Toolchain per layer.
type Environment struct {
    Toolchains []Toolchain `json:"toolchains"`
    Polyglot   bool        `json:"polyglot"`
}

// Detect is the single public entrypoint. Pure function of on-disk state:
// no network, no execution, context only for cancellation of the file walk.
func Detect(ctx context.Context, root string) (*Environment, error)
```

The manifest-type → candidate-command mapping is **table-driven** (one table
row per `Manager`, giving convention defaults per `CommandKind`), so adding a
new ecosystem is a table entry, not new control flow.

### 2. Phase 0 surfaces on `explore` (unconditional), no standalone tool initially

Decision: **fold Phase 0 output into `explore`'s existing result** rather than
minting a `detect_environment` MCP tool. Justification: `explore` already
answers "quick overview: stats, README, deps, health" — "how do I build/test
this" is the same question class and the same call the agent already makes.
A standalone tool would force a second round-trip for information the overview
call already has the clone in hand to compute. Because detection is pure-static
and cheap (a file walk over already-enumerated files), it runs **every** time,
unconditionally — no flag, no cold-path guard needed (there is nothing unsafe or
slow to guard). The new `<environment>` block is **additive** to `explore`
output; existing consumers ignoring it see no behavioral change. A standalone
`detect_environment` tool remains a trivial follow-up (thin wrapper over
`envdetect.Detect`) if a caller ever needs it without the rest of `explore`.

### 3. Phase 1 = `verify_environment`, opt-in via `GOCODE_VERIFY_ENABLE`, sandbox-only

Phase 1 actually *runs* the Phase-0-detected (or caller-supplied) commands.
Design commitments, each mirroring an existing hardened pattern:

- **Disabled by default.** Flag name `GOCODE_VERIFY_ENABLE` (mirrors
  `GOCODE_FLEET_SSH_ENABLE`, `ssh/driver.go:35`). Unset/`false` → the tool
  returns "verification disabled" and, crucially, `code_health` output is
  **byte-identical** to today (decision 6).
- **Never exec inside the go-code process.** The go-code process holds
  `DATABASE_URL`, `GITHUB_TOKEN`, `LLM_API_KEY`, Redis creds. A repo-authored
  build step must never observe that env (rejected option O3). Execution is
  delegated to a **separate execution path that owns its own Docker socket
  access** — distinct from `internal/fleet/docker` (which is read-only:
  `docker ps`-equivalent listing, *not* execution). Running arbitrary
  containers is a materially larger privilege than listing them and gets its
  own component and its own socket, never shared with the listing driver.
- **Each command runs in an ephemeral, locked-down container.** For every
  `Command`, the sidecar issues one `docker run --rm` with, at minimum:
  `--network=none` (build/test/lint steps) or a registry-only egress profile
  (install steps — see Open Questions); `--read-only` rootfs + a `tmpfs`
  workdir; `--cap-drop=ALL`; `--security-opt=no-new-privileges`;
  `--pids-limit`; `--memory` / `--cpus` caps; a non-root user; and a hard
  wall-clock timeout (`context.WithTimeout`, matching `healthBuildTimeout`
  discipline, `tool_code_health.go:28`). Output is capped exactly like the ssh
  driver's `cappedWriter` (1 MiB stdout / 64 KiB stderr, cancel-on-overflow,
  `driver.go:64-100`); stderr is not surfaced verbatim to the LLM prompt.
  Commands are passed as **argv slices only** — never `sh -c` / shell-string
  composition (`driver.go:7-8`).

### 4. go-code ↔ sidecar boundary: a **separate service over the network**, not in-process

Two shapes were considered:

- **(a) in-process package** that shells `docker run` from within go-code, and
- **(b) a separate minimal-privilege HTTP service** go-code talks to over the
  network.

**Chosen: (b), a separate service.** Justification grounded in the existing
architecture: go-code *already* treats privileged/heavy capabilities as separate
services reached over the network — the embedding server (`EMBED_URL`), the
Postgres/AGE store (`DATABASE_URL`), go-search (`GO_SEARCH_URL`). Adding a
"verifier" service is the same seam, and it is the seam that actually delivers
the security property: **only the verifier service has the Docker socket
mounted; the go-code container does not.** With (a), go-code's own container
would need `/var/run/docker.sock`, and Docker-socket access is
root-equivalent on the host — that would make a go-code RCE a host compromise.
With (b), a go-code compromise cannot start containers at all; it can only send
a verify *request* to a narrow API. The verifier holds **no** go-code secrets;
its env is empty of `DATABASE_URL`/`GITHUB_TOKEN`/`LLM_API_KEY`. Interface:
go-code POSTs `{argv, workDir, image, limits}` for one command; the verifier
returns `{exitCode, cappedStdout, cappedStderr, durationMs, killedReason}`.
Gated behind a new `GOCODE_VERIFY_URL` (empty ⇒ Phase 1 unavailable even if the
flag is on — belt-and-braces).

### 5. Concurrency & resource budget: per-repo dedup + global semaphore of 1 + preflight gate

Three layers, all required (host is 4-core / 24 GiB, no heavy parallel builds —
`~/AGENTS.md`):

1. **Per-repo dedup** via a `sync.Map` keyed by `repoKey` — the exact
   `buildingHealth` pattern (`tool_code_health.go:24`, `LoadOrStore`), so two
   concurrent verify requests for the same repo collapse to one job and the
   second gets the poll-for-result response.
2. **Global semaphore = 1** across the *whole service* — at most one
   verification job runs anywhere at a time, on top of per-repo dedup. This is
   a hard rule from `~/AGENTS.md`, not a tunable; a heavier limit is not offered.
3. **Preflight gate** before starting any job, mirroring `~/bin/cargo-load`
   semantics: read available memory + PSI `memory some avg10`; emit a verdict of
   `REFUSE` (available < 3 GiB **or** PSI avg10 > 20), `QUEUE-OK`, or `SAFE`
   *before* the container is started. `REFUSE` returns a "host under pressure,
   retry later" response rather than starting the build. The preflight check
   runs in the verifier service (it is the component that knows host pressure),
   not in go-code.

The slow-job response shape reuses `code_health`'s async convention exactly:
`<status>computing</status>` + "retry in 60s", background goroutine via a
`spawnHealthBuild`-shaped helper (`tool_code_health.go:40`), cleanup via `defer`
on **every** exit path.

### 6. Clone lifecycle: dedicated ephemeral clone, sole-ownership, no shared checkout

Verification reuses `internal/ingest`'s partial-clone-into-`WORKSPACE_DIR`
(`--filter=blob:none`) + the `resolveRoot`/`cleanup` pair already used by
`code_health` (`tool_code_health.go:91`). It **never** executes against a
shared/cached checkout — each verify job gets its own clone that the background
worker **solely owns**, and `cleanup` deletes it via `defer` **only after** the
worker has finished (and, for Phase 1, only after the sidecar has finished
reading the mounted tree). This is the same **use-after-delete race** that was
already fixed once for `code_health`'s background path
(`spawnHealthBuild`'s doc comment, `tool_code_health.go:30-46`,
`cleanupTransferred` guard `tool_code_health.go:95-106`): the handler must
**transfer clone ownership** to the worker instead of running `cleanup` when it
returns its synchronous "computing" response. The new tool adopts that exact
`cleanupTransferred`-flag structure so the race is not reintroduced. For Phase 1
the clone is bind-mounted **read-only** into the container wherever possible;
the writable workdir is a container `tmpfs`, so a build cannot mutate the
host-side clone.

### 7. Caching: keyed by `repoKey` + git SHA, mirroring the health cache

Verification results are cached exactly like `code_health`
(`LoadHealthCache`/`EnsureHealthCacheTable`/`UpsertHealthCache`,
`tool_code_health.go:112-113,272`), keyed by `repoKey =
codegraph.GraphNameFor(root)` **plus the git SHA** — so a new commit
invalidates the cache (a build result is only meaningful for the exact tree it
ran against, unlike a health score which is TTL-bounded). TTL still applies
(`GRAPH_TTL_LOCAL`/`GRAPH_TTL_REMOTE` semantics) as a secondary bound.

### 8. `code_health` gains a 15th sub-score `buildable`, gated + cold-path-identical

When `GOCODE_VERIFY_ENABLE` is **on**, `code_health` gains one sub-score:
`buildable: verified | unverified | failed`. When the flag is **off** (default),
the sub-score is **not emitted at all** and `code_health` output is
**byte-identical** to today — matching the documented **cold-path guarantee**
("every consumer skips enrichment when the signal is absent, preserving
byte-identical output", CLAUDE.md "Graph signal ecosystem"). The `buildable`
field is read from the verification cache (decision 7) only; `code_health` never
*triggers* a build itself (that would blow its own timeout and violate the
global-semaphore-of-1 rule under concurrent health calls). It reports
`unverified` when no cached result exists, `verified`/`failed` when one does.

## Consequences

- Phase 0 ships **independently and first** — pure-static, no flag, no new
  security surface, additive `explore` output. It is valuable on its own and is
  the accepted resting state if Phase 1 is never enabled (option O2).
- Phase 1 ships **behind `GOCODE_VERIFY_ENABLE` (default off)** plus
  `GOCODE_VERIFY_URL`, opt-in **per deployment**. With the flag off, the fleet
  sees zero behavioral change and zero new attack surface.
- The verifier is a **new deployable component** (a container with the Docker
  socket) — an operational cost and a new privileged surface that the
  security-cost review must sign off on before implementation.
- `envdetect` respects the peer-package boundary; the only new inter-package
  edges are `envdetect → freshness` (public) and `envdetect → polyglot`
  (public).

## Alternatives considered and rejected

- **In-process / same-env execution (O3)** — rejected (decision 3/4): exposes
  all go-code secrets to arbitrary repo-authored build scripts; output caps
  don't mitigate secret access.
- **In-process `docker run` shell-out from go-code (decision 4a)** — rejected:
  requires mounting the Docker socket into the go-code container, making a
  go-code RCE a host compromise. The separate-service seam (4b) is the property
  that isolates socket access.
- **Reuse `internal/fleet/docker` for execution** — rejected: that driver is
  read-only listing; running arbitrary containers is a materially larger
  privilege and gets its own component and socket.
- **Standalone `detect_environment` tool as the primary surface** — deferred
  (decision 2): folding into `explore` avoids a redundant round-trip; the
  standalone tool remains a trivial follow-up.
- **Global concurrency > 1 / tunable** — rejected (decision 5): the host is a
  4-core box with a hard no-heavy-parallel-builds rule; 1 is a constraint, not
  a knob.
- **Trust `freshness.ManifestInfo` for command discovery** — rejected
  (decision 1): it carries no `scripts`/target data, so `envdetect` must do its
  own ground-truth extraction or it would be *all* convention-guessing.

## Open Questions / Risks for security review

These are **explicitly not resolved** in this ADR and are handed to the
`architecture-security-cost` review to react to:

1. **Exact sidecar deployment topology.** Same host vs. a dedicated
   throwaway/microVM host; docker-in-docker vs. a mounted host socket vs.
   rootless Docker / gVisor / Kata. The choice materially changes blast radius
   and is deferred to security review.
2. **Network egress allowlist for install steps.** `--network=none` is safe for
   build/test/lint, but `install` steps (`npm install`, `cargo fetch`, `pip
   install`) need registry egress. What is the exact allowlist — and do we need
   **registry mirrors/proxies** (npm/crates.io/PyPI) to avoid arbitrary
   outbound network from within a container running untrusted `postinstall`
   code? Open.
3. **Blast radius if the verifier's Docker socket is compromised.** Docker
   socket access is root-equivalent on its host. What contains a verifier
   compromise — a dedicated host, a VM boundary, seccomp/AppArmor profiles on
   the verifier itself? Open.
4. **Secret / env leakage paths.** Confirm the verifier's env is provably empty
   of go-code secrets; confirm no secret rides in via build args, mounted
   files, or the request payload; confirm capped stderr cannot smuggle secrets
   back into the LLM prompt. Open.
5. **Malicious repo path / symlink-to-host-path abuse.** A caller-supplied
   `repo` that is actually a **symlink or a local host path** rather than a
   fresh clone could point the mount at arbitrary host files. What validates
   that the mounted tree is the ephemeral clone and nothing else (realpath
   containment check under `WORKSPACE_DIR`, symlink rejection, `PATH_MAPPINGS`
   interaction)? Open.
6. **Caller-supplied commands vs. detected-only.** Phase 1 allows a
   caller-supplied command as well as the Phase-0-detected one. Should
   caller-supplied argv be allowed at all, or restricted to the detected set
   (`Source: "manifest"`) to shrink the abuse surface? Open.
7. **Base-image selection & supply chain.** Which images run the commands, who
   pins/updates them, and what happens for a language/version with no pinned
   image? Open.
8. **Resource-exhaustion / DoS via many distinct repos.** Per-repo dedup +
   global-semaphore-of-1 + preflight bound concurrency, but a flood of *distinct*
   repoKeys could still queue unboundedly. Is a queue depth cap / rate limit
   needed? Open.

## Security-Cost Review (2026-07-04) — Verdict: BLOCK-UNTIL-RESOLVED (Phase 1)

Reviewed by `architecture-security-cost` against the AWS Well-Architected
Security/Cost/Reliability lenses. **Phase 0: PASS, ship independently now.**
**Phase 1 as specced: not yet buildable.** Full point-by-point findings live in
the review transcript; summary below.

**Core finding:** decisions 3-4 correctly harden *secret* isolation (go-code's
process never touches `docker.sock`) but the ADR under-specifies the boundary
that actually matters for Phase 1's purpose — running **actively-adversarial,
repo-authored code** (`npm postinstall`/`build.rs`/`Makefile` targets) on a
**shared** 4-core/24GB host that also runs postgres, redis, and 15+ other MCP
services. A hardened `runc` container (`--cap-drop=ALL`, `--read-only`,
`--network=none`, non-root) is a resource boundary, not a trust boundary,
against actively hostile code — see runc/cgroup escape CVEs
(CVE-2019-5736, CVE-2022-0492, CVE-2024-21626). An escape from Phase 1's sandbox
lands directly on the fleet's shared kernel.

**Blocking conditions to close before implementation starts:**

1. **Isolation runtime ≥ gVisor (`runsc`) + user-namespace remap** — no bare
   `runc`. (Target: Firecracker/Kata microVM-per-job once justified.)
2. **Verifier HTTP API must be authenticated and bound to loopback/private
   network only.** The ADR's decision 4 never specifies this — an
   unauthenticated `GOCODE_VERIFY_URL` reachable from the shared host's network
   is strictly *worse* than mounting the socket into go-code itself, because it
   becomes "docker-run-as-a-service" for every co-located process.
3. **Provably secret-free verifier env** — an explicit allowlist at process
   launch, not `append(os.Environ(), …)` inheritance (see the `loader.go`
   correction above for the anti-pattern to avoid repeating); capped
   stdout/stderr treated as untrusted and never forwarded verbatim into an LLM
   prompt.
4. **Symlink/path containment**: reject any mount target that isn't a fresh
   clone under `WORKSPACE_DIR` (realpath check, reject symlinks, no
   `PATH_MAPPINGS`-resolved host paths).
5. **v1 scope cuts**: no `install`-class execution at all (registry egress +
   `postinstall` is the worst combination — defer behind a pull-through
   registry proxy in v2); **detected-only argv**, no caller-supplied commands;
   no `docker build` of repo Dockerfiles; base images **pinned by digest**,
   refuse (`buildable: unverified`) rather than fall back to `:latest` when a
   language/version has no pinned image.
6. **Sized `tmpfs`** for the container workdir (unbounded tmpfs on a shared
   host is a memory-DoS vector the preflight PSI gate cannot catch in time) +
   a bounded job queue with fast-fail rather than unbounded queueing.

**Secondary findings (non-blocking, track separately):** `buildable: verified`
reflects an attacker-controlled exit code (a `Makefile` `test:` target can
`exit 0` unconditionally) — do not let it silently upgrade other confidence
signals elsewhere in `code_health`/`dead_code`. The new verifier deployable
(runtime patch cadence, pinned-image maintenance, standing CVE surveillance) is
a sticky operational cost the ADR's "Consequences" section under-weights
relative to the (real, correctly-claimed) two-way-door env-flag kill switch.

**Disposition:** O2 (Phase 0 only) is the accepted default until items 1-6
above have concrete, implemented answers. O4 remains the target architecture
and is fine to build once they close — this is a sequencing gate, not a
rejection of the design.
