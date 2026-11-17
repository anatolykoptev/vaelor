# Roadmap

**Last Updated:** 2026-10-07

Long-term direction and raw ideas for go-filewatcher. Items here are
**exploratory and not yet committed** — they graduate to
[TODO_LIST.md](./TODO_LIST.md) when scoped into bounded, estimable work. For the
current feature snapshot, see [FEATURES.md](./FEATURES.md).

---

## Themes

1. **Hardening** — broader platform coverage, error-path testing, performance
   regressions.
2. **Ecosystem integration** — first-class support for observability stacks and
   dogfooding in real consumers.
3. **API evolution** — toward v3 with feedback-driven breaking changes.
4. **Operational excellence** — automated releases, dependency hygiene, docs
   freshness, reproducible benchmarks.

Concrete tasks under these themes live in
[TODO_LIST.md](./TODO_LIST.md). The ideas below are the longer-term, less
certain directions worth exploring.

---

## Ideas Worth Exploring

### Platform Coverage

- **macOS FSEvents edge cases** — rename semantics differ from Linux; document
  or test the divergences (current CI is Linux-only).
- **BSD/kqueue** — fsnotify supports it; verify our assumptions (budget
  detection, batched registration) hold.
- **Real filesystem probing (v3)** — `CaseSensitivityAuto` currently resolves by
  `runtime.GOOS`, not by probing the actual mount. A Linux box with a
  case-insensitive ext4 mount (rare) gets the wrong mode. A `CaseSensitivityProbed`
  mode that writes two differently-cased probe files at startup would detect the
  truth. NFC normalization, `pathKey`, and the poll/gitignore case-awareness are
  already in place; probing is the remaining gap.
- **macOS/Windows CI for case-insensitive tests** — the case-insensitive and
  NFD/NFC behavior is verified as _logic_ on Linux (case-sensitive ext4). Real
  filesystem behavior on APFS/NTFS is only provable with a macOS/Windows CI
  matrix.

### API Evolution

- **v3 planning** — accumulated deprecations (`WithWatchedIgnoreDirs`,
  `WithOnError`, `MiddlewareRateLimit`) and awkward signatures (the two-arg
  `ErrorHandler`) suggest a v3 cleanup pass.
  The concrete deprecation inventory is a TODO_LIST task; this is the strategic
  framing: gather breaking changes over the next 6–12 months before cutting.
  See [docs/research/watchchanges-contract.md](./docs/research/watchchanges-contract.md)
  for the event-contract analysis that informs v3 API decisions.
- **Poll-mode event quality (v2.5+/v3)** — `WithPollDeduplicate(true)`
  (per-path timestamp tracking with an LRU) and `WithPollDetectRenames(true)`
  (platform-specific inode extraction) are designed-but-unbuilt; both are
  large features. The dedup limitation is documented in the `WithPolling` doc
  comment and Troubleshooting guide.
  (`src: 2026-08-12 series`)
- **`MiddlewareDropCallback` API** — explicit drop notification would replace
  the `atomic.Bool` "returned nil without emitting" heuristic (which can
  miscount transforming middleware).
- **Split `maxWatches` into configured + effective** — three fields currently
  track one concept (`maxWatches`, `maxWatchesExplicit`, `maxWatchesDetected`).
- **Split `Stats` into sub-structs** — `EventStats`/`ErrorStats`/`WatchStats`
  would keep the flat struct from growing a field per feature.
- **Extract a `channelSender` type from `emitEvent`** — encapsulate the
  blocking vs DropOnFull send strategy; makes the strategy testable in
  isolation and shortens a 60+ line function.
- **Runtime deprecation warning for `MiddlewareWriteFileLog`** — doc comment
  warns today; a `log.Warn` is v3 prep.
- **gogenfilter sqlc output-dir config** — expose gogenfilter's
  config-aware detection so sqlc users can deliberately re-enable
  `models.go` filtering.
  (`src: 2026-10-07_02-24 §f17`)
- **Streaming filter protocol** — current `Filter` is a sync bool. Consider
  returning `(keep bool, err error)` or a channel-based variant for filters
  that need async I/O (e.g. remote manifest lookup).
- **`WithNormalizeUnicode(false)` escape hatch** — NFC normalization is always
  on in `pathKey`. A small minority of systems (NFD-preficient, byte-exact
  comparison needs) may want raw-byte keys. An opt-out belongs in a v3 pass.
- **Phantom-typed `PathKey`** — `pathKey()` returns a bare `string`; a named
  `type PathKey string` (like the existing `EventPath`/`RootPath` phantom types)
  would make accidental misuse of raw paths vs canonical keys a compile error.

### Observability

- **pprof endpoints for watcher introspection** — expose watch-list size,
  debouncer queue depth, filter rejection counts via `net/http/pprof`.

### Performance

- **Zero-allocation event path** — current `ConvertEvent`/`Create` is 3 allocs;
  investigate pooling or stack-allocated `Event` for hot paths.
