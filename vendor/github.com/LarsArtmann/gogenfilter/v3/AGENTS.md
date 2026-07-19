# gogenfilter

_A Go library for detecting and filtering auto-generated code files._

## Overview

This project provides detection and filtering capabilities for auto-generated Go code files from tools like sqlc, templ, go-enum, protobuf, oapi-codegen, deepcopy-gen, wire, moq, mockgen, and stringer. It's designed as a library for use by linters and static analysis tools.

## Architecture

- **Two-phase detection**: filename-based (zero I/O) then content-based (reads file)
- **Table-driven detector system**: `[]detector` slice with `option`, `reason`, `matchFilename`, and `checkContent` fields
- **Functional options API**: `NewFilter(WithFilterOptions(FilterAll), ...)` — Filter is immutable after construction, enabled when options/patterns are provided
- **Branded errors**: `[gogenfilter:<code>]` prefix, sentinel errors for `errors.Is`, `ErrorCoder` interface, `Err` field for wrapped errors (stdlib convention)
- **`fs.FS` abstraction**: `WithFS()` option for testability; tests use `fstest.MapFS`
- **Derived lists**: `AllFilterOptions()`, `AllGeneratorOptions()`, `AllFilterReasons()` are all derived from the `detectors` table — adding a new detector automatically updates everything

## Project Structure

- Root-level `.go` files: Core library implementation (standard Go library convention)
- Test files: `*_test.go` alongside source files
- `docs/`: Planning documents and status reports
- `.github/workflows/ci.yml`: Go CI — test, vet, lint, benchmarks (path-filtered: `*.go`, `go.mod`, `go.sum`, `testdata/**`, `.golangci.*`)
- `.github/workflows/website.yml`: Website CI/CD — typecheck, build, validate docs, deploy to Firebase (path-filtered: `website/**`)
- `.github/dependabot.yml`: Weekly automated dependency updates (Go modules, npm, GitHub Actions)

### Key Source Files

| File           | Purpose                                                                                                                                                                                                                                                                  |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `filter.go`    | `Filter` type with functional options (`WithFilterOptions`, `WithFS`, `WithIncludePatterns`, `WithExcludePatterns`). `Filter` returns `(bool, error)`, `FilterDetailed` returns `(FilterResult, error)`, `FilterPaths` for batch. Enabled when options/patterns are set. |
| `detection.go` | Core detection logic, `detectors` table (11 entries), `DetectReason`, `DetectReasonReader`, filename/content matchers, trace-aware detection functions, `AllFilterOptions()`, `AllGeneratorOptions()`, `AllFilterReasons()`                                              |
| `types.go`     | `FilterOption` and `FilterReason` types, constants (12 options, 14 reasons), `FilterResult` struct                                                                                                                                                                       |
| `pattern.go`   | `**` glob pattern matching via `doublestar/v4`                                                                                                                                                                                                                           |
| `sqlc.go`      | SQLC config discovery and parsing (v1 and v2 formats, Go/JSON/Codegen output dirs)                                                                                                                                                                                       |
| `errors.go`    | Branded error types with sentinel errors, `ErrorCode` type, `ErrorCoder` interface                                                                                                                                                                                       |
| `project.go`   | Project root discovery                                                                                                                                                                                                                                                   |

### Website

- `website/` — Astro v6 + Starlight marketing/docs site
- Landing page at `/` with hero, features, code examples
- Starlight docs at root level with PageFind search
- Firebase Hosting deployment (firebase.json, .firebaserc)
- CI/CD: `.github/workflows/website.yml`
- Build: `cd website && npm run build`
- Dev: `cd website && npm run dev`
- Type check: `cd website && npx astro check`
- HTML validation: `cd website && npx html-validate 'dist/**/*.html'`

#### Website Patterns

