# Tree-sitter Based Code Analysis Tools

## Go Bindings Comparison

| Library | Stars | CGo | Bundled Grammars | Status | Recommendation |
|---------|-------|-----|-----------------|--------|----------------|
| [smacker/go-tree-sitter](https://github.com/smacker/go-tree-sitter) | 539 | Yes | 20+ languages | Mature, slowing | **Our choice.** Batteries-included, production-tested |
| [tree-sitter/go-tree-sitter](https://github.com/tree-sitter/go-tree-sitter) | 212 | Yes | Modular (separate `go get`) | Official, active | Best for new projects. Watch for future switch. |
| [odvcencio/gotreesitter](https://github.com/odvcencio/gotreesitter) | New | No | 209 grammars | Pure Go, Feb 2026 | Experimental, 11x slower parsing |

## ast-grep — Structural Code Search

- **Repo**: [ast-grep/ast-grep](https://github.com/ast-grep/ast-grep) | 10k+ stars | Rust
- Polyglot structural search, lint, and rewrite. 20+ languages.
- **Key insight**: Pattern language abstraction — `foo($A, $B)` instead of S-expressions.
  UX gold standard for tree-sitter-based search.
- **Useful for us**: Pattern matching UX, YAML rule files. Claude Code plugin exists.
- **Not usable directly**: Rust binary, no Go embedding.

## DeepSourceCorp/globstar — Static Analysis Toolkit

- **Repo**: [DeepSourceCorp/globstar](https://github.com/DeepSourceCorp/globstar) | 478 stars | Go
- Two-tier checker API: YAML rules (tree-sitter queries) + full Go API (imports, scope, cross-file).
- **Useful for us**: Dual YAML+Go checker model.

## semgrep — Pattern-Based Static Analysis

- **Repo**: [semgrep/semgrep](https://github.com/semgrep/semgrep) | 14,283 stars | OCaml
- Tree-sitter internally, 30+ languages. Patterns like real code with `$VAR` wildcards.
- **Useful for us**: Generic AST normalization concept. Not embeddable in Go.

## williamfzc/srctx — Call Graph Extraction

- **Repo**: [williamfzc/srctx](https://github.com/williamfzc/srctx) | 59 stars | Go
- Function-level call graphs via tree-sitter + SCIP. Diff-aware analysis.
- **Useful for us**: Go-native call graph reference, small and focused.

## Query Files (.scm) — Best Sources

| Source | Use |
|--------|-----|
| `tree-sitter/tree-sitter-go` `queries/tags.scm` | Go symbols — used by GitHub for code navigation |
| `nvim-treesitter/nvim-treesitter` (13k stars) | Most comprehensive .scm collection across all languages |
| Language grammar repos (`tree-sitter-{lang}/queries/`) | Official queries per language |

Query file types:
- `tags.scm` — symbol navigation (functions, types, refs) — **our primary need**
- `highlights.scm` — syntax coloring (most complete node coverage)
- `locals.scm` — scope/variable resolution
- `injections.scm` — embedded language detection

## Repo-to-LLM Ingestion Tools

### repomix
- [yamadashy/repomix](https://github.com/yamadashy/repomix) | 22,131 stars | TypeScript
- Pack entire repo into LLM-friendly format (XML, Markdown, JSON, plain).
- Token counting (tiktoken), output splitting for large repos, MCP server mode.
- **Key insight**: XML as default — LLMs parse structured tags reliably.

### code2prompt
- [mufeedvh/code2prompt](https://github.com/mufeedvh/code2prompt) | 7,169 stars | Rust
- Codebase → structured prompt. Handlebars templates, token counting, PyO3 Python SDK.
- Two-layer filtering — WalkBuilder (gitignore) + FileFilter (user globs).

### yek
- [mohsen1/yek](https://github.com/mohsen1/yek) | 2,430 stars | Rust
- Git history → file importance ranking. Recently changed = higher priority.
- Can cap output to N tokens by priority.

### Go-native alternatives
- **CodeWeaver** (721 stars, Go) — Go equivalent of repomix. `filepath.WalkDir` + tree rendering.
- **shotgun_code** (1,989 stars, Go) — XML-tagged output for LLM consumption.

## Go Code Analysis (Native)

| Package | Purpose | Notes |
|---------|---------|-------|
| `golang.org/x/tools/go/callgraph/rta` | RTA call graph | ~2s on medium projects, precise |
| `golang.org/x/tools/go/packages` | Go package loading with type info | Required for SSA |
| `golang.org/x/tools/go/ssa` | SSA form construction | Needed for RTA |
| `go/ast` | Go AST parsing | Full type info via `go/types` |

For Go repos, native `golang.org/x/tools` > tree-sitter. Use tree-sitter for all other languages.
Planned: Phase 11.2 (Go-native call graph enhancement).
