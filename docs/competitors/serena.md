# oraios/serena — LSP-backed MCP Server

- **Repo**: [oraios/serena](https://github.com/oraios/serena) | 20,742 stars | Python
- **Approach**: LSP-backed, 40+ languages, 30+ tools

## What It Is

Symbol-level MCP tools backed by real language servers (gopls, pyright, rust-analyzer).

## Key Tools

- `find_symbol` — locate by name
- `find_referencing_symbols` — who uses this?
- `get_symbols_overview` — package/module summary
- `insert_after_symbol` — code insertion by symbol, not line
- `rename_symbol` — refactoring via LSP

## Key Insight

> "The agent no longer needs to read entire files." Navigate by symbol, not by file.

This is the LSP approach vs our tree-sitter approach. LSP gives precise type-aware results
but requires running language servers for each language.

## Trade-offs vs Vaelor

| Aspect | Serena | Vaelor |
|--------|--------|---------|
| Precision | High (LSP) | Medium (tree-sitter) |
| Setup | Heavy (need gopls, pyright etc.) | Zero (tree-sitter bundled) |
| Languages | 40+ (any with LSP) | 9 (explicit handlers) |
| Graph DB | No | Apache AGE |
| NL queries | No | Yes (NL→Cypher) |
| Code compare | No | Yes |
| Offline | No (needs LSP) | Yes |
