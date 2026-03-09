# Dependency Freshness Check for code_health

## Goal

Add dependency version freshness as a metric in `code_health`. Parse manifest files, check latest versions via public package registry APIs, compute a freshness ratio, and factor it into the health grade.

## Supported Languages

| Language | Manifest | Registry API |
|----------|----------|-------------|
| Go | `go.mod` | `proxy.golang.org/<mod>/@latest` |
| TypeScript/JS | `package.json` | `registry.npmjs.org/<pkg>/latest` |
| Python | `pyproject.toml`, `requirements.txt` | `pypi.org/pypi/<pkg>/json` |
| Rust | `Cargo.toml` | `crates.io/api/v1/crates/<name>` |
| Java | `pom.xml`, `build.gradle` | `search.maven.org/solrsearch/select` |
| Ruby | `Gemfile` | `rubygems.org/api/v1/gems/<name>.json` |
| C# | `*.csproj` | `api.nuget.org/v3-flatcontainer/<name>/index.json` |
| C/C++ | — | Skipped (no standard package manager) |

## Language/Runtime Version Check

- Go: `go` directive in go.mod vs latest stable from `go.dev/dl/?mode=json`
- Node: `engines.node` in package.json
- Python: `requires-python` in pyproject.toml
- Rust: `edition` + `rust-version` in Cargo.toml

## Architecture

### 1. Manifest Parsers (`internal/polyglot/`)

Extend existing package with version-aware parsers:

- `gomod.go` — parse `go.mod`: Go version + require block (module, version)
- `packagejson.go` — parse `package.json`: dependencies + devDependencies with versions
- `pyproject.go` — parse `pyproject.toml` / `requirements.txt` with versions
- `cargo.go` — extend existing: add versions to Dependencies
- `pomxml.go` — parse `pom.xml` groupId:artifactId:version
- `gemfile.go` — parse `Gemfile` gem names + versions
- `csproj.go` — parse `*.csproj` PackageReference elements

Common type:
```go
type Dependency struct {
    Name     string
    Version  string // current version from manifest
    Language string
}

type ManifestInfo struct {
    Language        string
    RuntimeVersion  string       // go 1.26, node 20, etc.
    Dependencies    []Dependency
}
```

### 2. Freshness Checker (`internal/compare/freshness.go`)

- `CheckFreshness(ctx, deps []Dependency) (*FreshnessResult, error)`
- Concurrent registry lookups (bounded goroutines)
- Semver comparison: outdated = latest minor/patch > current (major bumps flagged separately)
- Redis cache: key `dep:freshness:<lang>:<name>`, TTL 24h
- Timeout per lookup: 3s, skip on error (don't fail the whole check)

Result:
```go
type FreshnessResult struct {
    Total         int
    UpToDate      int
    MinorOutdated int     // newer minor available
    MajorOutdated int     // newer major available
    Ratio         float64 // UpToDate / Total
    RuntimeStatus string  // "current" | "outdated" | "unknown"
    Outdated      []OutdatedDep
}

type OutdatedDep struct {
    Name    string
    Current string
    Latest  string
    Kind    string // "minor" | "major"
}
```

### 3. Integration into code_health

- New field in `RepoMetrics`: `DepFreshnessRatio float64`
- New weight in `grade.go`: ~0.06 (rebalance others to sum=1.0)
- Target: 0.8 (80% deps up-to-date = perfect score for this metric)
- Recommendation: list top-5 most outdated deps with current→latest

### 4. Output Format

In code_health XML report, new section:
```xml
<dependency_freshness>
  <runtime language="go" version="1.24" latest="1.26" status="outdated"/>
  <summary total="15" up_to_date="12" minor_outdated="2" major_outdated="1" ratio="0.80"/>
  <outdated>
    <dep name="github.com/foo/bar" current="v1.2.0" latest="v1.5.0" kind="minor"/>
  </outdated>
</dependency_freshness>
```

## What We Don't Do

- Vulnerability/CVE scanning (separate concern)
- Auto-update dependencies
- Private registry support (only public registries)
- C/C++ dependency checking (no standard package manager)

## API Requirements

All public, free, no API keys:
- proxy.golang.org, registry.npmjs.org, pypi.org, crates.io, search.maven.org, rubygems.org, api.nuget.org
- Rate limits are generous for single-repo checks
- Redis cache prevents repeated lookups
