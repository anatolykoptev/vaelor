# Rust First-Class Support — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make Rust a first-class citizen in go-code with accurate dead code detection, dependency graphs, and type relationships.

**Architecture:** Extend Symbol struct with 3 fields, rewrite Rust tree-sitter queries, add Rust-aware dead code filters, Rust import resolver, Cargo.toml parser, and relationship queries. All changes are additive — existing languages unaffected.

**Tech Stack:** Go, tree-sitter (smacker/go-tree-sitter), BurntSushi/toml

---

### Task 1: Extend Symbol struct with Receiver, IsPublic, Attributes

**Files:**
- Modify: `internal/parser/parser.go:36-73`

**Step 1: Add three fields to Symbol struct**

In `internal/parser/parser.go`, add after the `BodyHash` field (line 72):

```go
	// Receiver is the type name for methods (e.g. "Config" for impl Config,
	// "Display for Config" for impl Display for Config). Empty for free functions.
	Receiver string

	// IsPublic indicates the symbol has public visibility (pub in Rust, uppercase in Go).
	IsPublic bool

	// Attributes are annotations/decorators (e.g. "#[test]", "#[derive(Clone)]").
	Attributes []string
```

**Step 2: Run existing tests to verify no regressions**

Run: `cd /path/to/repos/src/go-code && go test ./internal/parser/ -v -count=1 2>&1 | tail -5`
Expected: all existing tests PASS (new fields are zero-value)

**Step 3: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/parser/parser.go
git commit -m "feat(parser): add Receiver, IsPublic, Attributes to Symbol struct"
```

---

### Task 2: Rewrite rust.scm with impl context captures

**Files:**
- Modify: `internal/parser/queries/rust.scm`

**Step 1: Replace rust.scm contents**

```scheme
; tree-sitter query for Rust symbol extraction.
; Extracts functions, methods (with impl context), types, traits, and imports.

; Top-level free functions (not inside impl).
(source_file
  (function_item
    name: (identifier) @symbol.name) @symbol.function)

; Free functions inside mod blocks.
(mod_item
  body: (declaration_list
    (function_item
      name: (identifier) @symbol.name) @symbol.function))

; Methods inside plain impl blocks: impl Type { fn ... }
(impl_item
  body: (declaration_list
    (function_item
      name: (identifier) @symbol.name) @symbol.method))

; Struct definitions.
(struct_item
  name: (type_identifier) @symbol.name) @symbol.type

; Enum definitions.
(enum_item
  name: (type_identifier) @symbol.name) @symbol.type

; Trait definitions.
(trait_item
  name: (type_identifier) @symbol.name) @symbol.interface

; Type alias definitions.
(type_item
  name: (type_identifier) @symbol.name) @symbol.type

; Const items.
(const_item
  name: (identifier) @symbol.name) @symbol.const

; Static items.
(static_item
  name: (identifier) @symbol.name) @symbol.var

; Use declarations (import paths).
(use_declaration
  argument: (_) @import.path)
```

Note: The .scm stays essentially the same for method capture — receiver/trait context is extracted in Go code by walking up the AST from the method node to its parent `impl_item`. This avoids complex multi-capture patterns that tree-sitter handles poorly.

**Step 2: Run parser tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/parser/ -run TestParseRust -v -count=1`
Expected: PASS

