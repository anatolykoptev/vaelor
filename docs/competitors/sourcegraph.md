# Sourcegraph — SCIP + Zoekt + PageRank

## SCIP — Code Intelligence Protocol

- **Repo**: [sourcegraph/scip](https://github.com/sourcegraph/scip) | 521 stars | Go
- **What**: Replaces LSIF. Language-agnostic Protobuf schema for indexing code symbols.
- Human-readable string IDs. 26 SymbolKind values. Definition vs Reference tracking.
- **Indexers**: scip-go, scip-typescript, scip-java, scip-python, scip-rust, scip-clang, scip-ruby.
- **Useful for us**: SymbolKind enum as output standard. Interop with Sourcegraph ecosystem.
- **Planned**: Phase 11.1 (SCIP backend for Go)

## Zoekt — BM25F for Code Search

- **Repo**: [sourcegraph/zoekt](https://github.com/sourcegraph/zoekt) | Go
- **Blog**: [keeping-it-boring-and-relevant-with-bm25f](https://sourcegraph.com/blog/keeping-it-boring-and-relevant-with-bm25f)
- **Fields**: symbol definitions (highest weight) > filename > file content (lowest)
- **Params**: k1=1.2, b=0.75, language-specific stopwords
- **Result**: +20% across all search quality metrics
- **Code tokenization**: `getUserById` → `[get, user, by, id, getUserById]` (both original and camelCase split)
- **Status in Vaelor**: Adopted in Phase 6.3 (BM25F) and 6.4 (query understanding with camelCase splitting)

## PageRank for Code

- **Blog**: [ranking-in-a-week](https://sourcegraph.com/blog/ranking-in-a-week)
- **Approach**: SCIP-based reference graph (file A references symbol in file B → edge A→B), PageRank via Apache Spark
- **Interpretation**: high PageRank = high code reuse = architecturally important
- **Status in Vaelor**: Adopted in Phase 6.5 (file-level import graph). Phase 7.5 planned for identifier-level upgrade.

## doctree — Tree-sitter Indexer in Go (archived)

- **Repo**: [sourcegraph/doctree](https://github.com/sourcegraph/doctree) | 881 stars | Go
- **What**: Multi-language symbol indexer using tree-sitter. Best Go reference architecture.
- **Key pattern** — `Language` interface with embedded queries:

```go
type Language interface {
    Name() schema.Language
    Extensions() []string
    IndexDir(ctx context.Context, dir string) (*schema.Index, error)
}

//go:embed queries.scm
var queries []byte
```

- **Adopted in Vaelor**: Language interface design, parallel indexing with WaitGroup, modtime caching.