- **URL structure** — Landing page at `/` (from `src/pages/index.astro`), docs at root level (e.g., `/getting-started/installation/` from `src/content/docs/`). No `/docs/` prefix. Starlight docs collection at `src/content/docs/` renders at root. Firebase redirect `/docs/:path*` → `/:path*` (301) handles old indexed URLs. Landing page components link directly to doc paths (e.g., `/getting-started/installation/`). Dead `index.mdx` removed — landing page renders at `/`.
- **`Icon.astro`** — Centralized SVG icon component. Import from `../components/Icon.astro`. Props: `name` (string), `size` (number, default 20). Available icons: Feature icons (lightning, sliders, glob, chart, folder, database), UseCase icons (cog, chart, refresh, bolt, check), UI icons (arrow-external, arrow-right, github, menu, close, sun, moon).
- **Theme system** — Dark mode default. Light mode via `.light` class on `<html>`. Toggle persists to `localStorage`. Initialize with `prefers-color-scheme` as fallback. CSS variables in `src/styles/global.css` under `:root.light`.
- **Type-safe icon keys** — `FeatureIcon` and `UseCaseIcon` exported from `src/data/types.ts` as `as const` + `typeof ...[number]` unions.
- **SEO** — Canonical URL, JSON-LD SoftwareApplication schema, OG meta tags all in `LandingLayout.astro`.
- **Code deduplication** — jscpd v4.0.9 via `scripts/dedup.sh` wrapper (needed because jscpd v4 `formats-exts` is broken for `.astro` files). Script copies `.astro` → `.html` in temp dir, runs jscpd with `--min-lines 2 --min-tokens 20`, remaps paths back. Run: `cd website && npm run dedup`.

#### Lighthouse / Performance