**Step 3: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/parser/queries/rust.scm
git commit -m "refactor(parser): clarify rust.scm comments for impl context"
```

---

### Task 3: Fix rust_calls.scm — remove false positives, add scoped + macro calls

**Files:**
- Modify: `internal/parser/queries/rust_calls.scm`

**Step 1: Write test for scoped calls and macro invocations**

In `internal/parser/parser_test.go`, add a new test (or create `internal/parser/rust_calls_test.go`):

```go
func TestRustCallExtraction(t *testing.T) {
	source := []byte(`
fn example() {
    helper();
    self.method();
    Module::scoped_func();
    println!("hello");
    let x = vec![1, 2, 3];
    callback(some_var);
}
`)
	calls, err := parser.ExtractCalls("test.rs", source, parser.ParseOpts{Language: "rust"})
	if err != nil {
		t.Fatalf("ExtractCalls: %v", err)
	}

	callNames := make(map[string]bool)
	for _, c := range calls {
		callNames[c.Name] = true
	}

	// Should find: helper, method, scoped_func, println, vec
	for _, want := range []string{"helper", "method", "scoped_func", "println", "vec"} {
		if !callNames[want] {
			t.Errorf("missing call %q; got %v", want, callNames)
		}
	}

	// Should NOT find: some_var (it's an argument, not a function reference)
	if callNames["some_var"] {
		t.Error("some_var should NOT be extracted as a call (it's an argument)")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/parser/ -run TestRustCallExtraction -v -count=1`
Expected: FAIL — `some_var` is currently extracted, scoped_func/println/vec are missing

**Step 3: Replace rust_calls.scm**

```scheme
; Direct function call: helper()
(call_expression
  function: (identifier) @call.function)

; Method call: self.method(), obj.do_thing()
(call_expression
  function: (field_expression
    field: (field_identifier) @call.method))

; Scoped function call: Module::func(), Type::new()
(call_expression
  function: (scoped_identifier
    name: (identifier) @call.function))

; Macro invocations: println!(), vec![], format!()
(macro_invocation
  macro: (identifier) @call.function)

; Scoped macro invocations: tokio::select!()
(macro_invocation
  macro: (scoped_identifier
    name: (identifier) @call.function))
```

**Step 4: Run test to verify it passes**

Run: `cd /path/to/repos/src/go-code && go test ./internal/parser/ -run TestRustCallExtraction -v -count=1`
Expected: PASS

**Step 5: Run all parser tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/parser/ -v -count=1 2>&1 | tail -5`
Expected: all PASS

**Step 6: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/parser/queries/rust_calls.scm internal/parser/
git commit -m "fix(parser): remove false positive calls in Rust, add scoped + macro calls"
```

---

### Task 4: Create rust_rels.scm and wire into handler

**Files:**
- Create: `internal/parser/queries/rust_rels.scm`
- Modify: `internal/parser/handler_rust.go`

**Step 1: Write test for Rust relationships**

Add to `internal/parser/parser_test.go` or a new file:

```go
func TestRustRelationships(t *testing.T) {
	source := []byte(`
trait Handler {
    fn handle(&self);
}

struct MyHandler;

impl Handler for MyHandler {
    fn handle(&self) {}
}

trait Serializable {
    fn serialize(&self) -> String;
}

impl Serializable for MyHandler {
    fn serialize(&self) -> String { String::new() }
}
`)
	rels, err := parser.ExtractRelationships("test.rs", source, parser.ParseOpts{Language: "rust"})
	if err != nil {
		t.Fatalf("ExtractRelationships: %v", err)
	}

	if len(rels) < 2 {
		t.Fatalf("expected >= 2 relationships, got %d", len(rels))
	}

	found := make(map[string]bool)
	for _, r := range rels {
		key := r.Subject + "->" + r.Target
		found[key] = true
	}

	if !found["Handler->MyHandler"] {
		t.Error("missing relationship: Handler -> MyHandler")
	}
	if !found["Serializable->MyHandler"] {
		t.Error("missing relationship: Serializable -> MyHandler")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/parser/ -run TestRustRelationships -v -count=1`
Expected: FAIL — Rust handler doesn't implement RelationshipQueryProvider

**Step 3: Create rust_rels.scm**

```scheme
; Trait implementation: impl Handler for MyHandler
(impl_item
  trait: (type_identifier) @rel.subject
  type: (type_identifier) @rel.impl_target)

; Trait impl with generic target: impl Handler for Arc<Foo>
(impl_item
  trait: (type_identifier) @rel.subject
  type: (generic_type
    type: (type_identifier) @rel.impl_target))

; Trait impl with scoped trait: impl std::fmt::Display for Foo
(impl_item
  trait: (scoped_type_identifier
    name: (type_identifier) @rel.subject)
  type: (type_identifier) @rel.impl_target)
```

**Step 4: Wire into handler_rust.go**

Add to `handler_rust.go`:
- `//go:embed queries/rust_rels.scm` + `var rustRelsQueryBytes []byte`
- Add `relsQuery *sitter.Query` field to `rustHandler` struct
- Compile and assign in `init()`
- Add `func (h *rustHandler) RelationshipsQuery() *sitter.Query { return h.relsQuery }`

The modified `handler_rust.go` header becomes:

```go
//go:embed queries/rust.scm
var rustQueryBytes []byte

//go:embed queries/rust_calls.scm
var rustCallsQueryBytes []byte

//go:embed queries/rust_rels.scm
var rustRelsQueryBytes []byte

type rustHandler struct {
	lang      *sitter.Language
	query     *sitter.Query
	callQuery *sitter.Query
	relsQuery *sitter.Query
}
```

In `init()`, add after callQuery compilation:

```go
	rq, err := sitter.NewQuery(rustRelsQueryBytes, lang)
	if err != nil {
		panic("rust_rels.scm query compile error: " + err.Error())
	}
	// ... existing assignments ...
	rustLang.relsQuery = rq
```

Add method:

```go
func (h *rustHandler) RelationshipsQuery() *sitter.Query { return h.relsQuery }
```

**Step 5: Run test to verify it passes**

Run: `cd /path/to/repos/src/go-code && go test ./internal/parser/ -run TestRustRelationships -v -count=1`
Expected: PASS

**Step 6: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/parser/queries/rust_rels.scm internal/parser/handler_rust.go
git commit -m "feat(parser): add Rust type relationship extraction (trait impl)"
```

---

### Task 5: Enrich handler_rust.go — visibility, attributes, receiver

**Files:**
- Modify: `internal/parser/handler_rust.go`

**Step 1: Write test for enriched Rust symbols**

Create `internal/parser/rust_enrich_test.go`:

```go
package parser_test

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestRustVisibilityAndAttributes(t *testing.T) {
	source := []byte(`
use std::io;

#[derive(Debug, Clone)]
pub struct Config {
    host: String,
}

pub fn public_function() {}

fn private_function() {}

#[test]
fn test_something() {}

#[tokio::test]
async fn test_async() {}

impl Config {
    pub fn new() -> Self { Config { host: String::new() } }
    fn secret() {}
}

pub trait Handler {
    fn handle(&self);
}

impl Handler for Config {
    fn handle(&self) {}
}
`)
	result, err := parser.ParseFile("test.rs", source, parser.ParseOpts{
		IncludeBody: true,
	})
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	byName := make(map[string]*parser.Symbol)
	for _, sym := range result.Symbols {
		byName[sym.Name] = sym
	}

	// Visibility checks
	tests := []struct {
		name     string
		isPublic bool
	}{
		{"Config", true},
		{"public_function", true},
		{"private_function", false},
		{"new", true},
		{"secret", false},
		{"Handler", true},
	}
	for _, tc := range tests {
		sym, ok := byName[tc.name]
		if !ok {
			t.Errorf("symbol %q not found", tc.name)
			continue
		}
		if sym.IsPublic != tc.isPublic {
			t.Errorf("%q: IsPublic = %v, want %v", tc.name, sym.IsPublic, tc.isPublic)
		}
	}

	// Attribute checks
	if sym, ok := byName["Config"]; ok {
		found := false
		for _, attr := range sym.Attributes {
			if attr == "#[derive(Debug, Clone)]" {
				found = true
			}
		}
		if !found {
			t.Errorf("Config missing #[derive(Debug, Clone)] attribute; got %v", sym.Attributes)
		}
	}

	if sym, ok := byName["test_something"]; ok {
		found := false
		for _, attr := range sym.Attributes {
			if attr == "#[test]" {
				found = true
			}
		}
		if !found {
			t.Errorf("test_something missing #[test] attribute; got %v", sym.Attributes)
		}
	}

	// Receiver checks
	if sym, ok := byName["new"]; ok {
		if sym.Receiver != "Config" {
			t.Errorf("new: Receiver = %q, want %q", sym.Receiver, "Config")
		}
	}
	if sym, ok := byName["handle"]; ok {
		if sym.Receiver != "Handler for Config" {
			t.Errorf("handle: Receiver = %q, want %q", sym.Receiver, "Handler for Config")
		}
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/parser/ -run TestRustVisibilityAndAttributes -v -count=1`
Expected: FAIL — IsPublic, Attributes, Receiver all empty

**Step 3: Add helper functions to handler_rust.go**

Add these functions at the end of `handler_rust.go`:

```go
// hasVisibilityModifier checks if a Rust node has a `pub` visibility modifier.
func hasVisibilityModifier(node *sitter.Node) bool {
	count := int(node.ChildCount())
	for i := range count {
		child := node.Child(i)
		if child != nil && child.Type() == "visibility_modifier" {
			return true
		}
	}
	return false
}

// extractRustAttributes collects #[...] attribute items preceding a node.
// Walks backward through previous siblings, stopping at non-comment/non-attribute nodes.
func extractRustAttributes(node *sitter.Node, source []byte) []string {
	parent := node.Parent()
	if parent == nil {
		return nil
	}
	idx := nodeIndex(node, parent)
	var attrs []string
	for i := idx - 1; i >= 0; i-- {
		sib := parent.Child(i)
		if sib == nil {
			break
		}
		switch sib.Type() {
		case "attribute_item":
			attrs = append(attrs, sib.Content(source))
		case "line_comment", "block_comment":
			continue
		default:
			goto done
		}
	}
done:
	// Reverse to preserve source order.
	for i, j := 0, len(attrs)-1; i < j; i, j = i+1, j-1 {
		attrs[i], attrs[j] = attrs[j], attrs[i]
	}
	return attrs
}

// nodeIndex returns the index of node within parent's children.
func nodeIndex(node, parent *sitter.Node) int {
	count := int(parent.ChildCount())
	for i := range count {
		if parent.Child(i) == node {
			return i
		}
	}
	return -1
}

// implReceiver extracts the receiver type from a method's parent impl_item.
// Returns "Type" for plain impl, "Trait for Type" for trait impl.
func implReceiver(methodNode *sitter.Node, source []byte) string {
	// method (function_item) → parent (declaration_list) → parent (impl_item)
	declList := methodNode.Parent()
	if declList == nil || declList.Type() != "declaration_list" {
		return ""
	}
	implNode := declList.Parent()
	if implNode == nil || implNode.Type() != "impl_item" {
		return ""
	}
	traitNode := implNode.ChildByFieldName("trait")
	typeNode := implNode.ChildByFieldName("type")
	if typeNode == nil {
		return ""
	}
	typeName := typeNode.Content(source)
	if traitNode != nil {
		return traitNode.Content(source) + " for " + typeName
	}
	return typeName
}
```

**Step 4: Update all map* methods to populate new fields**

Replace `mapFunction`:
```go
func (h *rustHandler) mapFunction(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:       nameNode.Content(source),
		Kind:       KindFunction,
		Language:   "rust",
		StartLine:  node.StartPoint().Row + 1,
		EndLine:    node.EndPoint().Row + 1,
		Signature:  extractSignature(node, source),
		IsPublic:   hasVisibilityModifier(node),
		Attributes: extractRustAttributes(node, source),
	}
}
```

Replace `mapMethod`:
```go
func (h *rustHandler) mapMethod(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:       nameNode.Content(source),
		Kind:       KindMethod,
		Language:   "rust",
		StartLine:  node.StartPoint().Row + 1,
		EndLine:    node.EndPoint().Row + 1,
		Signature:  extractSignature(node, source),
		IsPublic:   hasVisibilityModifier(node),
		Attributes: extractRustAttributes(node, source),
		Receiver:   implReceiver(node, source),
	}
}
```

Replace `mapType`:
```go
func (h *rustHandler) mapType(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	kind := KindType
	if node.Type() == "struct_item" {
		kind = KindStruct
	}
	return &Symbol{
		Name:       nameNode.Content(source),
		Kind:       kind,
		Language:   "rust",
		StartLine:  node.StartPoint().Row + 1,
		EndLine:    node.EndPoint().Row + 1,
		Signature:  extractSignature(node, source),
		IsPublic:   hasVisibilityModifier(node),
		Attributes: extractRustAttributes(node, source),
	}
}
```

Replace `mapInterface`:
```go
func (h *rustHandler) mapInterface(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:       nameNode.Content(source),
		Kind:       KindInterface,
		Language:   "rust",
		StartLine:  node.StartPoint().Row + 1,
		EndLine:    node.EndPoint().Row + 1,
		Signature:  extractSignature(node, source),
		IsPublic:   hasVisibilityModifier(node),
		Attributes: extractRustAttributes(node, source),
	}
}
```

Replace `mapConst`:
```go
func (h *rustHandler) mapConst(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:       nameNode.Content(source),
		Kind:       KindConst,
		Language:   "rust",
		StartLine:  node.StartPoint().Row + 1,
		EndLine:    node.EndPoint().Row + 1,
		Signature:  extractSignature(node, source),
		IsPublic:   hasVisibilityModifier(node),
		Attributes: extractRustAttributes(node, source),
	}
}
```

Replace `mapVar`:
```go
func (h *rustHandler) mapVar(node *sitter.Node, source []byte) *Symbol {
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return nil
	}
	return &Symbol{
		Name:       nameNode.Content(source),
		Kind:       KindVar,
		Language:   "rust",
		StartLine:  node.StartPoint().Row + 1,
		EndLine:    node.EndPoint().Row + 1,
		Signature:  extractSignature(node, source),
		IsPublic:   hasVisibilityModifier(node),
		Attributes: extractRustAttributes(node, source),
	}
}
```

**Step 5: Run test to verify it passes**

Run: `cd /path/to/repos/src/go-code && go test ./internal/parser/ -run TestRustVisibilityAndAttributes -v -count=1`
Expected: PASS

**Step 6: Run all parser tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/parser/ -v -count=1 2>&1 | tail -5`
Expected: all PASS

**Step 7: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/parser/handler_rust.go internal/parser/
git commit -m "feat(parser): enrich Rust symbols with visibility, attributes, receiver"
```

---

### Task 6: Rust-aware dead code detection

**Files:**
- Modify: `internal/deadcode/deadcode.go`
- Modify: `internal/deadcode/deadcode_test.go`

**Step 1: Write failing tests for Rust dead code scenarios**

Add to `internal/deadcode/deadcode_test.go`:

```go
// TestAnalyze_RustTestAttributeNotDead verifies that #[test] functions are skipped.
func TestAnalyze_RustTestAttributeNotDead(t *testing.T) {
	testFn := &parser.Symbol{
		Name: "test_something", Kind: parser.KindFunction,
		Language: "rust", File: "/src/lib.rs", StartLine: 10, EndLine: 20,
		Attributes: []string{"#[test]"},
	}
	asyncTestFn := &parser.Symbol{
		Name: "test_async_thing", Kind: parser.KindFunction,
		Language: "rust", File: "/src/lib.rs", StartLine: 25, EndLine: 35,
		Attributes: []string{"#[tokio::test]"},
	}
	reallyDead := &parser.Symbol{
		Name: "unused_helper", Kind: parser.KindFunction,
		Language: "rust", File: "/src/lib.rs", StartLine: 40, EndLine: 45,
	}
	mainSym := &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		Language: "rust", File: "/src/main.rs", StartLine: 1, EndLine: 5,
	}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{testFn, asyncTestFn, reallyDead, mainSym},
		Edges:   nil,
	}

	result := Analyze(cg, Options{})
	if result.DeadCount != 1 {
		t.Errorf("expected 1 dead (unused_helper), got %d", result.DeadCount)
		for _, d := range result.DeadSymbols {
			t.Logf("  dead: %s", d.Name)
		}
	}
}

