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
