# ADR 0002: Environment Detect & Verify (static detection → sandboxed execution)

- **Status:** Phase 0 **Accepted** — shipped (PR #296). Phase 1 design went
  through an original `architecture-security-cost` review (2026-07-04,
  BLOCK-UNTIL-RESOLVED, 6 conditions) and three resolution/re-review rounds
  (2026-07-06): round 1 closed 4/6 conditions outright and fixed the other 2
  (verifier network binding, clone-path TOCTOU) plus 3 cheap must-fixes; round
  2 (narrow, items 2+4 only) confirmed those fixes closed their original
  findings but surfaced a **new coupling gap** between them (the verifier's
  clone step needs network egress that its `internal: true` API network
  can't provide, and the clone was running inside the socket-holding
  verifier process); round 3 (narrow, the coupling fix only) reviewed the fix
  — making the clone step itself a sandboxed job on a separate egress-only
  network, reusing decision 1's isolation primitive — and returned
  **RESOLVED, ready for implementer dispatch**, with two implementation-time
  (non-blocking) notes folded in (`--rm` vs. the `docker cp` handoff timing;
  `--read-only` parity on the clone job). **All six original conditions plus
  the emergent coupling gap are now closed across this review chain. The
  formal BLOCK-UNTIL-RESOLVED verdict was never re-issued by a human/operator
  sign-off step — treat Phase 1 as design-complete and reviewed, but get an
  explicit go/no-go before dispatching an implementer, given the security
  sensitivity of this feature.**
- **Date:** 2026-07-02 (review appended 2026-07-04; three resolution/re-review
  rounds appended 2026-07-06)
- **Arc:** TBD (canonical plan store — plan not yet cut; this ADR is the
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
flag is on — belt-and-braces). **Payload revised in the Phase 1 Design
Resolution below (item 4, 2026-07-06 re-review fix):** the request carries
`{repoURL, gitSHA, argv, workDir, image, limits}` — a repo reference, not a
filesystem path — so the verifier clones the tree itself instead of trusting a
path go-code hands it. The response shape is unchanged.

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

## Phase 1 Design Resolution (design-only, no code)

This section closes the six BLOCK-UNTIL-RESOLVED conditions from the
Security-Cost Review with concrete, implementable decisions. It is **design
only** — no Go was written, no execution code exists, `internal/envdetect` is
untouched. Each item states the decision, the justification (with web citations
for the isolation-runtime claims, gathered 2026-07-06), and what is explicitly
left to implementation time. **The verdict is not changed here** — this is the
input to a follow-up security-cost re-review.

**Deployment ground truth used throughout.** The deploy target is the
deploy-config box: an Oracle Cloud **`an ARM VM`** free-tier shape —
ARM, **4 vCPU / 24 GiB**, running the single
`~/deploy/deploy-config/` docker-compose stack alongside postgres, redis, and
15+ MCP services. It is a **virtual machine, not bare metal**. This single fact
(a VM, on ARM, with no nested virtualization — see item 1) drives the runtime
choice.

### 1. Isolation runtime

**Decision (v1): gVisor (`runsc`) on the `systrap` platform + Docker
`userns-remap`. Firecracker is deferred — it is not viable on this box.**

**Why not Firecracker on this box.** Firecracker is GA on `aarch64` and boots a
microVM to `/sbin/init` in `<= 125 ms` with a `< 5 MiB` VMM memory overhead,
via the `jailer` (Firecracker spec/site, firecracker-microvm.github.io;
`SPECIFICATION.md`). That would be an excellent per-job trust boundary. **But
Firecracker requires `/dev/kvm` in the environment it runs in, and Oracle Cloud
Ampere A1 *VM* shapes do not expose nested virtualization / KVM.** Oracle's own
docs and engineering blog are explicit: "Ampere VMs (ARM/aarch64) did not
support nested virtualization" (blogs.oracle.com/linux, *KVM Nested
Virtualization in OCI*); "Running Firecracker inside an OCI VM requires nested
virtualization so that `/dev/kvm` is available inside the guest … If `/dev/kvm`
isn't available on your shape, use BM for Firecracker"
(blogs.oracle.com/cloud-infrastructure, *Firecracker on OCI*); and a free-tier
practitioner confirms "Oracle's Ampere A1 instances do not support nested
virtualization or KVM" (dev.to, *How I Secured an Autonomous AI Agent on
Oracle's Free Tier*). Firecracker therefore **cannot run on
`an ARM VM`** at all. It becomes the target only if verify is ever
moved to a dedicated `BM.Standard.A1.160` bare-metal host — recorded as the v2
"target architecture" the review already named, gated on that hardware move.

