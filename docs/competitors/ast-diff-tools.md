# AST Diff Tools

## difftastic — AST-Level Diff (Visual)

- **Repo**: [Wilfred/difftastic](https://github.com/Wilfred/difftastic) | 24,230 stars | Rust
- Tree-sitter → CST → three-phase matching → side-by-side colored output

### Algorithm
1. Hash-based identical subtree detection (O(n))
2. Language-specific heuristic matching (signatures, identifiers)
3. LCS on unmatched children for fine-grained alignment

### Key Insight
`NodeHash` for O(1) subtree equality checks before expensive traversal.

**Limitation**: Rust-only, visual output only, no structured data format.

## GumTree — Edit Script Generation (Academic Standard)

- **Repo**: [GumTreeDiff/gumtree](https://github.com/GumTreeDiff/gumtree) | 1,280 stars | Java
- Academic gold standard (ICSE 2014, TSE 2023)

### Algorithm
GreedySubtreeMatcher → BottomUpMatcher → TopDownMatcher pipeline.

### Key Data Structures
- `MappingStore` — bidirectional map between source/destination nodes
- `CandidateMultiset` — groups nodes by hash+size for O(1) candidate lookup

**Limitation**: Java only, no Go port exists.

## smacker/gum — Go GumTree (Our Target)

- **Repo**: [smacker/gum](https://github.com/smacker/gum) | ~50 stars | **Go**
- **Same author as `smacker/go-tree-sitter`** — guaranteed compatibility
- `gum/tsitter/` adapter converts tree-sitter CST → gum.Tree
- Insert/Delete/Update/Move operations
- **Planned**: Phase 8 (AST Structural Diff)

## codinuum/diffast — Normalized AST Diff

- **Repo**: [codinuum/diffast](https://github.com/codinuum/diffast) | ~200 stars | OCaml
- Normalizes ALL languages into a common `Ast.t` type
- Enables cross-language structural similarity
- Exports diffs as facts in XML/RDF

## cedricrupb/code_diff

- Small Python reimplementation of GumTree
- Confirms tree-sitter + GumTree works well together