// TestAnalyze_RustPubVisibility verifies IsPublic controls exported classification.
func TestAnalyze_RustPubVisibility(t *testing.T) {
	pubFn := &parser.Symbol{
		Name: "new", Kind: parser.KindMethod,
		Language: "rust", File: "/src/lib.rs", StartLine: 10, EndLine: 20,
		IsPublic: true,
	}
	privateFn := &parser.Symbol{
		Name: "helper", Kind: parser.KindFunction,
		Language: "rust", File: "/src/lib.rs", StartLine: 25, EndLine: 30,
		IsPublic: false,
	}
	mainSym := &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		Language: "rust", File: "/src/main.rs", StartLine: 1, EndLine: 5,
	}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{pubFn, privateFn, mainSym},
		Edges:   nil,
	}

	// Default: pub symbols skipped (like exported in Go).
	result := Analyze(cg, Options{})
	if result.DeadCount != 1 {
		t.Errorf("expected 1 dead (helper), got %d", result.DeadCount)
		for _, d := range result.DeadSymbols {
			t.Logf("  dead: %s (exported=%v)", d.Name, d.Exported)
		}
	}
	if result.DeadCount == 1 && result.DeadSymbols[0].Name != "helper" {
		t.Errorf("expected dead 'helper', got %q", result.DeadSymbols[0].Name)
	}
}