**Why gVisor works here and which platform.** gVisor officially supports ARM64:
"gVisor supports x86_64/AMD64 and ARM64 processors" and ships `arm64` apt
packages (gvisor.dev FAQ + Install). ARM64 syscall coverage is mature but not
total — the compatibility table lists **240 of 294 syscalls** with full or
partial support, **54 unsupported** (gvisor.dev/docs/user_guide/compatibility/
linux/arm64). The practical consequence is a **soft** one: most runtimes fall
back to a supported syscall when one is missing (per the same page's preamble),
so a build/test that fails only under `runsc` is treated as a verify
*inconclusive* signal, not a hard `failed` (see the secondary-finding guard in
the review — an exit code under gVisor is not authoritative). Two ARM64-specific
gVisor gaps we accept as out-of-scope for v1: CPU-feature masking for
checkpoint/restore is unimplemented on ARM64 (google/gvisor commit 93333d2) —
irrelevant, we never checkpoint verify jobs; and the **KVM platform is not
available on ARM CPUs** ("not supported by all hardware (such as ARM CPUs)",
gvisor.dev/blog/2023/04/28 *Releasing Systrap*). The last point is decisive: it
forces the **`systrap`** platform, which is exactly right for a VM — systrap is
the **default platform since mid-2023**, uses `seccomp` `SECCOMP_RET_TRAP` +
`SIGSYS` interception, needs **no virtualization**, and "will often yield better
performance than KVM" inside a VM (gvisor.dev/docs/user_guide/platforms +
systrap release blog). `--platform=kvm` would need nested virt this box does not
have, so it is not an option regardless.

**Concrete invocation.**

`/etc/docker/daemon.json` on the verifier host (or the verifier's own dockerd if
DinD is chosen at implementation time):

```json
{
  "runtimes": {
    "runsc": { "path": "/usr/local/bin/runsc", "runtimeArgs": ["--platform=systrap"] }
  },
  "userns-remap": "default"
}
```

`userns-remap: "default"` creates the `dockremap` user and maps container UID 0
to a high, unprivileged host subordinate UID via `/etc/subuid`/`/etc/subgid`, so
a container "root" that somehow crosses the namespace boundary is an
unprivileged nobody on the host (Docker docs, *Isolate containers with a user
namespace*). This is defense-in-depth *underneath* gVisor's syscall boundary,
per the review's "gVisor + userns-remap minimum."

Per-command container launch (argv slice, never `sh -c`):

```
docker run --rm --runtime=runsc \
  --network=none \
  --read-only \
  --tmpfs /work:rw,size=2g,mode=1777,nr_inodes=262144 \
  --cap-drop=ALL --security-opt=no-new-privileges \
  --pids-limit=512 --memory=4g --memory-swap=4g --cpus=2 \
  --user 65534:65534 \
  -v <cloneRealpath>:/src:ro -w /work \
  <image@sha256:...> <argv...>
```

