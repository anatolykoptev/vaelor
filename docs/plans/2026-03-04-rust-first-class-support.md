# Rust First-Class Language Support

**Date**: 2026-03-04
**Scope**: 8 improvements to make Rust a first-class citizen in go-code

## 1. Symbol struct — new fields

Add three fields to `parser.Symbol`:

```go
Receiver   string   // impl type or "Trait for Type"
IsPublic   bool     // pub visibility (Rust), uppercase (Go)
Attributes []string // #[test], #[derive(Clone)], etc.
```

Zero-value defaults — no breaking changes for existing languages.

## 2. rust.scm — expanded queries

- `impl_item` captures `@symbol.receiver` (type) and `@symbol.trait` + `@symbol.impl_target` (trait impl)
- Generic impl types via `generic_type` pattern
- Visibility and attributes parsed in handler Go code (not .scm) via AST sibling walk

## 3. rust_calls.scm — fix false positives

- Remove identifier-in-arguments pattern (catches closure params)
- Add scoped calls: `Module::func()` via `scoped_identifier`
- Add macro invocations: `macro_invocation` node

## 4. rust_rels.scm — new file

Type relationships for code_graph IMPLEMENTS edges:
- `impl Trait for Type` → `rel.subject` / `rel.impl_target`
- Generic and scoped trait targets

## 5. handler_rust.go — enriched parsing

Three new helpers:
- `hasVisibilityModifier(node)` — checks for `visibility_modifier` child
- `extractAttributes(node, source)` — collects `attribute_item` prev siblings
- `implReceiver(methodNode, source)` — walks up to `impl_item` for type/trait

Methods populate `Receiver`, `IsPublic`, `Attributes` on Symbol.

## 6. deadcode.go — Rust-aware filtering

- `isExported()` uses `sym.IsPublic` for Rust (not uppercase)
- `isTestFunc()` checks `sym.Attributes` for `#[test]`, `#[tokio::test]`
- `isTestFile()` adds `_test.rs` suffix
- `rustWellKnownMethods` map: `default`, `clone`, `drop`, `from`, `into`,
  `try_from`, `try_into`, `next`, `into_iter`, `poll`, `eq`, `cmp`, `hash`,
  `serialize`, `deserialize`, `deref`, `deref_mut`, `fmt`, `source`, etc.
- `classifyConfidence` uses `Receiver` to detect trait impl methods → medium

## 7. dep_graph — Rust import resolution

- `resolveRustImport()`: maps `crate::` to internal, `std::`/`core::`/`alloc::` to stdlib, rest to external
- `buildImportGraph` branches by language
- `addRustImports` builds edges from `use` declarations

## 8. Cargo.toml parsing

- Parse `[dependencies]`, `[dev-dependencies]`, `[workspace.members]`
- Feed into `explore` (external_deps count), `dep_graph` (edges), `code_health` (metrics)
- Use `BurntSushi/toml` or manual section parsing

## 9. symbol_search kind-only filter

- Allow `kind` without `query` — set `query = "*"` internally
- Error only when both `query` and `kind` are empty

## Rust 2024 Edition considerations

- Grammar already supports `let_chain`, `async_block`, `closure_expression`, `unsafe_block`
- `go-tree-sitter` v0.0.0-20240827 with LANGUAGE_VERSION 14
- AsyncFn/AsyncFnMut/AsyncFnOnce added to wellKnownMethods
- `gen` keyword reserved but no grammar node yet — no action needed

## Files to modify

| File | Change |
|------|--------|
| `internal/parser/parser.go` | Add `Receiver`, `IsPublic`, `Attributes` to Symbol |
| `internal/parser/handler_rust.go` | Enriched mappers, 3 new helpers |
| `internal/parser/queries/rust.scm` | impl_item captures |
| `internal/parser/queries/rust_calls.scm` | Fix FP, add scoped + macro |
| `internal/parser/queries/rust_rels.scm` | **New file** |
| `internal/deadcode/deadcode.go` | Rust-aware filters |
| `internal/analyze/analyze.go` | Rust import resolution |
| `internal/polyglot/cargo.go` | **New file** — Cargo.toml parser |
| `cmd/go-code/tool_symbol_search.go` | Kind-only filter |