// TestAnalyze_RustWellKnownTraitMethods verifies Rust trait methods are skipped.
func TestAnalyze_RustWellKnownTraitMethods(t *testing.T) {
	methods := []string{"fmt", "clone", "drop", "default", "from", "into", "next", "eq", "hash", "poll", "serialize", "deserialize"}
	var symbols []*parser.Symbol
	for i, name := range methods {
		symbols = append(symbols, &parser.Symbol{
			Name: name, Kind: parser.KindMethod,
			Language: "rust", File: "/src/lib.rs",
			StartLine: uint32(i*10 + 1), EndLine: uint32(i*10 + 5),
			Receiver: "SomeTrait for MyType",
		})
	}
	symbols = append(symbols, &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		Language: "rust", File: "/src/main.rs", StartLine: 1, EndLine: 5,
	})

	cg := &callgraph.CallGraph{Symbols: symbols, Edges: nil}
	result := Analyze(cg, Options{})

	if result.DeadCount != 0 {
		t.Errorf("expected 0 dead (all well-known trait methods), got %d", result.DeadCount)
		for _, d := range result.DeadSymbols {
			t.Logf("  dead: %s", d.Name)
		}
	}
}

// TestAnalyze_RustTraitImplMethodConfidence verifies trait impl methods get medium confidence.
func TestAnalyze_RustTraitImplMethodConfidence(t *testing.T) {
	traitMethod := &parser.Symbol{
		Name: "custom_method", Kind: parser.KindMethod,
		Language: "rust", File: "/src/lib.rs", StartLine: 10, EndLine: 20,
		Receiver: "MyTrait for MyType",
	}
	mainSym := &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		Language: "rust", File: "/src/main.rs", StartLine: 1, EndLine: 5,
	}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{traitMethod, mainSym},
		Edges:   nil,
	}

	result := Analyze(cg, Options{})
	if result.DeadCount != 1 {
		t.Fatalf("expected 1 dead, got %d", result.DeadCount)
	}
	if result.DeadSymbols[0].Confidence != ConfidenceMedium {
		t.Errorf("trait impl method should be medium confidence, got %q", result.DeadSymbols[0].Confidence)
	}
}