**Startup-latency budget.** The relevant number is gVisor sandbox-creation
overhead over `runc`, which is on the order of **~100–300 ms** per container in
practice; a public at-scale benchmark that isolated the sandbox cost found
gVisor-ARM pod startup essentially **on par with runc-ARM** once an unrelated
Istio-sidecar confound was removed (google/gvisor issue #13395: "Without Istio,
gVisor ARM and x86 are nearly identical … The ARM 'slowness' disappears
entirely"). Against the existing **5-minute total** `healthBuild`-style timeout
budget (`tool_code_health.go:28`), a sub-second sandbox-init tax is
negligible; systrap adds per-syscall overhead to the *build itself*, which is
bounded by the same wall-clock timeout. **No warm pool is needed** — jobs are
`docker run --rm` per verify call, matching the ephemeral-per-job requirement.

**Deferred to implementation time:** exact `runsc` release channel pin (use the
`release` channel, not `nightly`); whether the verifier runs its own dockerd
(DinD) vs. a dedicated host dockerd — both are compatible with this runtime
choice and the topology decision (item 2) is orthogonal; measuring the real
systrap per-job overhead on this specific box and recording it next to the
timeout constant.

### 2. Verifier API authentication

**Decision: shared bearer token (`GOCODE_VERIFY_TOKEN`) compared in constant
time, sent as `Authorization: Bearer <token>`, on a listener bound to the
private docker-compose network with no published host port. mTLS is
deliberately *not* adopted for v1.**

Mechanism, three layers:

1. **Bearer token.** The verifier requires `GOCODE_VERIFY_TOKEN` at boot; if
   unset or empty it **refuses to start** (fail loud — same discipline as item
   3). Every request must carry `Authorization: Bearer <token>`; the verifier
   validates with a **constant-time** comparison (`crypto/subtle.
   ConstantTimeCompare` over fixed-length SHA-256 digests of the presented and
   expected tokens, so length is not a side channel) and returns `401` with no
   body detail on mismatch. go-code reads the same value and sends it on every
   POST via `http.NewRequestWithContext` (per the repo convention). The token is
   a go-code↔verifier shared secret and is **not** on the verifier's env
   deny-list (item 3) — it is the one credential the verifier legitimately owns.
2. **Network binding — revised after re-review (2026-07-06): a dedicated,
   two-peer network, not the shared project network.** The first draft of this
   decision bound the verifier to "the compose internal network" without
   specifying *which* — on host-a's actual compose stack that is the single
   project-wide network shared by postgres, redis, and 15+ other MCP services
   (`~/AGENTS.md` ports table), so "no published host port" stopped an
   off-host attacker but left the verifier reachable **by service alias from
   every one of those ~25 co-located containers** — exactly the "co-located
   compromised container" adversary this ADR names, with the bearer token as
   the *only* remaining barrier instead of defense-in-depth. **Revised
   decision:** go-code and the verifier are attached to their own **dedicated
   compose network with `internal: true` and no other service attached** —
   e.g. a `gocode-verify` network in `~/deploy/deploy-config/compose/*.yml`
   listing only these two services. `internal: true` additionally blocks the
   network from routing to the outside world at all (Docker Compose networking
   docs — internal networks have no default gateway), which is a free
   corollary of decision 6's "install is out of scope, most job containers get
   `--network=none` anyway" but is set at the *verifier's own* network too,
   not just the job sandbox's. **This network carries only the go-code↔verifier
   API traffic — the verifier's per-job clone step needs its own egress and is
   deliberately kept on a separate `gocode-verify-egress` network instead,
   attached only to the ephemeral clone container, never to the verifier's own
   listener; see item 4's revised mechanism for why these must not be the same
   network.** This shrinks the reachable-peer set from ~25 to
   exactly 2, so network segmentation and the bearer token are now two
   independent layers, matching the original "auth AND isolation" intent
   rather than collapsing to auth-only.
3. **Belt-and-braces.** Empty `GOCODE_VERIFY_URL` ⇒ Phase 1 unavailable even
   with the enable flag on (already in decision 4). Empty token on the go-code
   side ⇒ it never sends a request.

**Why not mTLS (justified against the topology, as revised).** The threat mTLS
defends — network-level impersonation/MITM of the verifier by an off-host or
on-wire attacker — is not present here: both peers are containers on the
**same host**, on the **dedicated two-peer internal network** above, with **no
host-published verifier port** and no third container able to reach it at all.
The realistic remaining adversary is *one of these two peers itself* being
compromised, and against that neither mTLS nor a bearer token adds anything
(a compromised go-code already holds the token). mTLS would add
cert-issuance/rotation operational cost for no additional protection on this
seam. This is a **two-way door**: if verify is ever exposed beyond this
two-peer network, or moved cross-host, mTLS is a clean additive upgrade (add a
client cert check alongside the bearer check) and should be adopted then.
Recorded as a reversible decision, not a permanent one.

**Deferred to implementation time:** token length/generation (recommend ≥32
bytes from a CSPRNG, provisioned via `~/deploy/deploy-config/.env`); exact
compose service alias and port (next free per `~/AGENTS.md`: `8910+`).

### 3. Provably secret-free verifier env

**Decision: an allowlist-only process env plus a fail-loud deny-list self-check
at boot; the container's env is built from an explicit slice (never
`os.Environ()`); the request payload is confirmed structurally incapable of
carrying a secret.**

Three guarantees:

1. **Boot-time deny-list self-check (fail loud).** Before the verifier binds its
   listener, it scans its own process environment against a named deny-list and
   **refuses to start** (log a `critical` line + non-zero exit) if *any* is
   present and non-empty:

   ```
   DATABASE_URL, LEARNINGS_DATABASE_URL, REDIS_URL,
   GITHUB_TOKEN, GITLAB_TOKEN, GITHUB_WEBHOOK_SECRET,
   LLM_API_KEY, LLM_API_KEY_FALLBACK
   ```

   This is the exact go-code secret set enumerated in CLAUDE.md's env table.
   Fail-loud (refuse to boot), not fail-silent, is the point: a careless
   `docker-compose` edit that leaks a secret into the verifier makes the
   verifier **crash on deploy**, which is loud and caught immediately, rather
   than running with a secret quietly in scope.
