# gogenfilter

_A Go library for detecting and filtering auto-generated code files._

## Overview

This project provides detection and filtering capabilities for auto-generated Go code files from tools like sqlc, templ, go-enum, protobuf, oapi-codegen, deepcopy-gen, wire, moq, mockgen, and stringer. It's designed as a library for use by linters and static analysis tools.

## Architecture

- **Two-phase detection**: filename-based (zero I/O) then content-based (reads file)
- **Table-driven detector system**: `[]detector` slice with `option`, `reason`, `matchFilename`, and `checkContent` fields (18 detectors)
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
- `.github/dependabot.yml`: Weekly automated dependency updates (Go modules, pnpm, GitHub Actions)

### Key Source Files

| File           | Purpose                                                                                                                                                                                                                                                                                                                                                                                       |
| -------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `doc.go`       | Package documentation — overview, Quick Start examples, generator list                                                                                                                                                                                                                                                                                                                        |
| `filter.go`    | `Filter` type with functional options (`WithFilterOptions`, `WithFS`, `WithIncludePatterns`, `WithExcludePatterns`). `Filter` returns `(bool, error)`, `FilterDetailed` returns `(FilterResult, error)`, `FilterDetailedAndContent` returns `(FilterResult, []byte, error)` (lazy read + content return), `FilterPaths` for batch. Enabled when options/patterns are set.                     |
| `detection.go` | Core detection logic, `detectors` table (18 entries with doc metadata), `DetectReason`, `DetectReasonReader`, `DetectReasonFile`, filename/content matchers, trace-aware detection functions, `AllFilterOptions()`, `AllGeneratorOptions()`, `AllFilterReasons()`, `AllDetectorDocs()`                                                                                                        |
| `types.go`     | `FilterOption` and `FilterReason` types, constants (19 options, 21 reasons), `FilterResult` struct                                                                                                                                                                                                                                                                                            |
| `pattern.go`   | `**` glob pattern matching via `doublestar/v4`                                                                                                                                                                                                                                                                                                                                                |
| `scan.go`      | `ScanProject` — walks `fs.FS`, detects generated files, returns `ScanResult` with per-generator lists, `Exclusion` patterns, `ExclusionPattern()` method on `FilterReason`                                                                                                                                                                                                                    |
| `sqlc.go`      | SQLC config discovery and parsing (v1 and v2 formats, Go/JSON/Codegen output dirs)                                                                                                                                                                                                                                                                                                            |
| `errors.go`    | Branded error types with sentinel errors, `ErrorCode` type, `SQLCOperation` type, `ErrorCoder` interface                                                                                                                                                                                                                                                                                      |
| `project.go`   | Project root discovery                                                                                                                                                                                                                                                                                                                                                                        |
| `cmd/gendocs/` | Documentation generator binary. Reads `AllDetectorDocs()` from the detectors table, emits `website/src/data/generators.json`, README.md tables, `generators.mdx` detection table + tool count, `detection.mdx` per-generator function table, and `doc.go` generator list. Wired via `//go:generate` in `detection.go`. CI enforces freshness via `go generate ./... && git diff --exit-code`. |
| `plugin/`      | golangci-lint v2 module plugin. Separate Go module (`github.com/LarsArtmann/gogenfilter/plugin`) implementing `register.LinterPlugin`. Detects generated files via gogenfilter and reports diagnostics. Has its own `go.mod` with `replace github.com/LarsArtmann/gogenfilter/v3 => ../` for local development. Tests: `cd plugin && GOWORK=off go test ./...`.                               |

### Website

- `website/` — Astro v6 + Starlight marketing/docs site
- Landing page at `/` with hero, features, code examples
- Starlight docs at root level with PageFind search
- Firebase Hosting deployment (firebase.json, .firebaserc)
- CI/CD: `.github/workflows/website.yml`
- Build: `cd website && pnpm run build`
- Dev: `cd website && pnpm run dev`
- Type check: `cd website && pnpm dlx astro check`
- HTML validation: `cd website && pnpm dlx html-validate 'dist/**/*.html'`

#### Website Patterns

