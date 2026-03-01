# vitali87/code-graph-rag — Graph RAG for Code

- **Repo**: [vitali87/code-graph-rag](https://github.com/vitali87/code-graph-rag) | 1,970 stars | Python

## What It Is

Tree-sitter → Memgraph graph DB → NL query → LLM generates Cypher → code retrieval.

## Graph Schema

`File/Function/Class/Module + CALLS/INHERITS/CONTAINS/IMPORTS`

## Key Insight

NL → Cypher translation with **schema grounding** in the LLM prompt. Without schema, LLM hallucinates property names.

## What We Adopted

- Phase 7.1: Schema injection in freeform Cypher (our `SystemPromptGenerateCypher` currently has no schema context)
- Phase 7.4: `FileHashCache` per file, JSON state — incremental indexing pattern
- Graph schema design influenced our Apache AGE vertex/edge types

## Key Patterns

- `FileHashCache` for incremental indexing (per-file hash + JSON state)
- UniXcoder embeddings + vector DB for semantic search
- Schema text injected into Cypher generation prompt
