# ADR 0002: Environment Detect & Verify (static detection → sandboxed execution)

- **Status:** Phase 0 **Accepted** — ready to ship independently. Phase 1
  **BLOCK-UNTIL-RESOLVED** per `architecture-security-cost` review
  2026-07-04 — see "Security-Cost Review" section below for the closing
  conditions. **Update 2026-07-06:** concrete design resolutions for all six
  blocking conditions proposed in the "Phase 1 Design Resolution" section below
  — **resolutions proposed, pending security-cost re-review** (the verdict is
  not changed here; re-review is a separate step and remains that reviewer's
  call).
- **Date:** 2026-07-02 (review appended 2026-07-04; resolutions appended 2026-07-06)
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
2. **Network binding.** The verifier's HTTP listener binds to its container
   network interface reachable **only** on the compose internal network — it
   publishes **no** `ports:` to the host (compose `expose:` / internal network
   only), so it is not reachable from `0.0.0.0` / the host's public interface.
   `GOCODE_VERIFY_URL` therefore resolves to an internal service alias (e.g.
   `http://go-code-verify:PORT/verify`), never a host-published port. This
   mirrors how the existing MCP services already talk (`EMBED_URL`,
   `GO_SEARCH_URL`, `DATABASE_URL`) — private docker network, not the public
   internet (`~/AGENTS.md` ports table + CLAUDE.md env seams).
3. **Belt-and-braces.** Empty `GOCODE_VERIFY_URL` ⇒ Phase 1 unavailable even
   with the enable flag on (already in decision 4). Empty token on the go-code
   side ⇒ it never sends a request.

**Why not mTLS (justified against the topology).** The threat mTLS defends —
network-level impersonation/MITM of the verifier by an off-host or on-wire
attacker — is not present here: both peers are containers on the **same host**,
on a **private docker bridge network**, with **no host-published verifier
port**. The realistic adversary is a *co-located compromised container*, and
against that, the bearer token (which such a container does not hold — it lives
only in go-code's and the verifier's env, and item 3 keeps go-code's secrets off
the verifier) plus "no exposed port" already denies access; mTLS would add
cert-issuance/rotation operational cost for no additional protection on this
seam. This is a **two-way door**: if verify is ever exposed beyond the private
network, or moved cross-host, mTLS is a clean additive upgrade (add a client
cert check alongside the bearer check) and should be adopted then. Recorded as a
reversible decision, not a permanent one.

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
is `{argv, workDir, image, limits}` (decision 4). None of these is
secret-shaped: `argv` is a detected command slice (item 5 forbids
caller-supplied argv), `workDir` is a repo-relative path, `image` is a pinned
digest, `limits` are integers. The verifier **rejects** any request whose JSON
carries an unexpected field — specifically any `env`, `secrets`, `mounts`,
`token`, or `dockerArgs` key — with `400`, so a future caller cannot smuggle env
into a job through the API. This holds by construction: there is no legitimate
reason for a secret to appear in a verify request, so the schema simply has no
field for one and unknown fields are rejected rather than ignored.

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

**Decision: verify accepts *only* a freshly-cloned tree under `WORKSPACE_DIR`;
before any mount, resolve the realpath, reject any symlink in the path,
containment-check under the canonical `WORKSPACE_DIR`, and refuse
`PATH_MAPPINGS`/local-checkout inputs entirely for verify.**

The containment check runs in go-code **before** it POSTs the mount path, and is
re-validated in the verifier **before** it issues `docker run -v` (belt-and-
braces — neither trusts the other). Exact sequence:

1. **Force a fresh clone; refuse local/mapped inputs.** `resolveRoot`
   (`cmd/go-code/resolve.go:44`) today has three source shapes: `WPSource`,
   `RemoteSource` (clone into `WorkspaceDir`), and `LocalSource` (applies
   `PATH_MAPPINGS`, resolves `/host/src/<name>` checkouts via
   `localCheckoutFor`/`bareNameCheckoutFor`, `resolve.go:65-95`). **Verify uses
   `RemoteSource` only.** It explicitly does **not** take the local-checkout
   optimization and **rejects** `local:` / `path=` inputs — because those resolve
   to `/host/src/...` paths that are bind-mounted `:ro` from the host
   (CLAUDE.md gotcha) and are **not under `WORKSPACE_DIR`**. `PATH_MAPPINGS` is
   fine for read-only static-analysis tools but must never select the tree we
   *execute code against*. So verify's rule is: the tree is one go-code itself
   just cloned into `WORKSPACE_DIR`, nothing else.
2. **Realpath + symlink rejection.** Given the intended mount path `p`:
   - `real, err := filepath.EvalSymlinks(p)` — fully resolve; error ⇒ reject.
   - Walk every path component of the *original* `p` with `os.Lstat`; if any
     component is a symlink (`mode&os.ModeSymlink != 0`), **reject** even if
     `real` happens to land inside `WORKSPACE_DIR`. Fail loud on symlinks rather
     than silently following them — a symlink inside the clone pointing at
     `/etc` or `/var/run/docker.sock` must not be mountable.
   - Canonicalize the base: `wsReal, _ := filepath.EvalSymlinks(WORKSPACE_DIR)`.
   - **Containment:** require `real == wsReal` or `real` strictly under it —
     compute `rel, err := filepath.Rel(wsReal, real)`; reject if `err != nil`,
     if `rel == ".."`, or if `strings.HasPrefix(rel, ".."+sep)`. (Prefix-string
     checks alone are insufficient — `/ws-evil` shares the `/ws` prefix — so use
     `filepath.Rel` + `..` inspection.)
3. **Mount read-only, execute in tmpfs.** The validated `real` is mounted `-v
   real:/src:ro`; the writable workdir is the container `tmpfs` (item 6). A build
   cannot mutate the host-side clone (decision 6 already commits this).

This check **sits directly in front of** `internal/ingest`'s clone path handling
(`clone.go` → `CloneRepo` returns `CloneResult.LocalPath` under `DestDir =
WORKSPACE_DIR`; `resolve.go`'s `bareNameCheckoutFor`/`localCheckoutFor` already
reject `/`, `\`, `.`, `..` traversal at `resolve.go:118-122` — verify reuses that
hygiene but additionally *rejects the local-checkout branch outright*, since a
valid `/host/src` checkout is precisely the PATH_MAPPINGS case verify must not
mount).

**Deferred to implementation time:** whether to `chroot`/`openat2(RESOLVE_
BENEATH)`-harden the walk (a nice-to-have; the `EvalSymlinks` + `Lstat`-walk +
`Rel` triple is sufficient for v1); the shared helper's exact location (a small
unexported validator next to `resolveRoot`, not a new public package).

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

The preflight PSI/available-memory gate (decision 5, item 3: `REFUSE` when
available < 3 GiB or PSI avg10 > 20) remains as the *admission* check **on top**
of the per-container hard cap — it prevents starting a job when the host is
already stressed by *other* services; the cgroup cap prevents a running job from
stressing the host. Both are needed; neither replaces the other.

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

**Summary of what remains open after these resolutions.** All six blocking
mechanisms are now decided. What is left is **implementation-time
parameterization**, not undecided design: literal image digests (item 5), the
concrete token/port values (item 2), the benign-env allowlist contents (item 3),
and measured systrap overhead on the box (item 1). The one genuine
*architectural* deferral is Firecracker-per-job, which is blocked by hardware
(no `/dev/kvm` on the A1 VM) and only reopens if verify moves to A1 bare metal —
recorded, not hand-waved. These resolutions are submitted for
`architecture-security-cost` **re-review**; the BLOCK verdict stands until that
reviewer signs off.