2. **Allowlist-only as the stronger floor.** In addition to the deny-list, the
   verifier treats its expected env as an **allowlist** — `GOCODE_VERIFY_TOKEN`,
   `GOCODE_VERIFY_PORT`, and standard runtime vars (`PATH`, `HOME`, `HOSTNAME`,
   `TZ`, container/locale basics). Any env var **not** on the allowlist whose
   name matches a secret-shaped pattern
   (`/_(TOKEN|KEY|SECRET|PASSWORD|DSN|CREDENTIALS?)$/i` or ending `_URL` with a
   credential-bearing scheme) also triggers refuse-to-boot. The deny-list is the
   guaranteed-known floor; the pattern guard catches *future* secrets nobody
   remembered to add to the deny-list. This directly answers the review's "an
   explicit allowlist at process launch, not `append(os.Environ(), …)`
   inheritance."
3. **Container env built from scratch.** When the verifier launches a job
   container it constructs the container's env as an **explicit, minimal slice**
   (only what the toolchain image needs, e.g. `HOME=/work`, `PATH`,
   language-specific cache dirs pointing at the tmpfs) — it **never** does
   `append(os.Environ(), …)`. This is the precise anti-pattern the ADR's Context
   correction flagged in `goanalysis/loader.go:43` ("inherits the full process
   environment, including secrets … not a precedent for hygienic env handling").
   Even if guarantee 1/2 were bypassed, the container still would not inherit the
   verifier's env.

**Request payload carries no secret — confirmed.** The go-code→verifier request
is `{repoURL, gitSHA, argv, workDir, image, limits}` (decision 4, as revised
below to a repo reference rather than a path). None of these is
secret-shaped: `repoURL`/`gitSHA` identify a public or already-authenticated
clone target the same way go-code's own `internal/ingest` clone does today,
`argv` is a detected command slice (item 5 forbids caller-supplied argv),
`workDir` is a repo-relative path, `image` is a pinned digest, `limits` are
integers. The verifier **rejects** any request whose JSON
carries an unexpected field — specifically any `env`, `secrets`, `mounts`,
`token`, or `dockerArgs` key — with `400`, so a future caller cannot smuggle env
into a job through the API. **Mechanism, named (2026-07-06 re-review fix):**
Go's default `json.Unmarshal`/`Decoder.Decode` silently *ignores* unknown
fields, which would make the "rejects" claim above false as stated — the
verifier's request decoder must call
`json.NewDecoder(r.Body).DisallowUnknownFields()` before `Decode` (stdlib
`encoding/json`, same strict-decoding pattern Kubernetes uses for its
`UniversalDeserializer`) so an unrecognized key is a hard decode error, not a
silent no-op. This holds by construction: there is no legitimate reason for a
secret to appear in a verify request, so the schema simply has no field for
one and unknown fields are rejected rather than ignored.

**Untrusted output.** Per the review, capped stdout/stderr are treated as
attacker-controlled: they are size-capped exactly like the ssh driver's
`cappedWriter` (1 MiB / 64 KiB), and **never forwarded verbatim into an LLM
prompt** — `code_health`'s `buildable` sub-score consumes only the structured
`{exitCode, durationMs, killedReason}`, not the raw output text.

**Deferred to implementation time:** the exact allowlist of benign runtime vars
(kept minimal, reviewed at code time); whether the deny-list is a literal
constant or read from a shared config — recommend a literal constant so it
cannot be weakened by env.

### 4. Symlink / path containment

**Decision — revised after re-review (2026-07-06): the verifier clones the
repo itself, into a directory only it ever writes to. go-code never hands the
verifier a filesystem path at all, which removes the shared-write surface the
original design's symlink/containment check was trying to defend, rather than
just checking it more carefully.**

**Why the first draft was insufficient.** The original decision had go-code
clone into `WORKSPACE_DIR` and pass that path to the verifier, which
re-validated it (`EvalSymlinks` + per-component `Lstat` + `filepath.Rel`
containment) before `docker run -v`. Re-review identified this as a
**time-of-check-to-time-of-use (TOCTOU) gap**: the check validates a path
string/inode at check-time, but the actual mount happens later, by the Docker
daemon, at use-time. Between the two, anything with write access to that path
(and the ADR's own named adversary is *go-code itself*, compromised) could swap
the target — e.g. replace the directory with a symlink to `/` or
`/var/run/docker.sock` after the verifier's check but before the `docker run`
call resolves `-v` again internally. A string-level check cannot close a race
against the very party the check exists to distrust. Deferring the actual fix
(`openat2(RESOLVE_BENEATH)` / mount-by-fd) to "implementation time, nice to
have" was backwards: it is the fix for the *primary* threat, not a polish.

**Revised decision: eliminate the shared mutable path instead of racing to
validate it.** go-code's request to the verifier carries `{repoURL, gitSHA,
argv, workDir, image, limits}` — a **repo reference**, never a filesystem path.
The verifier performs its **own** clone (the same shallow/partial-clone
technique `internal/ingest.CloneRepo` already uses, `--filter=blob:none`) into
a fresh, per-job directory created immediately before the job and destroyed
immediately after, under a directory tree **only the verifier process ever
writes to** — go-code has no write access to it and no other job or process
shares it. Consequences:

- **No TOCTOU window exists**, because there is no second writer: the verifier
  clones the tree and mounts the tree it just cloned, in the same process,
  with nothing else in a position to swap it in between. The symlink/realpath
  checks from the original draft are **kept as defense-in-depth** against a
  malicious *repo* (a repo whose own working tree contains a symlink to
  `/etc`), not as the primary defense — that job now falls to "we cloned it,
  nobody else touched it."
- **go-code's own clone (in `WORKSPACE_DIR`, used by every other tool) is
  completely uninvolved in verify** — decision 4's original "refuse
  `PATH_MAPPINGS`/local-checkout inputs" still holds (the verifier never
  accepts anything but a `repoURL`+`gitSHA` it resolves itself), and now
  additionally go-code's *own* clone of the same repo (which it needed anyway,
  to run `envdetect.Detect` and produce the candidate `argv`) is never handed
  across the trust boundary either — only the reference is.
- **Cost:** one redundant clone per verify job (go-code's for detection,
  the verifier's own for execution). Both use the same `--filter=blob:none`
  partial-clone technique, so the marginal cost is bounded and is the price of
  removing the shared-write surface entirely rather than policing it.

**Second re-review (2026-07-06) finding, folded in: the clone step itself must
not run bare inside the verifier's own process.** A second narrow re-review of
this revision correctly flagged that "the verifier clones the repo" as first
written meant the verifier's **own process** — the sole holder of the Docker
socket — runs `git clone`/`checkout` directly against untrusted repo content.
A code-exec vector in that path (`.gitattributes` clean/smudge filters,
submodule path-traversal classes like CVE-2024-32002 — hooks alone are not
the only untrusted-git-content RCE surface) executing inside the
socket-holding process is a host compromise: exactly the catastrophe this
whole ADR exists to prevent, now relocated rather than removed. "Disable
hooks" alone (the first draft's mitigation) does not cover those other
vectors, and — per the review — deferring the real fix to "implementation
time, nice to have" repeats the *same* anti-pattern this ADR already rejected
once for the `openat2` deferral.

**Revised mechanism: the clone is itself a sandboxed job, using the exact same
primitive as every build/test/lint job (decision 1), not a bare git
invocation in the verifier's process.** The verifier's own process never
executes `git`; it only ever asks Docker to run containers, which is already
its full job. Sequence:

1. go-code resolves the repo to a concrete `gitSHA` (it already has the clone
   and the ref for `envdetect`/`explore`) and sends `{repoURL, gitSHA, argv,
   workDir, image, limits}` — no path.
2. The verifier launches a **clone job**: `docker run --runtime=runsc
   --read-only --cap-drop=ALL --security-opt=no-new-privileges
   --pids-limit=64 --memory=512m --user 65534:65534
   --tmpfs /clone:rw,size=1g <thin-git-image> git clone --filter=blob:none
   -c core.hooksPath=/dev/null --no-checkout <repoURL> /clone && git -C /clone
   checkout <gitSHA>` — `--read-only` added for parity with decision 1's
   build job (which already has it); **no `--rm`** here specifically, because
   step 4's handoff needs to read the container's `/clone` tmpfs *after* it
   exits (`--rm` auto-destroys both the container and its tmpfs on exit,
   which would race the handoff) — the verifier explicitly `docker rm`s the
   clone container itself once the handoff (step 4) has finished reading it,
   the same "cleanup after read, not on synchronous return" discipline
   `spawnHealthBuild` already uses. The **same** gVisor
   `systrap` + userns-remap + cap-drop sandbox as decision 1, just running
   `git` instead of the target repo's build command. A clone-time RCE is now
   contained by the identical sandbox boundary that already contains a
   build-time RCE — no new trust tier, no new mechanism to get wrong.
3. This clone job is the **only** container in the whole design that needs
   network egress. It attaches to a **second, dedicated network** —
   `gocode-verify-egress` — separate from the `gocode-verify` internal
   two-peer network the verifier's own HTTP listener binds to (item 2). The
   verifier's long-lived process and its API port are **never** attached to
   `gocode-verify-egress`; only the short-lived, per-job clone container is.
   This is what actually reconciles item 2 (the verifier's API network has no
   gateway/egress, by design) with item 4 (cloning needs egress): egress is
   scoped to the ephemeral clone container alone, never to the persistent,
   socket-holding, API-serving verifier process. `gocode-verify-egress` can be
   a plain bridge network with normal internet egress for v1, or — the v2
   hardening the review flagged as reasonable — routed through a pull-through
   git/package proxy the way decision 5's `install`-exclusion already
   anticipates for v2 registries.
4. The verifier `docker cp`s the checked-out tree out of the (exited, not-yet
   `--rm`'d) clone container into its own per-job private directory
   (`<verifier-private-root>/<job-id>/`, a UUID-per-job dir under a root only
   the verifier's *runner* logic touches) — **not** a bind-mount shared with
   go-code, and not the network-attached clone container itself. Once the
   copy completes, the verifier `docker rm`s the clone container (step 2's
   note on why `--rm` isn't used at launch). A short-lived shared-tmpfs
   volume between the clone job and the runner step is an equally valid
   implementation of this same handoff and avoids the explicit `docker rm`
   step — either is fine; `--rm` at launch is the one option that is **not**
   fine, since it would destroy the tmpfs before the handoff reads it.
5. The verifier runs the symlink/containment checks from the original draft
   (`EvalSymlinks` + `Lstat`-walk + `filepath.Rel` containment) against **its
   own copied tree** — defense-in-depth against a malicious repo's own
   symlinks, now checking content that already passed through one sandboxed
   git invocation — then launches the **build/test/lint job** (decision 1,
   `--network=none`) mounting that tree `-v <that path>:/src:ro`.
6. On job completion (success, failure, or timeout), the verifier deletes
   `<verifier-private-root>/<job-id>/` — mirroring the
   `cleanupTransferred`-guarded defer-cleanup discipline `spawnHealthBuild`
   already uses (`tool_code_health.go:30-46`), so cleanup fires exactly once,
   after the container has finished reading the tree.

This adds one more sandboxed container per job (clone) beyond the one that
already existed (build/test/lint) — no new isolation *mechanism*, one more
*use* of the mechanism decision 1 already committed to.

**Deferred to implementation time:** the exact `<thin-git-image>` pin (a
minimal image with just `git`, digest-pinned per item 5's discipline); whether
the clone→runner handoff uses `docker cp` or a short-lived shared tmpfs
(either is fine — pick whichever the Docker SDK/CLI wrapper the verifier uses
makes less code, not a security-relevant choice since both stay inside the
verifier's own container boundary); the `gocode-verify-egress` network's exact
egress policy (open internet vs. proxy-only, v1 vs. v2 as noted above).

### 5. v1 scope cuts

**Decision: v1 executes `build`/`test`/`lint` only — `install` is excluded
entirely. Detected-only argv, no caller-supplied commands. No `docker build` of
repo Dockerfiles. Images pinned by digest with refuse-on-miss.**

**CommandKinds in scope.** Mapping directly onto the shipped `envdetect.
CommandKind` enum (`internal/envdetect/envdetect.go:35-38`):

| `CommandKind` | v1 |
|---|---|
| `KindBuild` | **in scope** |
| `KindTest` | **in scope** |
| `KindLint` | **in scope** |
| `KindInstall` | **excluded** — deferred to v2 behind a pull-through registry proxy |

Rationale is the review's: `install` = registry egress + arbitrary
`postinstall`/`build.rs`/`pip` hooks, the worst combination, and `--network=none`
(which v1 uses for all in-scope kinds) makes install impossible anyway. The
honest v1 limitation this creates — a repo whose `build`/`test` needs deps that
were never installed will simply fail or error under `--network=none`, yielding
`buildable: unverified`/`failed` rather than a false `verified` — is acceptable
and correct (v1 verifies what can run without a network install step;
vendored-dep repos and no-dep builds verify cleanly). v2's pull-through registry
proxy is what unlocks `install`.

**Caveat, named explicitly (2026-07-06 re-review): "fails cleanly" is not
guaranteed — an empty-pass can look identical to a real pass.** A lenient
runner can `exit 0` having verified nothing: `pytest` collecting zero tests
under `--network=none`-starved deps, an `npm test` script with no `test` files
configured, a `make test` target that is a no-op. This is the same underlying
class as the standing secondary finding from the original review (an exit
code is attacker/repo-controlled, not ground truth) — it is restated here
because v1's own scope cut (no install) makes an empty-pass *more* likely, not
less. **Binding contract:** `buildable: verified` means "the detected command
exited 0 inside the sandbox," full stop — it is a necessary-not-sufficient
signal and **must never be used to upgrade the confidence of any other
go-code signal** (`dead_code`'s confidence levels, `code_health`'s other
sub-scores, etc.), which remain independently computed on their own evidence.
`code_health`'s `buildable` sub-score itself is reported plainly as
`verified`/`unverified`/`failed` with no derived "this makes the repo
healthier" weighting beyond that one field.

**Detected-only argv.** The verifier accepts argv only where the corresponding
`envdetect.Command` was surfaced by detection; caller-supplied arbitrary argv is
**rejected** in v1 (closes Open Question 6). Preferring `Source: "manifest"`
over `Source: "convention"` is a ranking concern, not a gate — both detected
sources are allowed; what is forbidden is a *caller* inventing a command.

**Curated v1 image list.** One minimal **official** base per ecosystem
`envdetect` detects. Named by repo here; **exact `@sha256:` digests are pinned
at implementation time, not design time** — and the runtime **refuses**
(`buildable: unverified`) rather than falling back to `:latest` when a
language/version has no pinned digest:

| Ecosystem (`Manager`) | Base image (official, pin digest at impl time) | Notes |
|---|---|---|
| go | `golang:1-bookworm` | Debian-based; ships `gcc`/`make` for cgo + Makefile builds |
| npm / yarn / pnpm | `node:22-bookworm-slim` | `corepack enable` covers yarn/pnpm without an install step |
| cargo | `rust:1-bookworm` | full toolchain incl. `clippy` component for `KindLint` |
| python / poetry | `python:3.12-slim-bookworm` | poetry/tooling baked at image-build; note deps needing `install` are out of v1 scope |
| make | (image of the layer's **primary** detected language) or `buildpack-deps:bookworm` for a language-less Makefile | `make` shells out to arbitrary tools — run it in the language image that has them; refuse if none applies |

All must be `linux/arm64` variants (the deploy box is ARM) — the official images
above are all multi-arch and publish `arm64`. Refuse-on-miss is the rule for any
ecosystem/version outside this table.

**No `docker build`.** Verify never builds a repo's own `Dockerfile` in v1
(arbitrary build-time code + registry egress); it only runs detected
build/test/lint argv inside a curated pinned image.

**Deferred to implementation time:** the literal digest pins (a maintenance task
with its own refresh cadence — a secondary-finding operational cost the review
flagged); the per-language default `WorkDir`/cache-env wiring; whether `make`'s
primary-language image selection reads the `Environment.Toolchains[0]` layer or
a `Polyglot` heuristic.

### 6. Sized tmpfs + bounded queue

**Decision: `tmpfs` workdir capped at 2 GiB with an inode cap, under a hard
container `--memory=4g --memory-swap=4g` bound; a bounded job queue of depth 8
that fast-fails with a *distinct* `queue_full` response (not the per-repo
`computing` response).**

**tmpfs sizing (justified against 24 GiB + no-parallel-builds + sem-of-1).** The
global semaphore of 1 (decision 5) means **at most one job's tmpfs exists at any
instant**. The container gets `--tmpfs /work:rw,size=2g,mode=1777,
nr_inodes=262144` and a hard `--memory=4g --memory-swap=4g` (no swap) cgroup
cap. Two things make this OOM-safe regardless of PSI timing:

- tmpfs pages are **charged to the container's memory cgroup**, so the 2 GiB
  tmpfs + process RSS together cannot exceed the 4 GiB `--memory` cap — the
  kernel OOM-kills the *container*, not the host, if a build tries to fill
  tmpfs and allocate simultaneously. This is the hard bound the review asked for:
  it does **not** depend on the preflight PSI gate catching pressure in time,
  because the cgroup limit is enforced synchronously by the kernel.
- Worst-case host footprint of verify at any instant is thus **≤ ~4 GiB** (one
  job), leaving ~20 GiB for postgres/redis/the 15+ MCP services — comfortably
  within 24 GiB and consistent with `~/AGENTS.md`'s no-heavy-parallel-builds
  rule. `nr_inodes` caps inode exhaustion (a many-tiny-files DoS) independently
  of byte size.

The preflight PSI/available-memory gate (decision 5, item 3) remains as the
*admission* check **on top** of the per-container hard cap — it prevents
starting a job when the host is already stressed by *other* services; the
cgroup cap prevents a running job from stressing the host. Both are needed;
neither replaces the other. **Threshold corrected (2026-07-06 re-review): the
verify-specific admission threshold must sit above the 4 GiB job cap, not
reuse `~/bin/cargo-load`'s general-purpose 3 GiB.** The original text quoted
the generic `cargo-load` `REFUSE` threshold (available < 3 GiB) verbatim; a
job admitted at, say, 3.5 GiB available can still ramp its container up to the
full 4 GiB `--memory` cap, transiently over-committing the host before the
cgroup limit (which bounds the *container*, not host-wide availability) has
any effect. Verify's own admission check uses a **verify-specific** threshold
of **available < 6 GiB ⇒ REFUSE** (job cap + a ≥2 GiB margin), distinct from
and stricter than the shared `cargo-load` 3 GiB used for other workloads on
this box; the PSI avg10 > 20 half of the gate is unchanged. **Worst-case host
footprint restated honestly:** "≤ ~4 GiB, comfortably within 24 GiB" undercounts
the gVisor `runsc` sentry's own per-sandbox memory, the verifier process, and
Docker daemon overhead, and — per `~/AGENTS.md` — this box already runs
postgres/redis/15+ other services with "no slack." The real safety property is
not idle headroom; it is **the 6 GiB admission threshold plus the 4 GiB
container-level hard cap acting together**, not free RAM assumed to exist.

**Bounded queue + fast-fail.** Concurrency is: per-repo dedup → bounded FIFO
queue (depth **8**) → global semaphore of 1. The queue is a buffered channel of
capacity 8. Behavior on arrival:

- **Repo already in flight** (per-repo dedup hit, `sync.Map`/`LoadOrStore` per
  decision 5): return the existing `code_health` async convention —
  `<status>computing</status>` + "retry in 60s". This is *not* a failure; it is
  "your result is being computed, poll for it."
- **Queue has room:** enqueue; return `<status>computing</status>` (a new job
  accepted) + retry hint.
- **Queue full** (8 distinct repos already waiting behind the running job):
  **fast-fail** with a **distinct** status the caller can tell apart from
  "computing" — the verifier returns HTTP `429` and go-code surfaces:

  ```
  <status>queue_full</status>
  <queued>8</queued>
  <retry_after_s>120</retry_after_s>
  <detail>verifier at capacity; this is a distinct condition from
   "already computing this repo" — the job was not enqueued</detail>
  ```

  The review explicitly wanted the full-queue case to "say so" as distinct from
  "already computing this repo": `queue_full` is a *rejection* (nothing was
  enqueued, caller should back off), whereas `computing` is an *acceptance*
  (work is in progress, caller should poll). Same retry-in-Ns ergonomics as
  `code_health`, different semantic.

**Deferred to implementation time:** tuning the queue depth (8 is a starting
point sized to the sem-of-1 throughput — a job at the 5-min ceiling means a full
queue drains in ≤40 min worst case, so 8 balances "absorb a burst" against "do
not promise work we cannot start within a reasonable window"); the exact
`retry_after_s` values; whether the queue is per-service in-memory (recommended
for v1 — a restart clears it, which is fine for an opt-in verify tool) vs.
Redis-backed (unnecessary for v1).

---

**Summary of what remains open after these resolutions (updated 2026-07-06,
post two re-review rounds).** Round 1 (2026-07-06) graded 4 of 6 items
CLOSED-WITH-CAVEAT (1, 3, 5, 6) and 2 STILL-OPEN (2, 4), overall **HARDEN**.
Round 1's fixes (item 2 → dedicated `internal: true` two-peer network; item 4
→ verifier clones its own tree instead of trusting a go-code-supplied path;
item 3 → names `Decoder.DisallowUnknownFields()`; item 6 → 6 GiB preflight
threshold; item 5 → `verified` never upgrades other signals) closed items 1/3/5/6
and closed items 2/4's *original* findings — but a **narrow round-2 re-review
(items 2+4 only)** surfaced a **new coupling gap between the two fixes**: the
verifier's clone step needs network egress, but its API network (item 2) is
`internal: true` (no egress by design), and the clone itself was running
inside the verifier's own process — the sole holder of the Docker socket —
where a git-content RCE (`.gitattributes` filters, submodule traversal
classes; hooks-disabled alone doesn't cover these) would be a host compromise,
the exact thing decision 4 as a whole exists to prevent. **Resolved** by making
the clone step *itself* a sandboxed job (same gVisor/userns/cap-drop primitive
as every build/test/lint job, decision 1) on a separate, egress-only
`gocode-verify-egress` network attached only to the ephemeral clone container
— never to the verifier's long-lived, socket-holding, API-serving process. No
new isolation mechanism was introduced; the existing one is used one more
time. What is left after both rounds is **implementation-time
parameterization**, not undecided design: literal image digests (items 1's
git-clone image and item 5's language images), the concrete token/port values
and `gocode-verify`/`gocode-verify-egress` network definitions (item 2),
the benign-env allowlist contents (item 3), measured systrap overhead on the
box (item 1), and the clone→runner handoff mechanism (`docker cp` vs. shared
tmpfs, item 4). The one genuine *architectural* deferral is Firecracker-per-job,
which is blocked by hardware (no `/dev/kvm` on the A1 VM) and only reopens if
verify moves to A1 bare metal — recorded, not hand-waved. These revisions are
submitted for a third `architecture-security-cost` **re-review**; the BLOCK
verdict stands until that reviewer signs off.