// TestAnalyze_RustTestFileRs verifies _test.rs files are recognized.
func TestAnalyze_RustTestFileRs(t *testing.T) {
	sym := &parser.Symbol{
		Name: "helper_in_test", Kind: parser.KindFunction,
		Language: "rust", File: "/src/foo_test.rs", StartLine: 1, EndLine: 10,
	}
	mainSym := &parser.Symbol{
		Name: "main", Kind: parser.KindFunction,
		Language: "rust", File: "/src/main.rs", StartLine: 1, EndLine: 5,
	}

	cg := &callgraph.CallGraph{
		Symbols: []*parser.Symbol{sym, mainSym},
		Edges:   nil,
	}

	result := Analyze(cg, Options{})
	if result.DeadCount != 0 {
		t.Errorf("expected 0 dead (_test.rs skipped), got %d", result.DeadCount)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /path/to/repos/src/go-code && go test ./internal/deadcode/ -run "TestAnalyze_Rust" -v -count=1`
Expected: FAIL — multiple tests fail

**Step 3: Implement Rust-aware dead code filtering**

Modify `internal/deadcode/deadcode.go`:

a) Add `rustWellKnownMethods` map after `wellKnownInterfaceMethods`:

```go
// rustWellKnownMethods are Rust trait method names that should not be flagged as dead.
var rustWellKnownMethods = map[string]bool{
	"fmt": true, "clone": true, "drop": true, "default": true,
	"from": true, "into": true, "try_from": true, "try_into": true,
	"as_ref": true, "as_mut": true, "deref": true, "deref_mut": true,
	"next": true, "into_iter": true, "to_string": true,
	"serialize": true, "deserialize": true, "poll": true,
	"source": true, "description": true,
	"eq": true, "ne": true, "partial_cmp": true, "cmp": true, "hash": true,
	"add": true, "sub": true, "mul": true, "div": true,
	"index": true, "index_mut": true,
	"borrow": true, "borrow_mut": true,
}
```

b) Update `shouldSkipSymbol` — add Rust attribute check before `isTestFunc`:

```go
func shouldSkipSymbol(sym *parser.Symbol, opts Options) bool {
	if isEntryPoint(sym.Name) || isTestFunc(sym.Name) {
		return true
	}
	if hasTestAttribute(sym) {
		return true
	}
	if constructorNames[sym.Name] {
		return true
	}
	if isHTTPHandler(sym) || isWellKnownInterfaceMethod(sym) {
		return true
	}
	if isRustWellKnownMethod(sym) {
		return true
	}
	if !opts.IncludeTests && isTestFile(sym.File) {
		return true
	}
	if !opts.IncludeExported && isSymbolExported(sym) {
		return true
	}
	return false
}
```

c) Add new helper functions:

```go
// hasTestAttribute checks if a symbol has a test-related attribute (Rust #[test], #[tokio::test], etc.).
func hasTestAttribute(sym *parser.Symbol) bool {
	for _, attr := range sym.Attributes {
		if strings.Contains(attr, "test") {
			return true
		}
	}
	return false
}

// isRustWellKnownMethod checks if a Rust method name matches a well-known trait method.
func isRustWellKnownMethod(sym *parser.Symbol) bool {
	return sym.Language == "rust" && sym.Kind == parser.KindMethod && rustWellKnownMethods[sym.Name]
}

// isSymbolExported returns true if the symbol is public/exported.
// Uses IsPublic field for languages that set it (Rust), falls back to Go uppercase convention.
func isSymbolExported(sym *parser.Symbol) bool {
	if sym.IsPublic {
		return true
	}
	// Go fallback: uppercase first letter.
	if sym.Language == "rust" {
		return false
	}
	return isExported(sym.Name)
}
```

d) Update `collectDeadSymbols` to use `isSymbolExported`:

Replace line 174:
```go
		exported := isSymbolExported(sym)
```

e) Update `classifyConfidence` to handle trait impl methods:

```go
func classifyConfidence(sym *parser.Symbol, exported bool) string {
	if exported {
		return ConfidenceLow
	}
	if sym.Kind == parser.KindMethod {
		return ConfidenceMedium
	}
	// Rust: trait impl methods with Receiver containing "for" → medium.
	if sym.Receiver != "" && strings.Contains(sym.Receiver, " for ") {
		return ConfidenceMedium
	}
	return ConfidenceHigh
}
```

f) Update `isTestFile` — add `_test.rs`:

```go
	testSuffixes := []string{
		"_test.go", "_test.py", "_test.rs",
		".test.ts", ".test.js",
		".spec.ts", ".spec.js",
	}
```

**Step 4: Run tests to verify they pass**

Run: `cd /path/to/repos/src/go-code && go test ./internal/deadcode/ -v -count=1 2>&1 | tail -10`
Expected: all PASS (existing + new Rust tests)

**Step 5: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/deadcode/
git commit -m "feat(deadcode): Rust-aware filtering — test attrs, pub visibility, trait methods"
```

---

### Task 7: Rust import resolution for dep_graph

**Files:**
- Modify: `internal/analyze/analyze.go:630-666`

**Step 1: Write test**

Add to existing analyze test file or create `internal/analyze/rust_imports_test.go`:

```go
func TestBuildImportGraph_Rust(t *testing.T) {
	// Simulate Rust parse results with use declarations
	results := []fileParseResult{
		{
			file: &ingest.File{Path: "/repo/src/client.rs", Language: "rust"},
			result: &parser.ParseResult{
				Language: "rust",
				Imports:  []string{"crate::config", "crate::error", "std::sync::Arc", "reqwest::Client"},
			},
		},
		{
			file: &ingest.File{Path: "/repo/src/config.rs", Language: "rust"},
			result: &parser.ParseResult{
				Language: "rust",
				Imports:  []string{"std::time::Duration", "serde::Deserialize"},
			},
		},
	}

	graph := buildImportGraph("/repo", results, false)

	// src/client.rs package should have edges to crate::config (internal)
	// but NOT to std::sync::Arc (stdlib)
	clientPkg := "src"
	deps, ok := graph[clientPkg]
	if !ok {
		t.Fatalf("package %q not in graph; keys: %v", clientPkg, graphKeys(graph))
	}

	// Internal crate imports should create edges
	if _, ok := deps["crate::config"]; !ok {
		// At minimum, non-stdlib imports should be present
		t.Logf("deps for %s: %v", clientPkg, setKeys(deps))
	}

	// Stdlib should be filtered
	if _, ok := deps["std::sync::Arc"]; ok {
		t.Error("stdlib import std::sync::Arc should be filtered")
	}
}
```

Note: The exact test structure depends on how `buildImportGraph` is modified. The key contract is:
- `crate::*` imports create internal edges
- `std::`/`core::`/`alloc::` imports are filtered (stdlib)
- Other imports (`reqwest`, `serde`) are external deps

**Step 2: Implement Rust import resolution**

In `internal/analyze/analyze.go`, modify `buildImportGraph`:

```go
func buildImportGraph(root string, results []fileParseResult, includeStdlib bool) importGraph {
	graph := make(importGraph)

	for _, pr := range results {
		if pr.result == nil || len(pr.result.Imports) == 0 {
			continue
		}
		pkg := goutil.PackageDir(root, pr.file.Path)
		if pr.result.Language == "rust" {
			addRustImports(graph, pkg, pr.result.Imports, includeStdlib)
		} else {
			addImports(graph, pkg, pr.result.Imports, includeStdlib)
		}
	}

	return graph
}
```

Add `addRustImports` and `isRustStdlib`:

```go
// addRustImports handles Rust use declarations for the import graph.
// crate:: imports map to internal packages, std::/core::/alloc:: are stdlib.
func addRustImports(graph importGraph, pkg string, imports []string, includeStdlib bool) {
	if _, ok := graph[pkg]; !ok {
		graph[pkg] = make(map[string]struct{})
	}
	for _, imp := range imports {
		if imp == "" {
			continue
		}
		// Clean tree-sitter artifacts.
		imp = strings.Trim(imp, "\"'{}")

		if !includeStdlib && isRustStdlib(imp) {
			continue
		}
		// Skip self-imports.
		if strings.HasPrefix(imp, "self::") {
			continue
		}
		graph[pkg][imp] = struct{}{}
	}
}

// isRustStdlib returns true for Rust standard library imports.
func isRustStdlib(imp string) bool {
	root, _, _ := strings.Cut(imp, "::")
	switch root {
	case "std", "core", "alloc":
		return true
	}
	return false
}
```

Also update `explore/deps.go` — `buildDepHighlights` uses `goutil.IsStdlibImport`. We need language awareness there too. The simplest approach: check if the import contains `::` (Rust separator) and use `isRustStdlib` from analyze package. However, to avoid circular imports, add an equivalent check inline:

In `internal/explore/deps.go:40`, change:
```go
		if imp == "" || goutil.IsStdlibImport(imp) {
```
to:
```go
		if imp == "" || isStdlibImport(imp) {
```

Add to `explore/deps.go`:
```go
// isStdlibImport checks for stdlib imports across languages.
func isStdlibImport(imp string) bool {
	// Rust: std::, core::, alloc::
	if strings.Contains(imp, "::") {
		root, _, _ := strings.Cut(imp, "::")
		return root == "std" || root == "core" || root == "alloc"
	}
	// Go: no dots in first path segment.
	return goutil.IsStdlibImport(imp)
}
```

**Step 3: Run tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/analyze/ -v -count=1 2>&1 | tail -10`
Expected: all PASS

**Step 4: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/analyze/ internal/explore/
git commit -m "feat(analyze): Rust import resolution for dep_graph — crate/std/external"
```

---

### Task 8: Cargo.toml parser for external deps

**Files:**
- Create: `internal/polyglot/cargo.go`
- Create: `internal/polyglot/cargo_test.go`

**Step 1: Write test**

Create `internal/polyglot/cargo_test.go`:

```go
package polyglot

import "testing"

func TestParseCargoToml(t *testing.T) {
	content := []byte(`
[package]
name = "ox-browser"
version = "0.1.0"
edition = "2024"

[dependencies]
reqwest = { version = "0.12", features = ["cookies"] }
tokio = { version = "1", features = ["full"] }
serde = "1.0"

[dev-dependencies]
mockall = "0.13"
`)

	info := ParseCargoToml(content)

	if info.Name != "ox-browser" {
		t.Errorf("Name = %q, want %q", info.Name, "ox-browser")
	}
	if info.Edition != "2024" {
		t.Errorf("Edition = %q, want %q", info.Edition, "2024")
	}
	if len(info.Dependencies) != 3 {
		t.Errorf("Dependencies count = %d, want 3; got %v", len(info.Dependencies), info.Dependencies)
	}
	if len(info.DevDependencies) != 1 {
		t.Errorf("DevDependencies count = %d, want 1", len(info.DevDependencies))
	}
}

func TestParseCargoToml_Workspace(t *testing.T) {
	content := []byte(`
[workspace]
members = ["crates/core", "crates/http", "crates/mcp"]
resolver = "2"
`)

	info := ParseCargoToml(content)

	if len(info.WorkspaceMembers) != 3 {
		t.Errorf("WorkspaceMembers = %d, want 3; got %v", len(info.WorkspaceMembers), info.WorkspaceMembers)
	}
}

func TestParseCargoToml_Empty(t *testing.T) {
	info := ParseCargoToml([]byte(""))
	if info.Name != "" {
		t.Errorf("expected empty name for empty input")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/polyglot/ -run TestParseCargo -v -count=1`
Expected: FAIL — function doesn't exist

**Step 3: Implement Cargo.toml parser**

Create `internal/polyglot/cargo.go`:

```go
package polyglot

import (
	"regexp"
	"strings"
)

// CargoInfo contains metadata extracted from a Cargo.toml file.
type CargoInfo struct {
	Name             string
	Version          string
	Edition          string
	Dependencies     []string
	DevDependencies  []string
	WorkspaceMembers []string
}

// ParseCargoToml extracts dependency and metadata from Cargo.toml content.
// Uses simple line-based parsing to avoid adding a TOML dependency.
func ParseCargoToml(data []byte) CargoInfo {
	var info CargoInfo
	lines := strings.Split(string(data), "\n")

	var section string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Track current section.
		if strings.HasPrefix(trimmed, "[") {
			section = extractSection(trimmed)
			continue
		}

		// Skip comments and empty lines.
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		key, val := splitKV(trimmed)
		if key == "" {
			continue
		}

		switch section {
		case "package":
			switch key {
			case "name":
				info.Name = val
			case "version":
				info.Version = val
			case "edition":
				info.Edition = val
			}
		case "dependencies":
			info.Dependencies = append(info.Dependencies, key)
		case "dev-dependencies":
			info.DevDependencies = append(info.DevDependencies, key)
		case "workspace":
			if key == "members" {
				info.WorkspaceMembers = parseTomlArray(trimmed)
			}
		}
	}
	return info
}

// extractSection returns the section name from a TOML header like [dependencies].
func extractSection(line string) string {
	line = strings.TrimPrefix(line, "[")
	line = strings.TrimSuffix(line, "]")
	return strings.TrimSpace(line)
}

// splitKV splits "key = value" and strips quotes from value.
func splitKV(line string) (string, string) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", ""
	}
	key := strings.TrimSpace(parts[0])
	val := strings.TrimSpace(parts[1])
	val = strings.Trim(val, `"'`)
	return key, val
}

var tomlArrayRe = regexp.MustCompile(`"([^"]+)"`)

// parseTomlArray extracts strings from a TOML inline array like ["a", "b", "c"].
func parseTomlArray(line string) []string {
	matches := tomlArrayRe.FindAllStringSubmatch(line, -1)
	result := make([]string, 0, len(matches))
	for _, m := range matches {
		result = append(result, m[1])
	}
	return result
}
```

**Step 4: Run test to verify it passes**

Run: `cd /path/to/repos/src/go-code && go test ./internal/polyglot/ -run TestParseCargo -v -count=1`
Expected: PASS

**Step 5: Wire into explore's dep highlights**

In `internal/explore/deps.go`, after building the graph from imports, also check for Cargo.toml in the root and add its dependencies to `allExternal`. This integration is optional for the first pass — the Cargo parser is immediately useful for `repo_analyze` output and `code_health` external dep count.

The simplest integration: in the explore tool handler, after file walking, find `Cargo.toml` and add its deps to the external deps count. This can be done in a follow-up commit.

**Step 6: Commit**

```bash
cd /path/to/repos/src/go-code
git add internal/polyglot/cargo.go internal/polyglot/cargo_test.go
git commit -m "feat(polyglot): add Cargo.toml parser for Rust dependency extraction"
```

---

### Task 9: Integration test with ox-browser

**Files:**
- No new files — run existing tools against the real Rust codebase

**Step 1: Build and deploy**

```bash
cd /path/to/repos/src/go-code
make build
cd /path/to/repos/deploy/example-server
docker compose build --no-cache go-code && docker compose up -d --no-deps --force-recreate go-code
```

**Step 2: Verify with MCP tools against ox-browser**

Test each improved tool:
- `explore` — check external_deps is no longer 0
- `dead_code` — verify test functions are not listed, dead ratio drops significantly
- `symbol_search kind=trait` — should return traits without query
- `dep_graph` — should show edges between crates
- `call_trace` (callees) — verify no closure param false positives

**Step 3: Run all go-code tests**

```bash
cd /path/to/repos/src/go-code && make test
```

**Step 4: Commit any fixes**

```bash
cd /path/to/repos/src/go-code
git add -A
git commit -m "test: verify Rust first-class support with ox-browser integration"
```
