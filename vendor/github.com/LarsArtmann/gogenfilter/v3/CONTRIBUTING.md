# Contributing to gogenfilter

Thank you for your interest in contributing! This guide covers everything you need to get started.

## Development Setup

**Requirements:** Go 1.26+

```bash
git clone https://github.com/LarsArtmann/gogenfilter.git
cd gogenfilter
go mod download
```

## Building

```bash
go build ./...
```

## Testing

```bash
# Run all tests (no cache)
go test -count=1 ./...

# Run with race detector
go test -race ./...

# Run a specific test
go test -run TestDetectReason -count=1 ./...
```

## Linting

```bash
golangci-lint run
```

Some linter warnings are expected false positives (e.g., `testpackage` on internal test files that need access to unexported symbols). These are annotated with `//nolint` comments where appropriate.

## CI

All pull requests must pass:

- `go build ./...` — compiles cleanly
- `go test -race -cover ./...` — tests pass with race detection
- `go vet ./...` — no vet warnings
- `golangci-lint run` — no new lint issues

## Code Style

- Follow standard Go conventions ([Effective Go](https://go.dev/doc/effective_go))
- Table-driven tests using `t.Parallel()` within `t.Run()`
- Functional options pattern for configuration (see `filter.go`)
- Derive lists from single-source tables (see `detectors` in `detection.go`)
- Use `fstest.MapFS` in tests — never real filesystem I/O

## Adding a New Generator Detector

1. Add a `FilterOption` and `FilterReason` constant in `types.go`
2. Add a detector entry to the `detectors` table in `detection.go`:
   - Set `matchFilename` and/or `checkContent` functions
   - Set `url` to the generator's homepage
   - Set `filenameDesc` to a human-readable filename pattern (use `filenameNoneDesc` if no filename detection)
   - Set `contentDesc` to a human-readable content marker description
3. Add a `websiteMetadata` entry in `cmd/gendocs/main.go` with the generator's logo path and display filename
4. Add a real generated file to `testdata/<generator>/` as an integration fixture
5. Update `integration_test.go` with the new fixture
6. Run `go generate ./...` to regenerate `generators.json`, README tables, `generators.mdx`, `detection.mdx`, and `doc.go`

The derivation system automatically updates `AllFilterOptions()`, `AllGeneratorOptions()`, `AllFilterReasons()`, `AllDetectorDocs()`, and `IsValid()` — no manual list maintenance needed.

**CI enforcement:** The docs freshness job runs `go generate ./...` and fails if any generated file has uncommitted changes. Adding a detector without a `websiteMetadata` entry will fail the build (gendocs exits with an error), and adding a detector without running `go generate` will fail the freshness check.

## Pull Request Process

1. Create a feature branch from `master`
2. Make focused, self-contained commits
3. Ensure all CI checks pass
4. Include tests for any new functionality
5. Update documentation (`README.md`, `CHANGELOG.md`) if applicable

## Project Structure

```
├── detection.go       # Core detection logic and detector table
├── filter.go          # Filter type with functional options
├── types.go           # FilterOption, FilterReason constants and methods
├── errors.go          # Branded error types
├── pattern.go         # Glob pattern matching via doublestar/v4
├── sqlc.go            # SQLC config discovery and parsing
├── project.go         # Project root discovery
├── scan.go            # ScanProject for walking fs.FS
├── doc.go             # Package documentation
├── cmd/gendocs/       # Documentation generator binary (reads detectors table)
├── bench_test.go      # Benchmarks
├── integration_test.go # Tests against real tool output
├── testdata/          # Fixture files from real generators
└── docs/              # Planning documents and status reports
```