- **NFD path-key caching** — `pathKey` on decomposed (NFD) input — emitted by
  the macOS filesystem — allocates (~1 µs/op, 3 allocs). ASCII and pre-composed
  NFC are allocation-free. An LRU/sync.Map cache would only matter at extreme
  macOS throughput (>10k Unicode events/sec); measured, not yet needed.
- **Trie-based exclude-path matching** — `shouldExcludePath` is O(n) in the
  number of excluded paths (linear prefix scan). For large exclude sets, a trie
  or prefix map gives O(path-depth) lookup.
- **Benchmark freshness CI** — local baseline capture (`nix run .#bench-baseline`)
  and diff (`nix run .#bench-diff`) tooling exist, but the baseline is gitignored.
  The open question is whether to commit a sanitized baseline and add a CI
  regression gate with tolerance (see TODO_LIST open questions).

### Reliability

- **Race-detector-on-CI flake quarantine** — two formerly-flaky tests
  (`TestWatcher_Stats_Metrics`, `TestWatcher_Watch_WithMiddleware`) are now
  hardened via the `waitForCondition` polling helper. The open question is
  whether to also add a `-count=50 -race` statistical gate in CI for
  timing-sensitive tests (self-heal, debouncer intervals).

### Operational

- **Cross-platform release artifacts via goreleaser** — `.goreleaser.yml` is
  configured but **not invoked** by `release.yml` (which uses
  `softprops/action-gh-release` with auto-generated notes). Now that
  release-please handles versioning + CHANGELOG, goreleaser may be dead config
  to delete (see TODO_LIST open questions). Wiring it in would ship compiled
  cross-platform binaries on tag. See FEATURES.md "Cross-platform releases"
  (currently PARTIALLY DONE).
- **Dependency freshness SLO** — current policy is "update within 24h"; codify
  with Dependabot status checks.
- **Docs freshness gate** — the `check-exported-symbol-docs` CI workflow exists
  and ratchets against a 15-symbol exemption list (11 phantom-type helpers, 1
  deprecated option, `WatcherStateFlags`, and 2 real gaps being closed). The
  exploratory angle is auto-generating doc tables from `go doc -all` source
  parsing to eliminate manual sync entirely.
  See [docs/research/INDEX.md](./docs/research/INDEX.md) for related research.
- **Automated release tooling** — release-please is wired in
  (`.github/workflows/release-please.yml`), auto-generating release PRs from
  conventional commits. The commitlint gate
  (`.github/workflows/commitlint.yml`) enforces the commit format that feeds it.
  See [docs/research/semantic-release-evaluation.md](./docs/research/semantic-release-evaluation.md)
  for the tradeoff analysis that informed this choice.

### Ecosystem

- **go-daemon pairing examples** — `examples/watcher-daemon`
  (OnListen→`Watch`, OnShutdown→`Close`, stream `Event`s over unix socket/SSE),
  a "watcher as a systemd service" website guide (sd_notify + watchdog ×
  `WithSelfHeal`), and a health endpoint backed by `Stats()`. Host-repo
  decision pending (this module is Go 1.26-pinned; go-daemon needs 1.27+).
  A standalone `filewatcher-daemon` sibling binary is demand-gated.
  (`src: 2026-10-07_00-42`)

- **Master-health automation** — beyond the existing CI badge: a workflow that
  auto-opens an issue when a master push run fails (go.mod-class breakage gets
  noticed without a PR-review session as detector), plus a "re-run failed jobs"
  nudge for PRs hit by zero-job startup failures (they never retry on their
  own). (`src: 2026-10-07_03-10 §f28/f38`)

---

## Non-Goals

These are explicitly **out of scope** to keep the library focused:

- **CLI tooling** — go-filewatcher is a library, not a CLI. Consumers build
  their own.
- **Database-backed event journaling** — out of scope; users compose
  `MiddlewareBatch` with their own persistence layer.
- **Cross-language bindings** — Go-only. Other languages should bind to fsnotify
  directly.
- **GUI / TUI** — not the library's job.

---

## Versioning Strategy

| Track         | Cadence                       | Trigger                                     |
| ------------- | ----------------------------- | ------------------------------------------- |
| Patch (x.y.Z) | As needed                     | Bug fixes, no API changes                   |
| Minor (x.Y.0) | Monthly–quarterly             | New options, filters, middleware (additive) |
| Major (X.0.0) | When breaking changes pile up | Removed deprecations, signature changes     |

See [API_STABILITY.md](./API_STABILITY.md) for the full stability policy.
v2.4.0 (2026-10-06) shipped filesystem case-sensitivity + NFC normalization,
the reliability knobs (`WithMaxWatchesSafetyFraction`, `WithEventChannelMode`,
`WithContentHashMaxSize`, `WithErrorBufferSize`), middleware drop/backpressure
stats, and symlink cycle detection. v2.4.1 (2026-10-07) was a website-only
patch (release-please is now path-scoped so that cannot recur). The next minor
will accumulate toward v3.0, where deprecated symbols will be removed.
