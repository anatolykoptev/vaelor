# Contributing: Adding Tools and Languages

## Adding a New Tool

1. Create `cmd/vaelor/tool_<name>.go` with input type and `register<Name>` function
2. Add `register<Name>(server, cfg)` call to `registerTools()` in `register.go`
3. Increment `toolCount` constant in `main.go`
4. Implement backing logic in the appropriate `internal/` package
5. Update tool count and description in CLAUDE.md

## Adding a New Language

1. Create `internal/parser/queries/<lang>.scm` with tree-sitter query patterns
2. Create `internal/parser/handler_<lang>.go` implementing `LanguageHandler` interface
3. Register the handler in `init()` via `registerHandler("<lang>", ...)`
4. Add test sample `internal/parser/testdata/sample.<ext>`
5. Add `TestParse<Lang>File` in `internal/parser/parser_test.go`
6. Update CLAUDE.md tool/language list

`LanguageHandler` interface requires: `Language()`, `Extensions()`, `Grammar()`, `QuerySource()`, `MapSymbol()`.

## CGO Requirement

tree-sitter grammars are C libraries requiring `CGO_ENABLED=1` and a C compiler (`gcc` or `clang`).
Docker uses `golang:1.24-alpine` with `apk add gcc musl-dev`. Cross-compilation requires target C toolchains.
