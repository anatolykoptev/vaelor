# Agent Guide: go-filewatcher

**Go 1.26.7** | `github.com/larsartmann/go-filewatcher/v2` | **MIT License**

> **Companion docs:** [FEATURES.md](./FEATURES.md) (feature inventory) ·
> [ROADMAP.md](./ROADMAP.md) (long-term direction) · [TODO_LIST.md](./TODO_LIST.md)
> (actionable work) · [CHANGELOG.md](./CHANGELOG.md) (release history).
> This file is for enduring, hard-to-discover-from-code context only.

---

## Critical Commands

```bash
# Using Nix flake (recommended)
nix develop              # Enter development shell with Go and tools
direnv allow             # Auto-load environment on cd (requires direnv)

# Nix apps (run from anywhere, no need to be in dev shell)
nix run .#check          # Full quality: vet + lint + test
nix run .#ci             # Full CI: tidy + fmt + vet + lint + test
nix run .#lint-fix       # Auto-fix linter issues
nix run .#test           # Run tests with -race
nix run .#test-v         # Run tests with -race -v
nix run .#lint           # Run linter
nix run .#lint-tests     # Run linter (--tests explicit)
nix run .#bench          # Run benchmarks
nix run .#bench-baseline # Capture benchmark baseline (run from repo root)
nix run .#bench-diff     # Diff benchmarks vs baseline (hermetic benchstat)
nix run .#coverage       # Generate coverage report
nix run .#fmt            # Format Go code (writes — run from repo root)
nix run .#tidy           # Run go mod tidy (writes — run from repo root)
nix run .                # Default = check

# Nix quality gates
nix flake check          # Run all checks (build, test, lint, fmt, vet, examples-build)
nix build .              # Validate reproducible build
nix fmt                  # Format .nix files

# Inside dev shell (aliases are set automatically):
check       # nix run .#check
ci          # nix run .#ci
lint        # nix run .#lint
lint-fix    # nix run .#lint-fix
test        # nix run .#test
```

## Updating vendorHash

