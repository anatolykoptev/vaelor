# AI Coding Assistants — Context & Ranking Strategies

How leading AI coding tools build LLM context and rank code for relevance.
See [aider.md](aider.md) for Aider's deep dive.

## Continue.dev — Hybrid BM25 + Embeddings

- **Repo**: [continuedev/continue](https://github.com/continuedev/continue) | ~25K stars | TypeScript
- **Key files**: `core/src/util/search/BM25.ts`, `core/src/context/providers/CodebaseContextProvider.ts`

### Pipeline
keyword search (lunr/BM25) + embedding search → RRF (Reciprocal Rank Fusion) → optional LLM reranking

### Key Insight
Hybrid BM25+embeddings = -49% failed retrievals; +reranking = -67% (Anthropic Contextual Retrieval paper).

## Cursor — Merkle Tree + AST Chunking

- **Approach** (from blog, closed source)
- Merkle tree for incremental diff
- AST chunking via tree-sitter (function boundaries)
- Embeddings per chunk → vector DB (Turbopuffer)
- Content-hash caching: unchanged chunks reuse embeddings
- Simhash for teammate index reuse (4h → 21s)
- **Result**: +12.5% accuracy from semantic search

## Cline — Dynamic Context via Tools

- **Repo**: [cline/cline](https://github.com/cline/cline) | ~40K stars | TypeScript
- System prompt: 59K chars, 12K tokens, XML-formatted tool descriptions

### Context Strategy
No static pre-selection. LLM explores via:
- `list_files` — directory listing
- `search_files` — content search
- `list_code_definition_names` — symbol overview

### Key Insight
For batch analysis (like go-code), **pre-ranking is better**.
For interactive agents, **exploration tools are better**.

## Patterns Summary

| Tool | Context Strategy | Key Innovation |
|------|-----------------|----------------|
| Aider | PageRank on identifier graph | Personalized ranking |
| Continue | BM25 + embeddings + RRF | Hybrid retrieval |
| Cursor | Merkle + AST chunks + vectors | Incremental indexing |
| Cline | LLM-driven exploration | No pre-ranking |
| go-code | BM25F + PageRank + intent prompts | Graph DB + NL→Cypher |