- **URL structure** — Landing page at `/` (from `src/pages/index.astro`), docs at root level (e.g., `/getting-started/installation/` from `src/content/docs/`). No `/docs/` prefix. Starlight docs collection at `src/content/docs/` renders at root. Firebase redirect `/docs/:path*` → `/:path*` (301) handles old indexed URLs. Landing page components link directly to doc paths (e.g., `/getting-started/installation/`). Dead `index.mdx` removed — landing page renders at `/`.
- **`Icon.astro`** — Centralized SVG icon component. Import from `../components/Icon.astro`. Props: `name` (string), `size` (number, default 20). Available icons: Feature icons (lightning, sliders, glob, chart, folder, database), UseCase icons (cog, chart, refresh, bolt, check), UI icons (arrow-external, arrow-right, github, menu, close, sun, moon).
- **Theme system** — Dark mode default. Three-state toggle (light/dark/auto) via `data-theme` attribute on `<html>` (unified with Starlight's convention). `"auto"` follows `prefers-color-scheme` and updates reactively. Toggle persists to `localStorage["starlight-theme"]` (old `"theme"` key read as backward-compat fallback). CSS variables in `src/styles/global.css` under `:root[data-theme="light"]`. Icons: sun (light), moon (dark), monitor (auto) in `Icon.astro`.
- **Type-safe icon keys** — `FeatureIcon` and `UseCaseIcon` exported from `src/data/types.ts` as `as const` + `typeof ...[number]` unions.
- **SEO** — Canonical URL, JSON-LD SoftwareApplication schema, OG meta tags all in `LandingLayout.astro`.
- **Code deduplication** — jscpd v4.0.9 via `scripts/dedup.sh` wrapper (needed because jscpd v4 `formats-exts` is broken for `.astro` files). Script copies `.astro` → `.html` in temp dir, runs jscpd with `--min-lines 2 --min-tokens 20`, remaps paths back. Run: `cd website && pnpm run dedup`.
- **CSP (Content Security Policy)** — Astro v6 built-in CSP (`security.csp` in `astro.config.mjs`). CSP is NOT in Firebase headers — Firebase only sets non-CSP security headers (HSTS, X-Frame-Options, etc.). Post-build `scripts/fix-csp.mjs` handles two Astro gaps: (1) **Style hash stripping** — Astro auto-hashes inline `<style>` blocks into `style-src`, making `'unsafe-inline'` ineffective. Per the CSP spec, when a source list contains ANY hash/nonce, `'unsafe-inline'` is ignored entirely — this silently breaks **expressive-code syntax highlighting** (token spans use inline `style="--0:#color;--1:#color"`). `fix-csp.mjs` robustly strips ALL hash/nonce sources from `style-src` (any type/order) so `'unsafe-inline'` stays effective for Starlight's `style=""` attributes AND expressive-code tokens. (2) **Missing script hashes** — Starlight injects non-module inline scripts (ThemeProvider, Search modal, SidebarPersister) after Astro's hash phase. The script computes SHA-256 hashes and appends them to `script-src`. Our own source code has **zero inline styles** — all converted to Tailwind classes. No `<style>` blocks in our components. If adding new inline scripts, run `pnpm run build` and the script will auto-hash them.

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
- **Content-only checkContent (v3.4.0)** — The detector table's `checkContent` field is content-only (no filename gate) for all detectors. Four detectors (SQLC, Templ, GoEnum, Protobuf) previously had filename gates baked into their `Is*Generated` functions, which were used as `checkContent`. This made phase-2 detection ineffective for files with non-standard names (e.g., a sqlc file named `repository.go` instead of `models.go`). The fix: `checkContent` now calls content-only wrappers (`checkSQLCContent`, `checkTemplContent`, `checkGoEnumContent`, `checkProtobufContent`) that use specific generation comment markers (`"Code generated by sqlc"`, `"Code generated by templ"`, etc.). The public `Is*Generated` functions remain filename-gated for backward compatibility — they represent the strict "filename AND content" check.
- **`codeGeneratedPrefix` moved to `detection.go`** — Only used there, not in `types.go`.
- **`detectorOptions(bool)` merged** — Replaces `allSpecificOptions()` + `allDetectorOptions()` with one function.
- **Trace/non-trace unified** — `*WithTrace` variants are canonical implementations; non-trace versions (`getFilenameBasedReason`, `getContentBasedReason`, `detectReasonFS`) are thin wrappers that discard the trace string. Eliminates the biggest source of code duplication.
- **CSP via Astro meta tags + post-build fixes** — Starlight injects inline scripts (ThemeProvider, ThemeSelect, Search, SidebarPersister, Tabs). Firebase `script-src 'self'` blocks these since static hosting can't generate nonces. Solution: Astro v6 built-in CSP (`security.csp`) auto-computes SHA-256 hashes at build time and emits `<meta>` tags. Firebase headers only set non-CSP security headers (HSTS, X-Frame-Options, etc.). However, Astro has two gaps: (1) it auto-hashes inline `<style>` blocks into `style-src`, making `'unsafe-inline'` ineffective (CSP Level 2 spec); (2) Starlight's non-module inline scripts are injected after Astro's hash phase. Post-build `scripts/fix-csp.mjs` strips style hashes and computes missing script hashes. Our own components use **zero inline styles** — all converted to Tailwind classes with arbitrary value syntax (e.g. `fill-[var(--color-accent)]`).
- **`SQLCOperation` typed constants** — `SQLCOperation string` type with `OpSQLCFind`, `OpSQLCWalk`, `OpSQLCRead`, `OpSQLCCollect`, `OpSQLCParse` constants. `SQLCConfigError.Operation` field is now `SQLCOperation` instead of raw `string`. Follows same pattern as `ErrorCode`, `FilterOption`, `FilterReason`.
- **`coverage_test.go` dissolved** — Tests moved to their natural test files (`errors_test.go`, `filter_test.go`, `pattern_test.go`, `sqlc_test.go`, `project_test.go`). No more catch-all coverage file.
- **Firebase deploy uses `firebase-tools` CLI directly** — Replaced `FirebaseExtended/action-hosting-deploy@v0` (Node 20 runtime, deprecated) with direct `firebase deploy` via `firebase-tools` pnpm package running under Node 24. Uses `GOOGLE_APPLICATION_CREDENTIALS` with service account key for auth.
- **Website a11y fixes** — `dependents.astro` table: added `<caption>`, replaced bare `★` header with `aria-hidden` + `sr-only` text, added `aria-label` to star count cells.
- **pnpm overrides audited (2026-08-10)** — All 4 overrides in `website/package.json` are redundant after the Astro v7 bump. **`brace-expansion`**: zero references in lockfile (dead). **`devalue`**: astro's `^5.8.1` range already guarantees the safe version; exact pin blocks patches. **`vite`**: astro's `^8.0.13` forces vite 8; pin only blocks patches. **`yaml`**: `yaml-language-server` pins 2.8.3 exact. All safe to remove, but requires `pnpm install` to regenerate lockfile (not available in all dev envs).
- **Zero inline styles in website source** — All `style=""` attributes and `<style>` blocks eliminated from our components. `dependents.astro` uses Tailwind arbitrary values (`border-[var(--sl-color-gray-5)]`). `Header.astro` mobile nav uses `max-sm:` responsive utilities + `[&.open]:max-sm:flex` for JS toggle. Only Starlight's built-in SVG `style=""` attributes remain (unavoidable, covered by `style-src 'unsafe-inline'`).
- **Visual design system (3-color accent)** — The site uses a warm three-color accent system, not a single hue: cyan (`--color-accent`) is the brand/primary, amber (`--color-amber`) is the warm secondary (human-written code, the "content" phase), emerald (`--color-success`) is the positive outcome (filtered/detected). Feature cards and use-case cards rotate through `accent`/`amber`/`success` via an `AccentColor` field on `Feature`/`UseCase` in `src/data/types.ts` — this was added specifically to break the monochrome feedback. Sections alternate between `bg-bg-primary` and `--color-bg-raised` via the `tone` prop on `Section.astro` for visual rhythm.
- **Color contrast / a11y pass** — All text/accent tokens verified against WCAG AA (≥4.5:1) on every surface they appear on (bg-primary, bg-raised, bg-card-solid, bg-code). Key decisions: (1) **`--color-on-accent` token** (`#0c0a09` dark, `#ffffff` light) — text color for elements on accent backgrounds; ALL `bg-accent` buttons/links use `text-on-accent`, never `text-bg-primary` or `text-white` directly (white on dark-mode bright cyan #22d3ee was 1.81:1, failed AA). (2) **`--color-text-muted`** — warm stone family (`#8e8884` dark, `#66605a` light) so it harmonizes with the warm stone backgrounds (previous `#9ca3af`/`#6b7280` were cool Tailwind gray). PASSES AA on all surfaces in BOTH modes. (3) **Light-mode `--color-accent: #0e7490`** (cyan-700) — kept darker than starlight.css's previous `#0891b2` because `#0891b2` on white = 3.53:1 (FAILS AA for text use). Starlight aligned to `#0e7490` too. (4) **Light-mode `--color-accent-hover: #155e75`** (cyan-800, DARKER) — opposite direction from dark mode's hover (which lightens) because light mode uses white text on the button; darkening on hover preserves AA contrast (white on `#155e75` = 7.27:1 ✓) and gives proper "press" affordance. Dark mode lightens because dark text on brighter cyan still gains contrast. (5) **`<meta name="theme-color">`** — two tags with `prefers-color-scheme` media queries (`#0c0a09` dark, `#fafaf9` light); `header.js` syncs both metas on manual theme toggle (covers case where user's manual `.light` class state diverges from OS preference). (6) **Logo gradient stops stay hardcoded** `#22d3ee`/`#0891b2` — brand identity doesn't change with theme. (7) **HeroSection/ComparisonSection hardcoded dracula hexes stay** — they intentionally match expressive-code's dracula theme (`#bd93f9` purple, `#f1fa8c` yellow, etc.); design-system tokens (success/amber/code-comment) are for UI chrome, not code content. (8) **ComparisonSection panel bg `bg-red-500/5` → `bg-[#ff5555]/5`** — was leaking Tailwind's red palette; now matches dracula red used elsewhere in same panel.
- **Expressive Code theme is `dracula`** (dark) + `github-light` (light) — switched from the muted `github-dark` to vivid dracula so syntax highlighting is unmistakable and adds color variety. The hero code block on the landing page is a CUSTOM (non-EC) `<pre>` that hand-replicates dracula colors via Tailwind arbitrary values (`text-[#bd93f9]` purple, `text-[#f1fa8c]` yellow, `text-[#8be9fd]` cyan, `text-[#50fa7b]` green, `text-[#ff79c6]` pink, `text-[#6272a4]` comment) so it matches the docs blocks.
- **Logo is a funnel** — `Logo.astro` / `favicon.svg` / `public/logo.svg` render a funnel mark (the literal concept: gogenfilter is a filter) on a cyan→teal gradient, not the old "FP" letters. Header uses 40px, footer 28px.
- **v3.2.0: `DetectReasonFile`/`DetectReasonFileFS`** — Standalone functions (not methods on Filter) for two-phase detection in one call. Eliminates 3 consumers' duplicated detection code. Uses `os.DirFS(".")` for no-FS variant, consistent with Filter's default.
- **v3.2.0: `FilterWithContent`/`FilterDetailedWithContent`** — Methods on Filter that accept pre-read content. Bypasses `detectReasonFS` and calls `detectReasonFromMap` directly. Fixes double I/O for analyzers that already have file content.
- **`FilterDetailedAndContent` (lazy content-return API)** — Like `FilterDetailed` but returns `(FilterResult, []byte, error)` — the file content read during phase-2 is returned to the caller instead of discarded. Content is `nil` when no read occurred (disabled filter, include/exclude pattern match, phase-1 filename match, or no content-check detectors enabled). When non-nil, the content was read exactly once from `f.fsys`. This closes the gap between `FilterDetailed` (lazy read, discards content) and `FilterDetailedWithContent` (caller provides content, no lazy read): callers needing the content for post-detection logic (e.g., art-dupl's `allowsContent` override for `ReasonGeneric` classification) get a single read with no double-read. Follows the `FilterDetailedWithContent` pattern of checking both include and exclude patterns inline (unlike `FilterDetailed` which uses the either/or includes-vs-excludes split).
- **v3.2.0: `ScanProject`** — Walks `fs.FS`, detects generated files, returns `ScanResult`. Creates internal Filter from configs, defaults to `FilterAll`. Eliminates ~200 lines of reimplemented scanner in golangci-lint-auto-configure.
- **v3.2.0: `ExclusionPattern()` on `FilterReason`** — Returns `(string, bool)`. Generators with consistent filename conventions (templ, protobuf, go-enum, wire, moq, mockgen, stringer, mockery, easyjson, counterfeiter, sqlc) get fixed regex patterns. Generators with configurable output (oapi-codegen, ent, gqlgen, go-swagger, generic, msgp) return `false`. msgp excluded because `_gen.go` is too generic (matches `wire_gen.go`, etc.). SQLC was updated from `false` to `\.sql\.go$` — see config-aware detection note below.
- **v3.2.0: 7 new detectors** — mockery, ent, gqlgen, easyjson, msgp, counterfeiter, go-swagger. msgp uses content-only detection (`_gen.go` suffix too generic). mockery exclusively owns `mock_` prefix (mockgen only matches `_mock.go` suffix). counterfeiter uses `fake_` prefix. gqlgen is content-only (`generated.go` too generic). All follow existing table-driven pattern.
- **`exclusionPatterns` package-level var** — Moved from local map literal in `ExclusionPattern()` method to package-level immutable var. Avoids map allocation on every call.
- **`detectReasonFromMap` empty content guard** — Skips content-based detection when `content == ""`. Previously called `needsContentCheck` + `getContentBasedReasonWithTrace` even with empty content, wasting iterations.
- **`FilterResult.Is(reason) bool`** — Ergonomic reason check: `result.Is(ReasonSQLC)` instead of `result.Filtered && result.Reason == ReasonSQLC`.
- **`String()` on `GeneratedFile`, `Exclusion`, `ScanResult`** — Debug/logging ergonomics for scan results.
- **`readFile` lint suppression** — `#nolint:gosec` for G304 false positive (library reads from `fs.FS` by design). `#nolint:wrapcheck` for `os.ReadFile` fallback (caller wraps with context). Error from `fs.ReadFile` now wrapped with `fmt.Errorf("read file from fs: %w", err)`.
- **Documentation generation pipeline (gendocs)** — The `detectors` table in `detection.go` is the single source of truth for generator data. `cmd/gendocs/main.go` reads `AllDetectorDocs()` and generates five outputs: `website/src/data/generators.json` (consumed by Astro), README.md tables (between `<!-- gendocs:*:start/end -->` markers), `generators.mdx` detection table + tool count (between `{/* gendocs:*:start/end */}` MDX markers), `detection.mdx` per-generator function table (between MDX markers), and `doc.go` generator list (between `// gendocs:*:start/end` markers). `websiteMetadata` in `cmd/gendocs/main.go` holds presentation-only data (logos, display filenames) not derivable from Go source. Adding a detector WITHOUT adding website metadata causes gendocs to fail (validation in `validateMetadata`). CI `docs` job enforces freshness: `go generate ./... && git diff --exit-code`. Run via `nix run .#gendocs` or `go run ./cmd/gendocs`.
- **Detector doc metadata fields** — Each detector in the `detectors` table has `url`, `filenameDesc`, `contentDesc`, and `isFuncName` string fields. These are intrinsic documentation properties of each generator, not detection logic. They power `AllDetectorDocs()` → `DetectorDoc` struct consumed by `gendocs`. The `isFuncName` field records the exported `Is*Generated` function name for the per-generator table in `detection.mdx`.
- **API reference pages replaced by pkg.go.dev** — The 4 hand-written API pages (`api/filter.mdx`, `api/scan.mdx`, `api/types.mdx`, `api/errors.mdx`) were deleted. pkg.go.dev auto-generates this content from Go doc comments. Only `api/detection.mdx` remains — it has unique hand-written content (code examples, usage notes) plus the gendocs-generated per-generator function table. Sidebar links to pkg.go.dev for full API reference.
- **`replaceSectionInline` for mid-line markers** — The `{/* gendocs:count:start/end */}` markers in `generators.mdx` appear mid-sentence (inline tool count). `replaceSection` adds newlines after the start marker, which would break the sentence. `replaceSectionInline` replaces content without adding newlines. Used only for the count marker; all other markers use `replaceSection`.
- **`formatMarkdownTable` centralized table formatter** — All 4 gendocs table outputs (README generators, README filter options, detection.mdx per-generator, generators.mdx detection table) share one `formatMarkdownTable(rows [][]string) string` function. Column widths are calculated dynamically from the widest cell in each column — no hardcoded width constants. The first row is the header, followed by a separator row of dashes. Pipe characters in cell content are escaped (`|` → `\|`) by `markdownRow` after padding, so column widths are based on raw cell length. The `makezero` nolint directives on the `widths`, `cells`, and `sep` slices are intentional: these slices are accessed by index, never appended to.
- **Config-aware sqlc detection (phase 1.5)** — Between filename (phase 1) and content (phase 3) detection, the Filter lazily parses all reachable `sqlc.yaml`/`sqlc.yml` configs and builds a `sqlcDerivedConfig` mapping output dirs to accepted filenames (defaults + custom `output_*_file_name`). A file like `db/models.go` is classified as sqlc-generated iff it lives in a configured output dir — even without a header comment. This eliminates false positives on hand-written files with common names (e.g., `pkg/batch.go`) outside configured dirs. The derived config is cached for the filter's lifetime via `atomic.Pointer[sqlcDerivedConfig]` with `CompareAndSwap`. On parse error, the Filter falls back to an empty derived config (headerContent still works). Only active on Filter, NOT on standalone `DetectReason`/`DetectReasonFile` (they have no project root context).
- **`sqlcDerivedConfig` type** — Maps each declared output dir (cleaned, slash-separated, relative to FS root) to a `map[string]bool` of accepted base filenames. Built once by `sqlcDerivedConfigForFS` (walks FS, parses configs via `FindSQLCConfigsFS` + `parseSQLCConfigFS`, merges via `mergeSQLCConfig`). Skips missing/unparseable configs gracefully.
- **`sqlcDefaultFileNames` constants** — The fixed output filenames (`db.go`, `models.go`, `querier.go`, `batch.go`, `copyfrom.go`) are defined as typed `const` (`sqlcFileDB`, `sqlcFileModels`, etc.) in `sqlc.go`. `configuredSQLCFileNames` resolves defaults vs custom names via `sqlcCustomFileName` helper. Single canonical source — detection must not duplicate.
- **`configOrFilenameResult` shared helper** — Extracted from duplicated logic in `FilterDetailedAndContent` and `shouldFilterDetailedByContent`. Checks config-aware SQLC detection then filename-based detection, returning `(FilterResult, bool)`. Reduces `FilterDetailedAndContent` from 66 to ~50 lines (funlen limit 60).
- **`sqlcConfigTrace` constant** — The trace string `"detected as sqlc via configured output directory"` used in `detectReasonFSWithConfig`, `configOrFilenameResult`, and (previously) inline in `FilterDetailedAndContent`/`shouldFilterDetailedByContent`. Single constant in `detection.go`.
- **SQLC exclusion pattern updated** — `ReasonSQLC.ExclusionPattern()` now returns `(\.sql\.go$, true)` instead of `("", false)`. `ScanProject` derives a fixed regex exclusion for sqlc files. scan.go comments updated to remove sqlc from the "directory-based derivation" list.
- **Tagliatelle configured for snake_case yaml** — `.golangci.yaml` now sets `tagliatelle.case.rules.yaml: snake` to accept snake_case yaml struct tags. sqlc's real config format uses snake_case keys (e.g., `output_batch_file_name`), so the yaml tags MUST match. Pre-existing single-word tags (`yaml:"version"`, `yaml:"out"`) are valid snake_case and unaffected.
- **Test lint exclusions** — `goconst`, `gocyclo`, and `maintidx` added to `_test.go` exclusions alongside existing `cyclop`, `funlen`, `gocognit`. Test fixture strings appearing 3+ times and table-driven test complexity are expected and provide zero safety benefit as constants. `varnamelen` `ignore-names` extended with `tc` and `f` (standard Go test variable names).
- **`FileReadError` branded type** — All file I/O failures during detection return `*FileReadError` (code `file_read`, sentinel `ErrFileRead`) instead of raw `fmt.Errorf`. The `readFile` helper is the single branding point; callers pass the error through without re-wrapping. `DetectReasonReader` also returns `FileReadError` for `io.ReadAll` failures. This ensures every error from the public API carries the `[gogenfilter:...]` prefix and supports `errors.Is`/`errors.AsType`.
- **`NewFilter` brands joined errors** — `errors.Join(errs...)` is wrapped in `&FilterConfigError{Code: CodeInvalidFilterOption, Err: ...}` so the top-level error from `NewFilter` is always branded. `FilterConfigError.Error()` handles empty `Option` (shows "invalid filter configuration" instead of `invalid filter option ""`). The individual inner errors remain discoverable via `errors.Is`/`errors.AsType` through the `errors.Join` unwrap chain.
- **`ScanError` branded type** — All `ScanProject` failures return `*ScanError` (codes `scan_config`, `scan_walk`, sentinels `ErrScanConfig`/`ErrScanWalk`) instead of raw `fmt.Errorf`. Scan phase constants (`scanPhaseConfigure`, `scanPhaseCreateFilter`, `scanPhaseCollect`, `scanPhaseWalk`) used for the `Phase` field.
- **No `samber/oops` dependency** — The project uses its own branded error system, not `samber/oops`. The `erraudit` tool's "project enforces samber/oops" is a false assumption. Error types: `ProjectRootError`, `FilterConfigError`, `FileReadError`, `ScanError`, `SQLCConfigError` — all implement `ErrorCoder` with sentinel errors for `errors.Is`.
- **golangci-lint plugin is a separate module** — `plugin/` has its own `go.mod` (`github.com/LarsArtmann/gogenfilter/plugin`) to isolate `golangci/plugin-module-register` and `golang.org/x/tools/go/analysis` dependencies from the main library. Uses `replace github.com/LarsArtmann/gogenfilter/v3 => ../` for local development. Plugin implements `register.LinterPlugin` with `LoadModeSyntax` (only needs AST comments + filenames, no type info). Creates the gogenfilter Filter in `BuildAnalyzers()` (once per init, not per package). See ADR 004.

### Testing

- Use table-driven tests where possible
- Use `t.Parallel()` within `t.Run()` for proper test isolation
- Generic helper functions in `helpers_test.go`: `assertFieldEqual[T]()`, `boolTestCase[T]`, `runBoolTableTest[T]()`. Error extraction helper `assertErrorType[T]()` is in `errors_test.go` (uses `errors.AsType` from Go 1.26).
- **BDD tests**: ~120 ginkgo specs in `bdd_test.go` + `bdd_extended_test.go`. Use `onsi/ginkgo/v2` + `onsi/gomega`. Patterns: `ginkgo.DescribeTable` for table-driven BDD, `ginkgo.BeforeEach` for FS setup, `gomega.Expect` with matchers.
- **Error path tests**: Previously in `coverage_test.go`, now distributed to dedicated test files (`errors_test.go`, `filter_test.go`, `sqlc_test.go`, `pattern_test.go`, `project_test.go`).
- Run tests with: `go test ./...`
- **Coverage**: 98.3% (only untestable `filepath.Abs` error path in `FindProjectRoot` remains below 100%)

### Linting

- Uses golangci-lint v2 with comprehensive configuration
- Run: `golangci-lint run`

### Code Organization

This is a library project, so the main package resides at the root level. This follows standard Go conventions for library packages.

## Commands

> See [RELEASING.md](RELEASING.md) for the full release runbook (version bump, tag, push, verify).

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
- `pnpm install --frozen-lockfile` → `astro check` (typecheck) → build → doc validation (md-go-validator, optional) → HTML validation (enforced, not suppressed) → code dedup check
- Import path validation: ensures all `gogenfilter` imports include `/v3`
- Stale reference detection: checks curated docs for references to deleted files
- CHANGELOG sync check: verifies version sections match between root and website
- Cross-repo checkout for `LarsArtmann/md-go-validator` uses `secrets.PRIVATE_REPO_TOKEN` with `github.token` fallback; `continue-on-error: true` so build/deploy proceeds even without access. `LarsArtmann/go-output` checkout removed (was unused).
- Deploy to Firebase Hosting via `firebase-tools` CLI (master push only, runs under Node 24). Formerly used `FirebaseExtended/action-hosting-deploy@v0` (removed due to Node 20 deprecation).
- Node version pinned via `website/.node-version` (currently Node 24)

**Lighthouse CI** (`.github/workflows/lighthouse.yml`):

- Uses `treosh/lighthouse-ci-action@v12` (official LHCI, 1.2k+ stars, collaborated with Google Chrome team)
- **Prerequisite**: Install [Lighthouse CI GitHub App](https://github.com/apps/lighthouse-ci) for the repo, then add the token as `LHCI_GITHUB_APP_TOKEN` secret
- Triggers: push/PR to master (when website/config files change), plus `workflow_dispatch` for on-demand
- Scans: `https://gogenfilter.web.app/` (root, docs, API, changelog) — 3 runs per URL for stability
- Assertions: `lighthouse:no-pwa` preset + permissive custom thresholds (performance: warn≥0.8, accessibility: error≥0.8, SEO/best-practices: error≥0.9)
- Uploads results to temporary public storage + artifacts (14-day retention)
- Config in `lighthouserc.json` only (budgets NOT used — LHCI v12 rejects budgets+assertions together)

### CI Known Issues (2026-06-01)

- **Website CI**: `PRIVATE_REPO_TOKEN` secret optional — md-go-validator checkout has `continue-on-error: true`, doc validation is skipped gracefully when unavailable
- **Lighthouse CI**: Advisory-only monitor — all assertions set to `warn` level. The workflow collects metrics and uploads artifacts but never fails the build. To upgrade to gating: install LHCI GitHub App, add `LHCI_GITHUB_APP_TOKEN`, upgrade assertions to `error` in `lighthouserc.json`.
- **Lighthouse CI**: `LHCI_GITHUB_APP_TOKEN` secret not configured — GitHub status checks not produced. Workflow runs as advisory-only with all assertions at `warn` level.

## Key API Patterns

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
- **Dependabot alerts** — All 4 alerts are pnpm ecosystem (website transitive deps), not Go production deps. The `yaml` alert is pnpm `yaml`, not `go-faster/yaml`.
- **`gomodguard` deprecated** — Replaced by `gomodguard_v2` in `.golangci.yaml`.
- **Nix quality gates are mandatory** — Always run `nix flake check`, `nix run .#lint`, `nix run .#test` before declaring work done. Raw `go` commands outside `nix develop` skip `golangci-lint` entirely. On NixOS, `golangci-lint` is only available inside the Nix dev shell / flake apps.
- **`nix flake check` runs the test suite in a sandbox** — Tests that read files by relative path (`os.ReadFile("README.md")`) will fail because the Nix source fileset excludes non-Go files. Use `//go:embed` for hermetic file access in tests, and ensure any embedded file is added to the `src` fileset in `flake.nix`.
- **`vendorHash` must be updated when `go.sum` changes** — If `nix flake check` fails with "hash mismatch in fixed-output derivation", update the `vendorHash` in `flake.nix` (the `pkg` derivation) to the "got" hash printed in the error. There are 2 `vendorHash` values (one for `gogenfilter`, one for `md-go-validator` override); only update the one that fails.
- **Theme systems unified (2026-08-11)** — The landing page now uses the same `data-theme` attribute on `<html>` as Starlight docs (`data-theme="light"` / `data-theme="dark"`), and both share the `localStorage["starlight-theme"]` key. The landing page toggle is three-state (light → dark → auto), matching Starlight's docs toggle. The `"auto"` state follows `prefers-color-scheme` and updates reactively via a `matchMedia` change listener. Old `"theme"` localStorage key is read as backward-compat fallback. Default is `"auto"` (no key set). No-JS fallback: `<html data-theme="dark">` default in LandingLayout. Changes: `global.css` (`:root.light` → `:root[data-theme="light"]`), `theme-init.js`, `header.js` (three-state cycle + `resolveVisualTheme`/`getStoredTheme` helpers), `Header.astro` (three icon spans: sun/moon/monitor), `types.ts` + `Icon.astro` (added `monitor` icon).
- **`astro-og-canvas` requires the `param` option** — `OGImageRoute({ param: "slug", ... })` in `src/pages/og/[...slug].ts`. The `param` value must match the bracket segment name. Removing it causes a `PrerenderDynamicEndpointPathCollide` build error (undefined param). The library generates `/og/<slug>.png` automatically; do NOT add `.png` to the filename. **v0.13.0 removed `param` from `OGImageRouteConfig` types** — a `@ts-expect-error` directive on that line is required for `astro check` to pass (the option is still needed at runtime). Local `node_modules` can drift stale (e.g., 0.12.0 installed while lockfile pins 0.13.0); `pnpm install --frozen-lockfile` resyncs — a stale local install masks the type error.
- **BuildFlow auto-commits during doc work** — BuildFlow may commit work mid-session with generic messages. Check `git log` and `git status` early and often. If commits bundle unrelated changes, consider the working tree may not reflect only your changes.
- **`docs/status/archive/`** — Historical status reports and planning docs. Reports accumulate in `docs/status/`; older ones move to `archive/` when the active directory grows beyond ~15 files. There is no hard "keep only 3" rule — historical reports carry valuable cross-references and resolution annotations, so pruning is by relevance and age, not a fixed count.

## License

MIT