When `go.mod`/`go.sum` change, update `vendorHash` in `flake.nix`: run
`nix build .` and copy the `got:` hash from the error (placeholder-then-rebuild
procedure: [docs/nix-vendorhash.md](docs/nix-vendorhash.md).

## Non-Obvious Conventions

Uses `errors`/`fmt` only — sentinel errors, `%w` wrapping, `errors.Is`. Full
sample: [docs/error-handling.md](docs/error-handling.md).

### Single Package Layout

All code in **root package** (`filewatcher`). No `internal/` or `pkg/` subdirectories — all code lives in the package root.

### File Organization

All code lives in the root package `filewatcher`; the per-file responsibility
table (watcher.go, backend.go, filter.go, middleware.go, ...) moved to
[docs/file-organization.md](docs/file-organization.md).

### Examples (`examples/`)

Separate Go programs demonstrating usage. Each subdirectory is a standalone
`main` package that imports the library. Shared helpers (including
`demo.MustWatch`) live in `examples/demo/`. Build with `go run ./examples/<name>`
or `go build ./examples/...`. Part of the same Go module (no separate go.mod).

### Website (`website/`)

Separate Astro + Starlight site (`filewatcher.lars.software`, Firebase Hosting) with
its own flake/toolchain — not part of the Go module. Build `cd website && nix run
.#build`. The `/changelog` page is GENERATED from repo `CHANGELOG.md` by
`sync-changelog.mjs` on every build — never hand-edit the mdx; update CHANGELOG and
rebuild. Details: [website/README.md](website/README.md) if present.

---

## Critical Gotchas (index)

Full detail for every gotcha: [docs/gotchas.md](docs/gotchas.md). The one-liners:

1. Middleware order is REVERSED — first in `WithMiddleware` runs outermost.
2. `WithDebounce` = global (all events → one callback); `WithPerPathDebounce` = per-path.
3. `exhaustruct` linter: every struct field must be initialized.
4. Every test needs `t.Parallel()` (paralleltest linter).
5. Multi-op events resolve by priority Create > Write > Remove > Rename.
6. Chmod events are ignored (`convertEvent` returns nil).
7. `DefaultIgnoreDirs` is an exported global with `//nolint:gochecknoglobals` — keep the nolint.
8. `WithDebug` is active: wires slog debug logging through the pipeline.
9. `WithPolling` is active: starts a pollLoop alongside fsnotify (NFS/FUSE).
10. Circuit breaker: Closed → Open → HalfOpen; one event probes recovery.
11. `fswatcher.Add` errors (incl. ENOSPC) degrade instead of aborting the walk; `Stats.WatchErrors` counts them.
12. `maxWatches` auto-detected on Linux; override with `WithMaxWatches`; over-budget dirs skip silently.
13. `.gitignore` walking is default-on; trailing-slash patterns (`Build/`) do NOT match the dir itself.
14. Watch registration is batched (1000/batch, `runtime.Gosched()` between).
15. `WithExcludePaths`: absolute-path prefix matching, walk-time only.
16. `Remove()` removes the whole subtree, not just the top-level dir.
17. `Reset()` clears runtime state, keeps config; enables `Watch()` again after `Close()`.
18. `pathKey()` = NFC-normalized (+lowercased when case-insensitive); perf contract and bench-diff methodology in docs/gotchas.md #18.
19. `watchListKeys` map gives O(1) duplicate detection and self-heal checks.
20. Symlink cycle detection via `symlinkVisited` map (`WithFollowSymlinks`).
21. Middleware drops tracked via atomic flag → `Stats.EventsDroppedByMiddleware`.
22. `Add`/`AddRecursive`/`Watch` validate paths (`ErrPathNotFound`/`ErrPathNotDir`); `Remove` does not.
23. Error classification: permission/not-exist/ENOTDIR permanent (self-heal abandons); ENOSPC transient.
24. Poll mode applies the same skip/exclusion/gitignore logic as the initial walk.
25. `EventChannelDropOnFull`: non-blocking send, drops counted; default is blocking backpressure.
26. Watch-budget safety fraction applies ONLY to auto-detected limits; explicit `WithMaxWatches` wins, incl. through `Reset()`.

## Key Patterns

Pattern→location table (13 rows) moved to [docs/patterns.md](docs/patterns.md).

Default-guard convention + middleware resource-cleanup patterns (worked
examples): [docs/patterns.md](docs/patterns.md).

## Dependencies

```
github.com/fsnotify/fsnotify          # Core file watching (v1.10.1)
github.com/LarsArtmann/gogenfilter/v3 # Generated code detection (v3.6.1, published — NO local replace)
github.com/sabhiram/go-gitignore      # .gitignore pattern matching (zero transitive deps)
golang.org/x/text/unicode/norm        # NFC Unicode normalization in pathKey (v0.42.0)
golang.org/x/time/rate                # rate.Limiter for MiddlewareThrottle
```

gogenfilter v3 API notes (v3.6.1 migration semantics, sqlc detection):
[docs/gogenfilter-v3-notes.md](docs/gogenfilter-v3-notes.md).

## Named Types (phantom types)

`type X string` types for compile-time safety on path-like strings:
`EventPath`, `RootPath`, `DebounceKey`, `LogSubstring`, `TempDir`, `OpString`.
Constructors like `NewEventPath()`; full table + domain methods
(`.Base()/.Dir()/.Ext()/.Join()`): [docs/phantom-types.md](docs/phantom-types.md).

## Release / CI Gotchas

| Gotcha                                    | Rule                                                                                                                                                                                                                                                                                                                                                                                                            |
| ----------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| release-please needs a repo permission    | Fails with "GitHub Actions is not permitted to create or approve pull requests" when off. Fix: `gh api -X PUT repos/LarsArtmann/go-filewatcher/actions/permissions/workflow --input - <<< '{"default_workflow_permissions":"read","can_approve_pull_request_merge_requests":true}'`. On PR merge release-please creates the tag AND the GitHub Release; `release.yml` only fires for manually-pushed `v*` tags. |
| release-please is path-scoped             | Config-file mode with `exclude-paths: ["website", "docs", ".github"]` — commits touching ONLY those paths never cut releases. Convention: `chore/docs/build(website)` for non-module changes, never `fix(website)`/`feat(website)` (that shipped a Go-less v2.4.1 on 2026-10-07).                                                                                                                               |
| golangci-lint version pin                 | v2.14.0 pinned in flake, `ci.yml`, and `release.yml` — bump all three together. `.golangci.yml` uses the `exhaustruct_v5` key (v2.13+ only; v2.12 rejects the config).                                                                                                                                                                                                                                          |
| Release-PR CI "workflow file issue"       | Release-please PRs show `conclusion: failure` with ZERO jobs (hit v2.4.0 + v2.4.1). GitHub-side quirk — identical workflows run green on Dependabot PRs and master pushes. Verify releases via master CI on the merge commit; the release itself is unaffected.                                                                                                                                                 |
| Verify built slugs before publishing URLs | GitHub slugs strip dots: `migration-v2.3-to-v2.4` → `/guides/migration-v23-to-v24/`. Check `ls dist/guides/` (or the live URL) before `gh release edit`/deploy — the unchecked slug 404'd in the published v2.4.0 notes.                                                                                                                                                                                        |
| Auto-commit daemon races                  | The daemon commits (and occasionally resets) local master while you work. `git fetch` + check `git log HEAD..origin/master` before pushing; rebase if diverged. Never assume your last local commit is still HEAD.                                                                                                                                                                                              |
| ubuntu-latest migrates to 26.04           | Rollout starts 2026-10-19, complete 2026-11-19 (actions/runner-images#14748). Audited 2026-10-07: all workflows are pinned-SHA actions + explicit Go versions, no Python/apt-key/cgroup exposure. The CI test matrix runs both ubuntu-24.04 and ubuntu-26.04 (early compat proof); all other jobs are pinned to ubuntu-24.04 for determinism through the rollout window. Re-evaluate pins when GitHub announces 24.04 brownouts (expected 2027). |

### go.mod language version

The `go` directive must stay at the CI matrix floor (`go 1.26.7`); the local
toolchain is newer and three BuildFlow tools silently bump it (strace-proven
2026-10-07, all skipped via `skip_steps` in `.buildflow.yml`):
`go-version-auto-configure` (wants major.minor-only), `go-mod-update` (minor
mode bumps to latest Go), and `go-structure-linter` (buildflow 202b114+
auto-repairs its "Go 1.27 is available" finding mid-run, backups in
`/tmp/go-structure-linter-backups/`). `go-mod-normalize` is exonerated
(candidate downgrade is rejected by the dependency-floor gate, then atomically
restored). Restore: `sed -i 's/^go 1\.27$/go 1.26.7/' go.mod`. The bump can
land on origin even when you ran no go commands (daemon commits) — check the
`go` line whenever the daemon commits go.mod; dependent PR CI recovers only
after master is fixed plus a branch update/re-run.

**Structural guard (added 2026-10-07)**: the floor is now enforced, not just
remembered — a `Go Directive Floor` CI job asserts `go 1.26.7` and blocks the
test matrix (`needs: go-directive`), and `nix flake check` runs the same
assertion as the `go-directive` check. Intentional bumps must change go.mod
AND both guards in one commit.

**No go 1.27 CI leg (deliberate, 2026-10-07)**: a 1.27 matrix entry was
tried and removed the same day — under the Go 1.27 toolchain,
`encoding/json/v2` (imported in `event.go`) requires the go directive to be
≥ 1.27, so the leg cannot build while the floor is pinned at 1.26.7 (CI run
37581977239: "json.Marshal requires go1.27 or later (file is go1.26)").
Re-adding the 1.27 leg REQUIRES the intentional floor bump to 1.27 — which
also means revisiting the json/v2 usage or accepting the bump. This is
recorded as a v3 decision (docs/research/v3-decisions.md).

**Worktrees and nix**: flake evaluation IGNORES dirty state in linked
worktrees (no `-dirty` drv suffix; it builds the COMMITTED go.mod). Commit the
floor before any worktree proof run.

**BuildFlow on this shared machine**: a fleet binary can gain NEW tools
mid-session — on any behavior change run `buildflow doctor` + `buildflow list
providers` BEFORE rerunning (a stale-binary run cost ~2h on 2026-10-07). Under
high load (many concurrent agent sessions), steps get killed at spawn and
report false failures; serialize heavy runs or wait for a quiet window.

## Known Issues

### Website build (pnpm 11) build-script approvals

Build-script approvals live in `website/pnpm-workspace.yaml` under `allowBuilds:` (`esbuild: true`) — pnpm v11 ignores `pnpm.*` in `package.json` and silently skips unapproved postinstall scripts, so `astro build` then fails on a missing esbuild binary. A placeholder value (e.g. `esbuild: set this to true or false`) silently disables the whole key (cmdguard incident, fixed 2026-09-19).

### Nix sandbox needs GOEXPERIMENT=jsonv2

`event.go` imports `encoding/json/v2` (GOEXPERIMENT-gated). CI sets `GOEXPERIMENT: jsonv2` workflow-wide (ci.yml) and direnv exports it locally, but nix sandbox derivations receive neither, so every Go target fails with "build constraints exclude all Go files in .../encoding/json/v2". The flake sets it wherever Go compiles: `packages.default` and the four `runCommand` checks via `env.GOEXPERIMENT`, and every `mkApp`/`mkBenchApp` script exports it (apps run outside direnv). Any new Go-building derivation or app must set it too (fixed 2026-10-07).

### buildflow nix-hash-fix cannot repair this flake

`nix-hash-fix` has failed 15+/15 runs here: after a hash mismatch it reports "the stale hash was not found verbatim in any .nix file" even when `vendorHash = "sha256-…"` sits verbatim in flake.nix (BuildFlow repo bug; the 2026-10-07 fix was hand-applied after the fixer gave up). Until fixed upstream, update the `vendorHash` in flake.nix to the `got:` hash from `nix build` output by hand, then verify with `nix build`. Consequence: do NOT extract vendorHash into a separate file (the nix-checker suggestion) — it would break the one manual repair path that works.

### buildflow pnpm-audit result cache replays after lockfile-only fixes

The detector result cache keys pnpm-audit on `package.json` but NOT `pnpm-lock.yaml`, so
vulnerabilities fixed via `pnpm audit --fix=update` (lockfile-only change) keep gating the
full pipeline with the stale 9-error finding set even though `pnpm audit` in `website/` is
clean. Surgical purge (the reference's DB path is wrong; the real DB is `cache.db`):

```bash
nix shell nixpkgs#sqlite -c sqlite3 ~/.cache/buildflow/cache.db \
  "DELETE FROM result_cache WHERE value LIKE '%pnpm-audit%';"
```

A no-cache run (`BUILDFLOW_NO_RESULT_CACHE=1`) proves the fix but does NOT overwrite the
stale entry (BuildFlow gap), and it re-executes every step — under high machine load the
re-run hits step timeouts (go killed at spawn) and produces false step failures. Purge the
entry, then re-run the normal cached pipeline. Note `pnpm audit --fix` in pnpm 11 needs an
explicit strategy (`--fix=update` re-resolves the lockfile; `--fix=override` adds
overrides to `website/pnpm-workspace.yaml`). 2026-10-07: 12 vulns (9 high) → 0 via
update + one override (`postcss-selector-parser@<7.1.6: ^7.1.6`, forced major bump,
website build verified green after).
