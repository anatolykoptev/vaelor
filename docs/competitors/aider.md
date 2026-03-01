# Aider-AI/aider — AI Pair Programming with RepoMap

- **Repo**: [Aider-AI/aider](https://github.com/Aider-AI/aider) | ~22K stars | Python
- **Key file**: `aider/repomap.py`
- **Go port**: [codeberg.org/MadsRC/aigent](https://codeberg.org/MadsRC/aigent) `internal/repomap`
- **Analyzed**: 2026-02-28 via `repo_analyze` deep mode

## What It Is

Terminal-based AI pair-programming tool that connects LLMs to a local Git repo.
Open-source competitor to Claude Code / Cursor / Cline. Supports 100+ languages, 20+ LLMs.

## RepoMap Architecture (most relevant to go-code)

The core innovation is **RepoMap** — a concise, ranked overview of the codebase sent to the LLM with each request.

### Pipeline (7 steps)

1. **Tag extraction** (`get_tags_raw`): tree-sitter `.scm` queries extract `Tag(rel_fname, name, kind="def"|"ref", line)` per file
2. **Pygments fallback**: if tree-sitter finds defs but no refs → fallback to `pygments` Token.Name extraction. Guarantees graph edges for poorly-covered languages
3. **Graph construction** (`get_ranked_tags`): NetworkX MultiDiGraph, files as nodes:
   - Self-edges: `(definer → definer, weight=0.1)` for each defined identifier
   - Reference edges: `(referrer → definer, weight=mul/len(definers))` per identifier reference
   - `mul` multiplier: ×10 for mentioned identifiers, ×10 for long camelCase/snake_case (≥8 chars), ×0 for `_` prefixed
4. **Personalized PageRank** (`nx.pagerank(alpha=0.85, personalization=...)`):
   - Personalization vector boosts chat files + mentioned files + files matching mentioned identifiers
   - Chat files receive `100 / len(fnames)` personalization score
5. **Token-budgeted output**: iterate ranked files, collect tags, stop at `max_map_tokens`
6. **Rendering** (`render_tree`, `to_tree`): hierarchical path + lines-of-interest format
7. **Caching**: `diskcache` (SQLite) with mtime invalidation, fallback to in-memory dict on corruption

### Key Design Decisions

- **Identifier-level edges, not file-level imports**: Much denser graph than our current approach
- **Dynamic budget**: 8x expansion when no specific files mentioned
- **Skeleton format**: `│func Foo()` + `⋮...` for omitted code — explicit truncation signal for LLM
- **RecursionError guard**: catches networkx overflow on very large graphs, disables repo map

## Edit Format System (Strategy Pattern)

6 format strategies, each with its own Coder class:

| Format | How it works |
|--------|-------------|
| `diff` (SEARCH/REPLACE) | `<<<<<<< SEARCH ... ======= ... >>>>>>> REPLACE` blocks |
| `whole` | LLM returns entire file content |
| `patch` | Custom "V4A diff" format |
| `udiff` | Standard unified diff |
| `func` | OpenAI function calling (`write_file`) |
| `diff-fenced` | Variant with filename inside fence |

### Robust Diff Application (`search_replace.py`)

`flexible_search_and_replace` — multiple fallback strategies:
1. Direct string match
2. `diff_match_patch` for fuzzy matching
3. Git-like cherry-picking heuristics
4. `relative_indent` normalization for whitespace tolerance

## Dynamic Prompt Assembly (ChatChunks)

`ChatChunks` — structured container for LLM context parts:
- System messages with `{fence}`, `{platform}`, `{shell_cmd_prompt}` placeholders
- Example conversations (few-shot)
- Summarized past turns
- Repo map (ranked)
- Current file contents
- Behavioral nudges: `lazy_prompt`, `overeager_prompt` per model

## What We Should Adopt (go-code)

| Pattern | Our Phase | What to do |
|---------|-----------|------------|
| Identifier-level reference edges | 7.5 | Replace import-only graph with per-identifier reference graph |
| Personalized PageRank | 7.5 | Add personalization vector for query-mentioned identifiers |
| Weight = `mul / len(definers)` | 7.5 | Distribute importance across definition sites |
| Pygments/regex fallback for refs | 7.5 | Ensure graph edges for under-covered languages |
| Mentioned identifiers ×10 boost | 7.5 | Exact symbol name match in scoring |

## What We Already Do Better

- **9-language tree-sitter support** vs their pygments fallback
- **Apache AGE graph DB** for persistent queries vs their in-memory networkx
- **BM25F scoring** with IDF normalization vs their simple contains match
- **Intent-aware system prompts** vs their static depth-based prompts
- **Cross-language analysis** (polyglot, HTTP routes) — they have none
