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
For batch analysis (like Vaelor), **pre-ranking is better**.
For interactive agents, **exploration tools are better**.

## Vaelor — Three-Layer Search + Graph Intelligence

- **Layer 1 (Recall)**: Vector embeddings (jina-code-v2) find semantically similar symbols
- **Layer 2 (Precision)**: pg_trgm trigram similarity finds functions by name abbreviations
  (`init_chat_llms` for query "initialize LLM" — vector misses it, pg_trgm finds it)
- **Layer 3 (Quality)**: CE cross-encoder (gte-multi-rerank, 306M params) reranks top-20
  by seeing query + code together, not just independent embeddings

**Unique capabilities vs competitors:**
- Cross-language call graph (Python → Go blast radius via AGE)
- Dead code detection with CE probability [0..1] at graph build time
- code_health grade A-F with dead code metric in formula
- Background computation + PostgreSQL cache for large repos

## Patterns Summary

| Tool | Context Strategy | Key Innovation |
|------|-----------------|----------------|
| Aider | PageRank on identifier graph | Personalized ranking |
| Continue | BM25 + embeddings + RRF | Hybrid retrieval |
| Cursor | Merkle + AST chunks + vectors | Incremental indexing |
| Cline | LLM-driven exploration | No pre-ranking |
| Vaelor | Vector + pg_trgm + CE reranker (3 layers) | AGE cross-language graph, dead code CE scores, code_health cache, background builds |
