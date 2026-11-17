# Contributing to go-filewatcher

Thank you for your interest in contributing! This project is open-source under the [MIT License](LICENSE).

## Development Setup

```bash
# Enter dev shell (requires nix)
nix develop

# Or with direnv
direnv allow

# Run all checks
GOWORK=off go vet ./...
GOWORK=off golangci-lint run ./...
GOWORK=off go test -race ./...
```

## Code Style

- Follow existing patterns in the codebase
- All tests must use `t.Parallel()` (enforced by linter)
- All struct fields must be initialized (enforced by `exhaustruct` linter)
- Use functional options pattern for configuration
- Use `errors` and `fmt` from stdlib (no external error packages)

## Pull Request Process

1. Fork the repository
2. Create a feature branch
3. Make your changes with tests
4. Ensure `golangci-lint run ./...` passes with 0 issues
5. Ensure `go test -race ./...` passes
6. Submit a pull request

## Reporting Issues

- Use GitHub Issues
- Include Go version, OS, and minimal reproduction steps

## Benchmarking

The project tracks performance regressions via `benchstat` comparisons against a
captured baseline. Three Nix apps handle the workflow:

```bash
# 1. Run benchmarks with -race (includes race-detector overhead)
nix run .#bench

# 2. Capture a clean baseline (no -race, -count=6 for stable deltas)
#    Writes bench-baseline.txt in the project root (gitignored)
nix run .#bench-baseline

# 3. Compare current benchmarks against the baseline
nix run .#bench-diff
```

### Baseline format

`bench-baseline.txt` is captured with these flags for reproducibility:

- **`-count=6`** — enough samples for `benchstat` to compute stable deltas.
- **No `-race`** — the race detector adds overhead and noise that obscures
  real regressions.
- **`-run=^$`** — skips all tests, runs only benchmarks.

When making performance-impacting changes, capture a fresh baseline:

```bash
nix run .#bench-baseline   # captures new baseline
nix run .#bench-diff        # verify deltas are expected
```

Commit `bench-baseline.txt` updates in the same PR as the change that warrants
them. Note: `bench-baseline.txt` is gitignored — it exists locally for
comparison, not in the repo.