- **Lighthouse / Performance**: Config in `lighthouserc.json` only. Assertions cover performance, accessibility, SEO, best-practices, resource sizes, and timings. Budgets not used (LHCI v12 rejects budgets+assertions together).
- **[unlighthouse.dev/tools](https://unlighthouse.dev/tools)** — Free web tools for performance auditing: Bulk PageSpeed Test, CWV Checker, CWV History, Lighthouse Score Calculator, HAR Viewer, Page Size Checker.
- **[LHCI](https://unlighthouse.dev/learn-lighthouse/lighthouse-ci)** — Automated Lighthouse CI via `treosh/lighthouse-ci-action@v12`. GitHub App token required (`LHCI_GITHUB_APP_TOKEN` secret). Run: `workflow_dispatch` for on-demand or push/PR for continuous audits.
- **TL;DR**: Use the web tools for quick checks. Use the CI workflow for regression tracking. Tighten assertions over time as baselines are established.

## Development Guidelines

### Design Decisions

- **oapi-codegen has no filename heuristic** — `*.gen.go` is not specific to oapi-codegen (used by many generators). Adding it as phase-1 detection would cause false positives. Content-based detection is correct.
- **`ReasonOutsideScope` (was `ReasonIncludePattern`)** — renamed in v0; describes the outcome (file is outside include scope) rather than the mechanism. `ReasonIncludePattern` was misleading: the file was filtered because it did NOT match, not because it matched.
- **SQLC v1 config supported** — `sqlcV1Config` struct maps v1 `packages[].path` to output dirs. Version dispatch in `unmarshalSQLCConfig` routes v1 to `parseV1AsV2` which converts to v2 format. Unsupported versions return a parse error.
- **`Error()` uses `fmt.Sprintf`** — 228ns on cold path (error formatting). `strings.Builder` optimization is not worth the complexity.
- **art-dupl known false positive** — `unmarshalSQLCConfig` and `parseV1AsV2` in `sqlc.go` share identical signatures `([]byte, string) → (*sqlcConfig, *SQLCConfigError)` but are fundamentally different functions (version dispatch vs v1→v2 conversion). Art-dupl's structural matching flags them; fixed via `--exclude-pattern 'sqlc.go'` in the dedup command.
- **`errors.AsType` migration (Go 1.26)** — All code and tests use `errors.AsType[T]` exclusively. The `assertErrorType[T error]` helper in `errors_test.go` wraps `errors.AsType` for test ergonomics.
- **`FilterResult` is additive, not replacing** — `Filter()` returns `(bool, error)` unchanged. `FilterDetailed()` returns `(FilterResult, error)` with trace info. No breaking changes to existing API.
- **`FilterOption.Reason()` returns `(FilterReason, bool)`** — Previously panicked on `FilterAll`. Now returns `("", false)` for meta-options. This is the correct Go pattern — no panics in library code.
- **`AllGeneratorOptions()` vs `AllFilterOptions()`** — `AllFilterOptions()` includes `FilterAll` (for validation). `AllGeneratorOptions()` excludes `FilterAll` (for enumeration). Both are derived from the detectors table.
- **Metrics removed** — Stats aggregation is the caller's responsibility. `FilterDetailed()` and `FilterPaths()` return per-call results with all the data callers need.
- **Phantom types removed** — `StartPath`, `ConfigPath`, `Operation`, `ErrorMessage` provided zero compile-time safety. Error struct fields are now plain `string`.
- **Context methods removed** — `FilterContext`, `FilterDetailedContext`, `FilterPathsContext` promised cancellation over synchronous I/O. They were lies.
- **Error system simplified** — Removed `errorCodeDefs` table, `AllErrorCodes()`, `CodeHelp()`, `Helper` interface, `CodeEqual[T]`. Kept `ErrorCode` type, `ErrorCoder` interface, sentinel errors, branded prefix. Is() methods use direct `.Code == .Code` comparison.
- **Detection helpers unexported** — `MatchesSQLCFilename`, `HasSQLCContent`, `HasSQLCCodePatterns` are internal helpers. Users should use `DetectReason()` or `Filter`.
- **`codeGeneratedPrefix` moved to `detection.go`** — Only used there, not in `types.go`.
- **`detectorOptions(bool)` merged** — Replaces `allSpecificOptions()` + `allDetectorOptions()` with one function.
- **Trace/non-trace unified** — `*WithTrace` variants are canonical implementations; non-trace versions (`getFilenameBasedReason`, `getContentBasedReason`, `detectReasonFS`) are thin wrappers that discard the trace string. Eliminates the biggest source of code duplication.
- **`coverage_test.go` dissolved** — Tests moved to their natural test files (`errors_test.go`, `filter_test.go`, `pattern_test.go`, `sqlc_test.go`, `project_test.go`). No more catch-all coverage file.

### Testing

- Use table-driven tests where possible
- Use `t.Parallel()` within `t.Run()` for proper test isolation
- Generic helper functions in `helpers_test.go`: `assertFieldEqual[T]()`, `boolTestCase[T]`, `runBoolTableTest[T]()`. Error extraction helper `assertErrorType[T]()` is in `errors_test.go` (uses `errors.AsType` from Go 1.26).
- **BDD tests**: ~120 ginkgo specs in `bdd_test.go`. Use `onsi/ginkgo/v2` + `onsi/gomega`. Patterns: `ginkgo.DescribeTable` for table-driven BDD, `ginkgo.BeforeEach` for FS setup, `gomega.Expect` with matchers.
- **Error path tests**: Previously in `coverage_test.go`, now distributed to dedicated test files (`errors_test.go`, `filter_test.go`, `sqlc_test.go`, `pattern_test.go`, `project_test.go`).
- Run tests with: `go test ./...`
- **Coverage**: 99.8% (only untestable `filepath.Abs` error path in `FindProjectRoot` remains below 100%)

### Linting

- Uses golangci-lint v2 with comprehensive configuration
- Run: `golangci-lint run`

### Code Organization

This is a library project, so the main package resides at the root level. This follows standard Go conventions for library packages.

## Commands

```bash
# Run tests
go test ./...

# Run tests with race detector
go test -race ./...

# Run linter
golangci-lint run

# Detect code duplication (excludes testdata/moq - generated mock code; sqlc.go - known false positive: function signature collision)
art-dupl --semantic -t 15 --exclude-pattern 'testdata/moq/**' --exclude-pattern 'sqlc.go'

# Website: detect code duplication (jscpd via wrapper script)
cd website && npm run dedup
```

## CI

Four separate GitHub Actions workflows, all triggered on push to master with path filters + `workflow_dispatch` for manual triggering (CI and Website also run on PRs):

**Go CI** (`.github/workflows/ci.yml`):

- Path filters: `*.go`, `go.mod`, `go.sum`, `testdata/**`, `.golangci.*`
- Concurrency group cancels in-progress runs
- `go vet` → tests with race detector and coverage (98% threshold) → benchmarks
- govulncheck job (parallel with lint)
- golangci-lint (separate job, parallel, uses `gomodguard_v2`)
- Uses `actions/setup-go@v6` (Node.js 24)

**Benchmark** (`.github/workflows/benchmark.yml`):

- Push to master only (no PR trigger — avoids `contents: write` permission on fork PRs)
- Path filters: `*.go`, `go.mod`, `go.sum`, `.github/workflows/benchmark.yml`
- `workflow_dispatch` enabled
- `go test -bench=. -benchmem` → `benchmark-action/github-action-benchmark@v1`
- Pushes benchmark data to `gh-pages` branch (`dev/bench/` directory)
- Alert threshold: 150%, fail threshold: 300%

**Website** (`.github/workflows/website.yml`):

- Path filters: `website/**`, `.github/workflows/website.yml`, `.github/workflows/lighthouse.yml`, `lighthouserc.json`
- `workflow_dispatch` enabled
- Concurrency group cancels in-progress runs
- `npm ci` → `astro check` (typecheck) → build → doc validation (md-go-validator, optional) → HTML validation (enforced, not suppressed) → code dedup check
- Import path validation: ensures all `gogenfilter` imports include `/v3`
- Stale reference detection: checks curated docs for references to deleted files
- CHANGELOG sync check: verifies version sections match between root and website
- Cross-repo checkout for `LarsArtmann/md-go-validator` uses `secrets.PRIVATE_REPO_TOKEN` with `github.token` fallback; `continue-on-error: true` so build/deploy proceeds even without access. `LarsArtmann/go-output` checkout removed (was unused).
- Deploy to Firebase Hosting (master push only, least-privilege permissions)
- Node version pinned via `website/.node-version` (currently Node 24)

**Lighthouse CI** (`.github/workflows/lighthouse.yml`):

- Uses `treosh/lighthouse-ci-action@v12` (official LHCI, 1.2k+ stars, collaborated with Google Chrome team)
- **Prerequisite**: Install [Lighthouse CI GitHub App](https://github.com/apps/lighthouse-ci) for the repo, then add the token as `LHCI_GITHUB_APP_TOKEN` secret
- Triggers: push/PR to master (when website/config files change), plus `workflow_dispatch` for on-demand
- Scans: `https://gogenfilter.web.app/` (root, docs, API, changelog) — 3 runs per URL for stability
- Assertions: `lighthouse:no-pwa` preset + permissive custom thresholds (performance: warn≥0.8, accessibility: error≥0.8, SEO/best-practices: error≥0.9)
- Uploads results to temporary public storage + artifacts (14-day retention)
- Config in `lighthouserc.json` only (budgets NOT used — LHCI v12 rejects budgets+assertions together)

### CI Known Issues (2026-05-04)

- **Website CI**: `PRIVATE_REPO_TOKEN` secret optional — md-go-validator checkout has `continue-on-error: true`, doc validation is skipped gracefully when unavailable
- **Lighthouse CI**: Accessibility assertions fail on live site — `color-contrast`, `label-content-name-mismatch` on root page; `redirects` on `/docs`
- **Lighthouse CI**: `LHCI_GITHUB_APP_TOKEN` secret not configured — GitHub status checks skipped

## Key API Patterns

```go
// Functional options configuration
f := gogenfilter.NewFilter(
    gogenfilter.WithFilterOptions(gogenfilter.FilterAll),
)

// Filter returns (bool, error) — I/O errors propagate
filtered, err := f.Filter("file.go")

// FilterPaths returns ([]bool, error) — batch filtering
results, err := f.FilterPaths([]string{"a.go", "b.go", "c.go"})

// Variadic DetectReason (no I/O)
reason := gogenfilter.DetectReason("file.go", content,
    gogenfilter.FilterSQLC, gogenfilter.FilterGeneric,
)

// Detailed result with trace info
result, err := f.FilterDetailed("file.go")
fmt.Printf("filtered=%v reason=%s trace=%s\n", result.Filtered, result.Reason, result.Trace)

// Batch filtering
results, err := f.FilterPaths([]string{"a.go", "b.go", "c.go"})
```

## Dependencies

- `github.com/bmatcuk/doublestar/v4` - `**` glob pattern matching
- `github.com/go-faster/yaml` - YAML parsing for SQLC config
- `github.com/onsi/ginkgo/v2` - BDD testing framework (test-only)
- `github.com/onsi/gomega` - BDD matchers (test-only)

## Gotchas

- **`/v3` import path** — Module is `github.com/LarsArtmann/gogenfilter/v3`. All docs, website source, and README must reference `/v3`. CI validates this.
- **`.gitignore` filtering is out of scope** — Rejected (2026-05-27): would require alpha dependency (`go-git/v6`) and blur the library's identity from "generated code detector" to "general file filterer". Use `WithExcludePatterns("vendor/**", "**/testdata/**")` for common exclusions, or pre-filter with any gitignore library before passing to gogenfilter. Documented in `website/src/content/docs/guides/gitignore-pre-filtering.mdx`.
- **BuildFlow `todo-check`** — Detects `note:` as a `NOTE:` comment marker. Use `hint` instead of `note` for TypeScript property names to avoid false positives.
- **`.buildflow.yml`** — Configures BuildFlow with project-specific excludes (testdata, website/dist, website/node_modules).
- **Dependabot alerts** — All 4 alerts are npm ecosystem (website transitive deps), not Go production deps. The `yaml` alert is npm `yaml`, not `go-faster/yaml`.
- **`gomodguard` deprecated** — Replaced by `gomodguard_v2` in `.golangci.yaml`.
- **`docs/status/archive/`** — Historical status reports (pre-May-25) are archived here. Active reports remain in `docs/status/`.

## License

MIT
