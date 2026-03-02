# Type Hierarchy Extraction + Graph Enrichment (P4) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add IMPLEMENTS/INHERITS/EMBEDS edge extraction to the parser and code graph, enabling type hierarchy queries ("who implements this interface?", "what does this class extend?") across Go, Python, TypeScript, and Java.

**Architecture:** New `TypeRelationship` struct in the parser package (parallel to `CallSite`), extracted via dedicated `.scm` query files per language. A new `RelationshipQueryProvider` interface follows the existing `CallQueryProvider` pattern. Relationships are resolved against the symbol table and stored as graph edges. New Cypher templates expose type hierarchy queries. The graph schema text is updated so LLM freeform queries can reference the new edges.

**Tech Stack:** Go 1.26+, tree-sitter (existing grammars), Apache AGE (existing)

---

### Task 1: Add TypeRelationship struct + Go relationship queries

**Context:** This task adds the core `TypeRelationship` type and the Go extraction query. Go has two type relationship patterns: (1) struct embedding (`type Foo struct { Bar }`) and (2) interface method sets (implicit — Go doesn't have `implements` keyword, but we can extract embedded interfaces from interface definitions). For Go structs, we extract embedded type names from the struct body.

**Files:**
- Create: `/path/to/repos/src/go-code/internal/parser/relationships.go`
- Create: `/path/to/repos/src/go-code/internal/parser/relationships_test.go`
- Create: `/path/to/repos/src/go-code/internal/parser/queries/go_rels.scm`
- Modify: `/path/to/repos/src/go-code/internal/parser/handler.go` (add interface + capture constants)
- Modify: `/path/to/repos/src/go-code/internal/parser/handler_go.go` (implement RelationshipQueryProvider)

**Step 1: Add capture constants and RelationshipQueryProvider interface**

In `/path/to/repos/src/go-code/internal/parser/handler.go`, add after the existing capture constants:

```go
const (
	// ... existing constants ...

	captureRelSubject = "rel.subject"
	captureRelTarget  = "rel.target"
	captureRelKind    = "rel.kind"
)
```

Add after `CallQueryProvider`:

```go
// RelationshipQueryProvider is an optional interface that LanguageHandler
// implementations can satisfy to support type relationship extraction
// (embedding, inheritance, interface implementation).
type RelationshipQueryProvider interface {
	RelationshipsQuery() *sitter.Query
}
```

**Step 2: Write the failing test**

Create `/path/to/repos/src/go-code/internal/parser/relationships_test.go`:

```go
package parser

import (
	"testing"
)

func TestExtractRelationships_Go(t *testing.T) {
	src := `package p

type Reader interface {
	Read(p []byte) (n int, err error)
}

type ReadCloser interface {
	Reader
	Close() error
}

type MyReader struct {
	io.Reader
	buf []byte
}

type SimpleStruct struct {
	name string
}
`
	rels, err := ExtractRelationships("/test/example.go", []byte(src), ParseOpts{Language: "go"})
	if err != nil {
		t.Fatal(err)
	}

	// ReadCloser embeds Reader (interface composition).
	// MyReader embeds io.Reader (struct embedding).
	// SimpleStruct has no embeddings.
	if len(rels) < 2 {
		t.Fatalf("expected at least 2 relationships, got %d: %+v", len(rels), rels)
	}

	found := map[string]bool{}
	for _, r := range rels {
		key := r.Subject + "->" + r.Target + ":" + string(r.Kind)
		found[key] = true
		t.Logf("rel: %s %s %s (line %d)", r.Subject, r.Kind, r.Target, r.Line)
	}

	if !found["ReadCloser->Reader:embeds"] {
		t.Error("expected ReadCloser->Reader embeds relationship")
	}
	if !found["MyReader->Reader:embeds"] {
		t.Error("expected MyReader->Reader embeds relationship")
	}
}

func TestExtractRelationships_Unsupported(t *testing.T) {
	rels, err := ExtractRelationships("/test/example.rb", []byte("class Foo; end"), ParseOpts{Language: "ruby"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) != 0 {
		t.Errorf("expected empty for unsupported, got %d", len(rels))
	}
}
```

**Step 3: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/parser/ -run TestExtractRelationships -v`
Expected: FAIL — `ExtractRelationships` undefined

**Step 4: Create the Go relationship query file**

Create `/path/to/repos/src/go-code/internal/parser/queries/go_rels.scm`:

```lisp
; Go type relationship extraction.
; Captures embedded types in struct and interface definitions.

; Struct embedding: type Foo struct { Bar; io.Reader }
; The qualified_type and type_identifier inside a field_declaration
; without a field name are embedded types.
(type_declaration
  (type_spec
    name: (type_identifier) @rel.subject
    type: (struct_type
      (field_declaration_list
        (field_declaration
          type: [
            (type_identifier) @rel.target
            (qualified_type) @rel.target
          ])))))

; Interface embedding: type Foo interface { Bar; io.Reader }
(type_declaration
  (type_spec
    name: (type_identifier) @rel.subject
    type: (interface_type
      [
        (type_identifier) @rel.target
        (qualified_type) @rel.target
      ])))
```

**Step 5: Implement relationships.go**

Create `/path/to/repos/src/go-code/internal/parser/relationships.go`:

```go
package parser

import (
	"context"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// RelKind describes the type of relationship between two symbols.
type RelKind string

const (
	// RelEmbeds means the subject type embeds the target type (Go struct/interface embedding).
	RelEmbeds RelKind = "embeds"

	// RelExtends means the subject class extends the target class (Python, Java, TypeScript).
	RelExtends RelKind = "extends"

	// RelImplements means the subject class implements the target interface (Java, TypeScript).
	RelImplements RelKind = "implements"
)

// TypeRelationship represents a type hierarchy relationship extracted from source code.
type TypeRelationship struct {
	Subject string  // the type/class that has the relationship (e.g. "MyReader")
	Target  string  // the type/class/interface being referenced (e.g. "Reader")
	Kind    RelKind // embeds, extends, or implements
	Line    uint32  // 1-based line number of the subject type
	File    string  // absolute file path
}

// ExtractRelationships parses a source file and returns all type relationships.
// Returns empty slice (not error) for unsupported languages.
func ExtractRelationships(path string, source []byte, opts ParseOpts) ([]TypeRelationship, error) {
	ext := filepath.Ext(path)
	handler := HandlerForExt(ext)
	if handler == nil {
		return nil, nil
	}

	rqp, ok := handler.(RelationshipQueryProvider)
	if !ok || rqp.RelationshipsQuery() == nil {
		return nil, nil
	}

	p := sitter.NewParser()
	p.SetLanguage(handler.SitterLanguage())

	tree, err := p.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return nil, err
	}

	return runRelationshipQuery(rqp.RelationshipsQuery(), tree.RootNode(), source, path, handler.Language()), nil
}

func runRelationshipQuery(q *sitter.Query, root *sitter.Node, source []byte, path, language string) []TypeRelationship {
	qc := sitter.NewQueryCursor()
	qc.Exec(q, root)

	var rels []TypeRelationship
	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}

		var subject, target string
		var line uint32
		for _, capture := range match.Captures {
			capName := q.CaptureNameForId(capture.Index)
			text := capture.Node.Content(source)

			switch capName {
			case captureRelSubject:
				subject = text
				line = capture.Node.StartPoint().Row + 1
			case captureRelTarget:
				target = text
			}
		}

		if subject == "" || target == "" {
			continue
		}

		// Strip package qualifier for graph resolution (e.g. "io.Reader" → "Reader").
		shortTarget := target
		if idx := strings.LastIndexByte(target, '.'); idx >= 0 {
			shortTarget = target[idx+1:]
		}

		kind := inferRelKind(language, subject, target)

		rels = append(rels, TypeRelationship{
			Subject: subject,
			Target:  shortTarget,
			Kind:    kind,
			Line:    line,
			File:    path,
		})
	}

	return deduplicateRels(rels)
}

// inferRelKind determines the relationship kind based on language conventions.
func inferRelKind(language, _, _ string) RelKind {
	switch language {
	case "go":
		// Go uses embedding for both struct composition and interface composition.
		return RelEmbeds
	case "java":
		// Java: handled per-capture in the query (extends vs implements).
		// Default to extends; overridden by capture metadata if available.
		return RelExtends
	default:
		return RelExtends
	}
}

// deduplicateRels removes duplicate relationships (same subject+target+kind).
func deduplicateRels(rels []TypeRelationship) []TypeRelationship {
	type key struct {
		subject, target string
		kind            RelKind
	}
	seen := make(map[key]bool)
	var out []TypeRelationship
	for _, r := range rels {
		k := key{r.Subject, r.Target, r.Kind}
		if !seen[k] {
			seen[k] = true
			out = append(out, r)
		}
	}
	return out
}
```

**Step 6: Wire Go handler to compile and expose the query**

In `/path/to/repos/src/go-code/internal/parser/handler_go.go`:

Add embed directive after existing ones:

```go
//go:embed queries/go_rels.scm
var goRelsQueryBytes []byte
```

Add `relsQuery` field to `goHandler`:

```go
type goHandler struct {
	lang      *sitter.Language
	query     *sitter.Query
	callQuery *sitter.Query
	relsQuery *sitter.Query
}
```

In `init()`, compile the query and assign it:

```go
func init() {
	lang := golang.GetLanguage()
	q, err := sitter.NewQuery(goQueryBytes, lang)
	if err != nil {
		panic("go.scm query compile error: " + err.Error())
	}
	cq, err := sitter.NewQuery(goCallsQueryBytes, lang)
	if err != nil {
		panic("go_calls.scm query compile error: " + err.Error())
	}
	rq, err := sitter.NewQuery(goRelsQueryBytes, lang)
	if err != nil {
		panic("go_rels.scm query compile error: " + err.Error())
	}
	goLang.lang = lang
	goLang.query = q
	goLang.callQuery = cq
	goLang.relsQuery = rq
	registerHandler(goLang)
}
```

Add the method:

```go
func (h *goHandler) RelationshipsQuery() *sitter.Query { return h.relsQuery }
```

**Step 7: Run tests to verify they pass**

Run: `cd /path/to/repos/src/go-code && go test ./internal/parser/ -run TestExtractRelationships -v`
Expected: PASS

Run: `cd /path/to/repos/src/go-code && go test ./internal/parser/ -v -count=1`
Expected: All PASS

**Step 8: Commit**

```bash
cd /path/to/repos/src/go-code
sudo -u example git add internal/parser/relationships.go internal/parser/relationships_test.go internal/parser/queries/go_rels.scm internal/parser/handler.go internal/parser/handler_go.go
sudo -u example git commit -m "feat(parser): add TypeRelationship extraction for Go struct/interface embedding

New RelationshipQueryProvider interface (parallel to CallQueryProvider).
Go handler extracts embedded types from struct and interface definitions.
Supports both simple (Bar) and qualified (io.Reader) embedded type names.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 2: Add relationship queries for Python, TypeScript, and Java

**Context:** Python uses `class Foo(Bar, Baz)` for inheritance. TypeScript uses `class Foo extends Bar implements IBaz`. Java uses `class Foo extends Bar implements IBaz`. Each needs a dedicated `.scm` query file and handler wiring.

**Files:**
- Create: `/path/to/repos/src/go-code/internal/parser/queries/python_rels.scm`
- Create: `/path/to/repos/src/go-code/internal/parser/queries/typescript_rels.scm`
- Create: `/path/to/repos/src/go-code/internal/parser/queries/java_rels.scm`
- Modify: `/path/to/repos/src/go-code/internal/parser/handler_python.go` (implement RelationshipQueryProvider)
- Modify: `/path/to/repos/src/go-code/internal/parser/handler_typescript.go` (implement RelationshipQueryProvider)
- Modify: `/path/to/repos/src/go-code/internal/parser/handler_java.go` (implement RelationshipQueryProvider)
- Modify: `/path/to/repos/src/go-code/internal/parser/relationships_test.go` (add tests)

**Step 1: Write the failing tests**

Add to `/path/to/repos/src/go-code/internal/parser/relationships_test.go`:

```go
func TestExtractRelationships_Python(t *testing.T) {
	src := `class Animal:
    pass

class Dog(Animal):
    pass

class ServiceDog(Dog, Trainable):
    pass
`
	rels, err := ExtractRelationships("/test/example.py", []byte(src), ParseOpts{Language: "python"})
	if err != nil {
		t.Fatal(err)
	}

	if len(rels) < 3 {
		t.Fatalf("expected at least 3 relationships, got %d: %+v", len(rels), rels)
	}

	found := map[string]bool{}
	for _, r := range rels {
		key := r.Subject + "->" + r.Target + ":" + string(r.Kind)
		found[key] = true
		t.Logf("rel: %s %s %s", r.Subject, r.Kind, r.Target)
	}

	if !found["Dog->Animal:extends"] {
		t.Error("expected Dog->Animal extends")
	}
	if !found["ServiceDog->Dog:extends"] {
		t.Error("expected ServiceDog->Dog extends")
	}
	if !found["ServiceDog->Trainable:extends"] {
		t.Error("expected ServiceDog->Trainable extends")
	}
}

func TestExtractRelationships_TypeScript(t *testing.T) {
	src := `interface Serializable {
  serialize(): string;
}

interface Loggable extends Serializable {
  log(): void;
}

class BaseModel {
  id: string;
}

class User extends BaseModel implements Serializable {
  serialize() { return ""; }
}
`
	rels, err := ExtractRelationships("/test/example.ts", []byte(src), ParseOpts{Language: "typescript"})
	if err != nil {
		t.Fatal(err)
	}

	found := map[string]bool{}
	for _, r := range rels {
		key := r.Subject + "->" + r.Target + ":" + string(r.Kind)
		found[key] = true
		t.Logf("rel: %s %s %s", r.Subject, r.Kind, r.Target)
	}

	if !found["Loggable->Serializable:extends"] {
		t.Error("expected Loggable->Serializable extends")
	}
	if !found["User->BaseModel:extends"] {
		t.Error("expected User->BaseModel extends")
	}
	if !found["User->Serializable:implements"] {
		t.Error("expected User->Serializable implements")
	}
}

func TestExtractRelationships_Java(t *testing.T) {
	src := `public class Animal {
}

public interface Runnable {
    void run();
}

public class Dog extends Animal implements Runnable {
    public void run() {}
}
`
	rels, err := ExtractRelationships("/test/example.java", []byte(src), ParseOpts{Language: "java"})
	if err != nil {
		t.Fatal(err)
	}

	found := map[string]bool{}
	for _, r := range rels {
		key := r.Subject + "->" + r.Target + ":" + string(r.Kind)
		found[key] = true
		t.Logf("rel: %s %s %s", r.Subject, r.Kind, r.Target)
	}

	if !found["Dog->Animal:extends"] {
		t.Error("expected Dog->Animal extends")
	}
	if !found["Dog->Runnable:implements"] {
		t.Error("expected Dog->Runnable implements")
	}
}
```

**Step 2: Run test to verify they fail**

Run: `cd /path/to/repos/src/go-code && go test ./internal/parser/ -run "TestExtractRelationships_(Python|TypeScript|Java)" -v`
Expected: FAIL — no relationships extracted (handlers don't implement RelationshipQueryProvider yet)

**Step 3: Create query files**

Create `/path/to/repos/src/go-code/internal/parser/queries/python_rels.scm`:

```lisp
; Python class inheritance: class Foo(Bar, Baz)
; The argument_list contains base classes.
(class_definition
  name: (identifier) @rel.subject
  superclasses: (argument_list
    (identifier) @rel.target))

; Dotted base classes: class Foo(module.Bar)
(class_definition
  name: (identifier) @rel.subject
  superclasses: (argument_list
    (attribute
      attribute: (identifier) @rel.target)))
```

Create `/path/to/repos/src/go-code/internal/parser/queries/typescript_rels.scm`:

```lisp
; Class extends: class Foo extends Bar
(class_declaration
  name: (type_identifier) @rel.subject
  (class_heritage
    (extends_clause
      value: (identifier) @rel.target)))

; Class implements: class Foo implements IBar, IBaz
(class_declaration
  name: (type_identifier) @rel.subject
  (class_heritage
    (implements_clause
      (type_identifier) @rel.target)))

; Interface extends: interface Foo extends Bar
(interface_declaration
  name: (type_identifier) @rel.subject
  (extends_type_clause
    (type_identifier) @rel.target))
```

Create `/path/to/repos/src/go-code/internal/parser/queries/java_rels.scm`:

```lisp
; Java class extends: class Dog extends Animal
(class_declaration
  name: (identifier) @rel.subject
  (superclass
    (type_identifier) @rel.target))

; Java class implements: class Dog implements Runnable
(class_declaration
  name: (identifier) @rel.subject
  (super_interfaces
    (type_list
      (type_identifier) @rel.target)))

; Java interface extends: interface Foo extends Bar
(interface_declaration
  name: (identifier) @rel.subject
  (extends_interfaces
    (type_list
      (type_identifier) @rel.target)))
```

**Step 4: Wire handlers**

For each handler (`handler_python.go`, `handler_typescript.go`, `handler_java.go`), follow the same pattern as Task 1's Go handler:

1. Add `//go:embed queries/<lang>_rels.scm` directive
2. Add `relsQuery *sitter.Query` field to the handler struct
3. Compile query in `init()` and assign to `relsQuery`
4. Add `func (h *<handler>) RelationshipsQuery() *sitter.Query { return h.relsQuery }` method

**Step 5: Update inferRelKind for TypeScript and Java**

In `/path/to/repos/src/go-code/internal/parser/relationships.go`, update `runRelationshipQuery` to pass capture context to `inferRelKind`. For TypeScript and Java, the query file captures distinguish extends vs implements via different patterns that produce different match indices.

However, since tree-sitter queries don't carry metadata per-capture, we need a different approach. The simplest: use a **second capture name** to signal the kind. Change the TypeScript and Java query files to use `@rel.extends` and `@rel.implements` instead of `@rel.target`, then handle both in the extraction loop.

**Actually**, the cleaner approach: keep `@rel.target` but add a `@rel.implements_target` capture for implements clauses. Update `runRelationshipQuery` to check for both:

In the capture constants in `handler.go`:

```go
const (
	// ... existing ...
	captureRelSubject    = "rel.subject"
	captureRelTarget     = "rel.target"
	captureRelImplTarget = "rel.impl_target"
)
```

Update TypeScript and Java queries to use `@rel.impl_target` for implements clauses:

TypeScript `typescript_rels.scm` (updated):
```lisp
; Class extends
(class_declaration
  name: (type_identifier) @rel.subject
  (class_heritage
    (extends_clause
      value: (identifier) @rel.target)))

; Class implements — uses impl_target to distinguish from extends
(class_declaration
  name: (type_identifier) @rel.subject
  (class_heritage
    (implements_clause
      (type_identifier) @rel.impl_target)))

; Interface extends
(interface_declaration
  name: (type_identifier) @rel.subject
  (extends_type_clause
    (type_identifier) @rel.target))
```

Java `java_rels.scm` (updated):
```lisp
; Class extends
(class_declaration
  name: (identifier) @rel.subject
  (superclass
    (type_identifier) @rel.target))

; Class implements — uses impl_target
(class_declaration
  name: (identifier) @rel.subject
  (super_interfaces
    (type_list
      (type_identifier) @rel.impl_target)))

; Interface extends
(interface_declaration
  name: (identifier) @rel.subject
  (extends_interfaces
    (type_list
      (type_identifier) @rel.target)))
```

Update `runRelationshipQuery` to handle `captureRelImplTarget`:

```go
func runRelationshipQuery(q *sitter.Query, root *sitter.Node, source []byte, path, language string) []TypeRelationship {
	qc := sitter.NewQueryCursor()
	qc.Exec(q, root)

	var rels []TypeRelationship
	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}

		var subject, target string
		var line uint32
		kind := RelExtends // default

		for _, capture := range match.Captures {
			capName := q.CaptureNameForId(capture.Index)
			text := capture.Node.Content(source)

			switch capName {
			case captureRelSubject:
				subject = text
				line = capture.Node.StartPoint().Row + 1
			case captureRelTarget:
				target = text
			case captureRelImplTarget:
				target = text
				kind = RelImplements
			}
		}

		if subject == "" || target == "" {
			continue
		}

		// Strip package qualifier.
		shortTarget := target
		if idx := strings.LastIndexByte(target, '.'); idx >= 0 {
			shortTarget = target[idx+1:]
		}

		// Go always uses embeds.
		if language == "go" {
			kind = RelEmbeds
		}

		rels = append(rels, TypeRelationship{
			Subject: subject,
			Target:  shortTarget,
			Kind:    kind,
			Line:    line,
			File:    path,
		})
	}

	return deduplicateRels(rels)
}
```

Remove the old `inferRelKind` function (no longer needed).

**Step 6: Run tests to verify they pass**

Run: `cd /path/to/repos/src/go-code && go test ./internal/parser/ -run TestExtractRelationships -v`
Expected: All PASS

Run: `cd /path/to/repos/src/go-code && go test ./internal/parser/ -v -count=1`
Expected: All PASS

**Step 7: Commit**

```bash
cd /path/to/repos/src/go-code
sudo -u example git add internal/parser/queries/python_rels.scm internal/parser/queries/typescript_rels.scm internal/parser/queries/java_rels.scm internal/parser/handler_python.go internal/parser/handler_typescript.go internal/parser/handler_java.go internal/parser/relationships.go internal/parser/relationships_test.go internal/parser/handler.go
sudo -u example git commit -m "feat(parser): add type relationship extraction for Python, TypeScript, Java

Python: class inheritance via argument_list (supports multiple bases).
TypeScript: extends + implements clauses for classes and interfaces.
Java: extends + implements clauses with type_list support.
Uses rel.impl_target capture to distinguish implements from extends.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 3: Wire relationships into code graph (IMPLEMENTS/INHERITS/EMBEDS edges)

**Context:** The codegraph pipeline currently extracts symbols, calls, and imports in `parse.go`. We need to add relationship extraction and build graph edges from them. The edge types: EMBEDS (Go), EXTENDS (Python/TS/Java class inheritance), IMPLEMENTS (TS/Java interface implementation). For simplicity in the graph schema, we'll use two edge labels: INHERITS (for extends + embeds) and IMPLEMENTS.

**Files:**
- Modify: `/path/to/repos/src/go-code/internal/codegraph/parse.go` (extract relationships in parallel)
- Modify: `/path/to/repos/src/go-code/internal/codegraph/graph_build.go` (build INHERITS/IMPLEMENTS edges)
- Modify: `/path/to/repos/src/go-code/internal/codegraph/schema.go` (update schema text)
- Modify: `/path/to/repos/src/go-code/internal/codegraph/index.go` (pass relationships through)

**Step 1: Update indexParseResult and ingestAndParse**

In `/path/to/repos/src/go-code/internal/codegraph/parse.go`:

Add `rels` field to `indexParseResult`:

```go
type indexParseResult struct {
	file    *ingest.File
	symbols []*parser.Symbol
	calls   []parser.CallSite
	rels    []parser.TypeRelationship
	imports []string
}
```

Add `allRels` to `ingestAndParse` return:

```go
func ingestAndParse(ctx context.Context, root string) ([]*ingest.File, []*parser.Symbol, []parser.CallSite, []parser.TypeRelationship, map[string][]string, error) {
```

Collect `allRels` in the results loop:

```go
	var allRels []parser.TypeRelationship
	// in the loop:
	allRels = append(allRels, r.rels...)
```

In `indexParseFile`, extract relationships:

```go
	rels, _ := parser.ExtractRelationships(f.Path, source, opts)

	return indexParseResult{
		file:    f,
		symbols: pr.Symbols,
		calls:   calls,
		rels:    rels,
		imports: pr.Imports,
	}
```

**Step 2: Add relationship edges to buildGraph**

In `/path/to/repos/src/go-code/internal/codegraph/graph_build.go`:

Update `buildGraph` signature to accept relationships:

```go
func buildGraph(root string, files []*ingest.File, symbols []*parser.Symbol, cg *callgraph.CallGraph, fileImports map[string][]string, rels []parser.TypeRelationship) ([]vertexData, []edgeData) {
```

Add after CALLS edges block:

```go
	// INHERITS / IMPLEMENTS edges (Symbol→Symbol).
	relEdges := buildRelationshipEdges(root, rels, symbols)
	edges = append(edges, relEdges...)
```

Add helper function:

```go
// buildRelationshipEdges resolves type relationships against the symbol table
// and creates INHERITS or IMPLEMENTS edges.
func buildRelationshipEdges(root string, rels []parser.TypeRelationship, symbols []*parser.Symbol) []edgeData {
	// Build symbol lookup by name (may have multiple with same name in different files).
	byName := make(map[string][]*parser.Symbol)
	for _, s := range symbols {
		byName[s.Name] = append(byName[s.Name], s)
	}

	var edges []edgeData
	for _, r := range rels {
		// Resolve target to a known symbol.
		targets, ok := byName[r.Target]
		if !ok || len(targets) == 0 {
			continue // target not in parsed symbols (external type)
		}

		subjectRelFile := relPath(r.File, root)
		targetSym := closestByDir(targets, r.File)
		targetRelFile := relPath(targetSym.File, root)

		edgeLabel := "INHERITS"
		if r.Kind == parser.RelImplements {
			edgeLabel = "IMPLEMENTS"
		}

		edges = append(edges, edgeData{
			FromLabel: "Symbol",
			FromKey:   r.Subject + ":" + subjectRelFile,
			ToLabel:   "Symbol",
			ToKey:     targetSym.Name + ":" + targetRelFile,
			EdgeLabel: edgeLabel,
			Props:     map[string]string{},
		})
	}

	return edges
}

// closestByDir returns the symbol from candidates that is in the closest directory to refFile.
func closestByDir(candidates []*parser.Symbol, refFile string) *parser.Symbol {
	if len(candidates) == 1 {
		return candidates[0]
	}
	refDir := filepath.Dir(refFile)
	best := candidates[0]
	bestScore := 0
	for _, s := range candidates {
		score := commonPrefixLen(filepath.Dir(s.File), refDir)
		if score > bestScore {
			bestScore = score
			best = s
		}
	}
	return best
}

func commonPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}
```

**Step 3: Update schema.go**

In `/path/to/repos/src/go-code/internal/codegraph/schema.go`, add after the FETCHES line:

```go
	b.WriteString("  - INHERITS (Symbol->Symbol) — struct embedding (Go), class extends (Python/Java/TS)\n")
	b.WriteString("  - IMPLEMENTS (Symbol->Symbol) — interface implementation (Java/TS)\n")
```

**Step 4: Update index.go callers**

In `/path/to/repos/src/go-code/internal/codegraph/index.go`, update the `ingestAndParse` call to receive `rels`:

```go
files, symbols, calls, rels, fileImports, err := ingestAndParse(ctx, root)
```

Pass `rels` to `buildGraph`:

```go
vertices, edges := buildGraph(root, files, symbols, cg, fileImports, rels)
```

**Step 5: Run all tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/codegraph/ -v -count=1`
Expected: All PASS (compilation + existing tests)

Run: `cd /path/to/repos/src/go-code && go test ./... -count=1`
Expected: All PASS

**Step 6: Commit**

```bash
cd /path/to/repos/src/go-code
sudo -u example git add internal/codegraph/parse.go internal/codegraph/graph_build.go internal/codegraph/schema.go internal/codegraph/index.go
sudo -u example git commit -m "feat(codegraph): build INHERITS and IMPLEMENTS edges from type relationships

Extracts TypeRelationship data in parallel during indexing.
Resolves target types against symbol table with closest-directory heuristic.
Go embeds → INHERITS, Python/TS/Java extends → INHERITS, implements → IMPLEMENTS.
Updated graph schema for LLM freeform queries.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 4: Add Cypher templates for type hierarchy queries

**Context:** Add 4 new Cypher templates that leverage INHERITS/IMPLEMENTS edges: `implements`, `implementors`, `type_hierarchy`, `subtypes`. Also improve `dead_code` to exclude interface-required methods.

**Files:**
- Modify: `/path/to/repos/src/go-code/internal/codegraph/templates.go`
- Modify: `/path/to/repos/src/go-code/internal/codegraph/templates_test.go` (if exists, else create)

**Step 1: Write failing test**

Create or add to `/path/to/repos/src/go-code/internal/codegraph/templates_test.go`:

```go
package codegraph

import (
	"strings"
	"testing"
)

func TestNewTemplatesExist(t *testing.T) {
	for _, id := range []string{"implements", "implementors", "type_hierarchy", "subtypes"} {
		tmpl := GetTemplate(id)
		if tmpl == nil {
			t.Errorf("template %q not found", id)
			continue
		}
		if tmpl.ID != id {
			t.Errorf("template %q: ID mismatch %q", id, tmpl.ID)
		}
	}
}

func TestTemplateList_ContainsTypeHierarchy(t *testing.T) {
	list := TemplateList()
	for _, kw := range []string{"implements", "implementors", "type_hierarchy", "subtypes", "INHERITS", "IMPLEMENTS"} {
		if !strings.Contains(list, kw) {
			t.Errorf("TemplateList() missing %q", kw)
		}
	}
}

func TestDeadCodeTemplate_ExcludesInterfaceMethods(t *testing.T) {
	tmpl := GetTemplate("dead_code")
	if tmpl == nil {
		t.Fatal("dead_code template not found")
	}
	if !strings.Contains(tmpl.Cypher, "INHERITS") && !strings.Contains(tmpl.Cypher, "IMPLEMENTS") {
		t.Log("NOTE: dead_code template does not filter interface-required methods")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd /path/to/repos/src/go-code && go test ./internal/codegraph/ -run "TestNewTemplatesExist|TestTemplateList" -v`
Expected: FAIL — templates not found

**Step 3: Add templates**

In `/path/to/repos/src/go-code/internal/codegraph/templates.go`, add to the `templates` map:

```go
	"implements": {
		ID:          "implements",
		Description: "Find all interfaces/classes that the named type implements or inherits from",
		Params:      []string{"name"},
		Cypher:      "MATCH (s:Symbol {name: '{name}'})-[:INHERITS|IMPLEMENTS]->(target:Symbol) RETURN target",
		Cols:        1,
	},
	"implementors": {
		ID:          "implementors",
		Description: "Find all types that implement or inherit from the named interface/class",
		Params:      []string{"name"},
		Cypher:      "MATCH (child:Symbol)-[:INHERITS|IMPLEMENTS]->(parent:Symbol {name: '{name}'}) RETURN child",
		Cols:        1,
	},
	"type_hierarchy": {
		ID:          "type_hierarchy",
		Description: "Show the full type hierarchy (parents and children) for a named type",
		Params:      []string{"name"},
		Cypher:      "MATCH (s:Symbol {name: '{name}'}) OPTIONAL MATCH (s)-[:INHERITS|IMPLEMENTS]->(parent:Symbol) OPTIONAL MATCH (child:Symbol)-[:INHERITS|IMPLEMENTS]->(s) RETURN s, parent, child",
		Cols:        3,
	},
	"subtypes": {
		ID:          "subtypes",
		Description: "Find all transitive subtypes of the named type (up to 5 levels deep)",
		Params:      []string{"name"},
		Cypher:      "MATCH (child:Symbol)-[:INHERITS|IMPLEMENTS*1..5]->(ancestor:Symbol {name: '{name}'}) RETURN child, length(shortestPath((child)-[:INHERITS|IMPLEMENTS*]->(ancestor:Symbol {name: '{name}'}))) AS depth ORDER BY depth",
		Cols:        2,
	},
```

Also update the `dead_code` template to exclude methods on types that implement interfaces (they're required, not dead):

```go
	"dead_code": {
		ID:          "dead_code",
		Description: "Find functions that are never called (excludes methods required by IMPLEMENTS/INHERITS relationships)",
		Params:      []string{},
		Cypher:      "MATCH (s:Symbol) WHERE s.kind IN ['function', 'method'] AND NOT ()-[:CALLS]->(s) AND NOT EXISTS { MATCH (t:Symbol)-[:INHERITS|IMPLEMENTS]->(:Symbol) WHERE t.file = s.file } RETURN s",
		Cols:        1,
	},
```

**Step 4: Run tests to verify they pass**

Run: `cd /path/to/repos/src/go-code && go test ./internal/codegraph/ -run "TestNewTemplatesExist|TestTemplateList" -v`
Expected: PASS

Run: `cd /path/to/repos/src/go-code && go test ./internal/codegraph/ -v -count=1`
Expected: All PASS

**Step 5: Commit**

```bash
cd /path/to/repos/src/go-code
sudo -u example git add internal/codegraph/templates.go internal/codegraph/templates_test.go
sudo -u example git commit -m "feat(codegraph): add Cypher templates for type hierarchy queries

4 new templates: implements, implementors, type_hierarchy, subtypes.
Updated dead_code to exclude methods on types with INHERITS/IMPLEMENTS edges.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 5: Wire type relationships into code_compare

**Context:** When comparing two repos, type hierarchy data adds value: show which repo has richer interface usage, more inheritance depth, etc. Add relationship counts to `CompareResult` and include in LLM context.

**Files:**
- Create: `/path/to/repos/src/go-code/internal/compare/relstats.go`
- Create: `/path/to/repos/src/go-code/internal/compare/relstats_test.go`
- Modify: `/path/to/repos/src/go-code/internal/compare/compare.go` (add RelStats to CompareResult)
- Modify: `/path/to/repos/src/go-code/internal/compare/snapshot.go` (extract relationships during snapshot)
- Modify: `/path/to/repos/src/go-code/internal/compare/context.go` (include RelStats in LLM context)

**Step 1: Write the failing test**

Create `/path/to/repos/src/go-code/internal/compare/relstats_test.go`:

```go
package compare

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestComputeRelStats(t *testing.T) {
	rels := []parser.TypeRelationship{
		{Subject: "Dog", Target: "Animal", Kind: parser.RelExtends},
		{Subject: "Cat", Target: "Animal", Kind: parser.RelExtends},
		{Subject: "Dog", Target: "Runnable", Kind: parser.RelImplements},
		{Subject: "MyReader", Target: "Reader", Kind: parser.RelEmbeds},
	}

	stats := ComputeRelStats(rels)

	if stats.Total != 4 {
		t.Errorf("Total = %d, want 4", stats.Total)
	}
	if stats.Extends != 2 {
		t.Errorf("Extends = %d, want 2", stats.Extends)
	}
	if stats.Implements != 1 {
		t.Errorf("Implements = %d, want 1", stats.Implements)
	}
	if stats.Embeds != 1 {
		t.Errorf("Embeds = %d, want 1", stats.Embeds)
	}
	if stats.UniqueSubjects != 3 {
		t.Errorf("UniqueSubjects = %d, want 3", stats.UniqueSubjects)
	}
}

func TestComputeRelStats_Empty(t *testing.T) {
	stats := ComputeRelStats(nil)
	if stats != nil {
		t.Error("expected nil for empty rels")
	}
}
```

**Step 2: Implement relstats.go**

Create `/path/to/repos/src/go-code/internal/compare/relstats.go`:

```go
package compare

import (
	"github.com/anatolykoptev/go-code/internal/parser"
)

// RelStats summarizes type relationship counts for a repository.
type RelStats struct {
	Total          int `json:"total"`
	Extends        int `json:"extends"`
	Implements     int `json:"implements"`
	Embeds         int `json:"embeds"`
	UniqueSubjects int `json:"uniqueSubjects"`
}

// ComputeRelStats computes relationship statistics from extracted relationships.
// Returns nil if no relationships exist.
func ComputeRelStats(rels []parser.TypeRelationship) *RelStats {
	if len(rels) == 0 {
		return nil
	}

	stats := &RelStats{Total: len(rels)}
	subjects := make(map[string]bool)

	for _, r := range rels {
		subjects[r.Subject] = true
		switch r.Kind {
		case parser.RelExtends:
			stats.Extends++
		case parser.RelImplements:
			stats.Implements++
		case parser.RelEmbeds:
			stats.Embeds++
		}
	}

	stats.UniqueSubjects = len(subjects)
	return stats
}
```

**Step 3: Add RelStats to CompareResult and snapshot**

In `/path/to/repos/src/go-code/internal/compare/compare.go`, add to `CompareResult`:

```go
	RelStatsA     *RelStats      `json:"rel_stats_a,omitempty"`
	RelStatsB     *RelStats      `json:"rel_stats_b,omitempty"`
```

In `/path/to/repos/src/go-code/internal/compare/snapshot.go`, add `Rels` field to `RepoSnapshot`:

```go
	// Rels holds type relationships extracted from the repository.
	Rels []parser.TypeRelationship `json:"rels,omitempty"`
```

In the `BuildSnapshot` function, extract relationships for each parsed file and collect them into the snapshot. Add after symbols are collected:

```go
	// Extract type relationships (parallel to symbol extraction).
	var allRels []parser.TypeRelationship
	for _, sf := range snap.Files {
		if sf.RelPath == "" {
			continue
		}
		source, err := os.ReadFile(filepath.Join(root, sf.RelPath))
		if err != nil {
			continue
		}
		rels, _ := parser.ExtractRelationships(filepath.Join(root, sf.RelPath), source, parser.ParseOpts{Language: sf.Language})
		allRels = append(allRels, rels...)
	}
	snap.Rels = allRels
```

In `CompareRepos`, compute RelStats and assign:

```go
	relStatsA := ComputeRelStats(snapA.Rels)
	relStatsB := ComputeRelStats(snapB.Rels)
```

Add to result struct: `RelStatsA: relStatsA, RelStatsB: relStatsB,`

**Step 4: Add to LLM context**

In `/path/to/repos/src/go-code/internal/compare/context.go`, add a `writeRelStats` section in `BuildCompareContextV2` after the hotspots section:

```go
	if relStatsA != nil || relStatsB != nil {
		writeRelStats(&sb, relStatsA, relStatsB)
		if sb.Len() >= maxContextChars {
			return sb.String()
		}
	}
```

Update `BuildCompareContextV2` signature to accept RelStats (or use the matches to compute inline). **Better approach**: pass RelStats as parameters:

```go
func BuildCompareContextV3(matches []SymbolMatch, metricsA, metricsB RepoMetrics, query string, hotspotsA, hotspotsB []HotspotFile, relStatsA, relStatsB *RelStats) string {
```

Or simpler — keep V2 and add RelStats to the Metrics section by embedding in RepoMetrics context. **Simplest approach**: just write RelStats inside the existing metrics JSON by adding a helper:

```go
func writeRelStats(sb *strings.Builder, statsA, statsB *RelStats) {
	sb.WriteString("## Type Hierarchy\n\n")
	if statsA != nil {
		fmt.Fprintf(sb, "**Repo A**: %d relationships (%d extends, %d implements, %d embeds) across %d types\n",
			statsA.Total, statsA.Extends, statsA.Implements, statsA.Embeds, statsA.UniqueSubjects)
	} else {
		sb.WriteString("**Repo A**: no type relationships detected\n")
	}
	if statsB != nil {
		fmt.Fprintf(sb, "**Repo B**: %d relationships (%d extends, %d implements, %d embeds) across %d types\n",
			statsB.Total, statsB.Extends, statsB.Implements, statsB.Embeds, statsB.UniqueSubjects)
	} else {
		sb.WriteString("**Repo B**: no type relationships detected\n")
	}
	sb.WriteString("\n")
}
```

**Step 5: Run all tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/compare/ -v -count=1`
Expected: All PASS

Run: `cd /path/to/repos/src/go-code && go test ./... -count=1`
Expected: All PASS

**Step 6: Commit**

```bash
cd /path/to/repos/src/go-code
sudo -u example git add internal/compare/relstats.go internal/compare/relstats_test.go internal/compare/compare.go internal/compare/snapshot.go internal/compare/context.go
sudo -u example git commit -m "feat(compare): add type hierarchy stats to code comparison

Extract type relationships during snapshot building.
RelStats counts extends/implements/embeds per repo.
LLM context includes Type Hierarchy section for informed analysis.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

### Task 6: Lint + Full Test Pass + Deploy

**Files:** None new — validation only.

**Step 1: Run all tests**

Run: `cd /path/to/repos/src/go-code && go test ./internal/parser/ -v -count=1`
Expected: All PASS

Run: `cd /path/to/repos/src/go-code && go test ./internal/codegraph/ -v -count=1`
Expected: All PASS

Run: `cd /path/to/repos/src/go-code && go test ./internal/compare/ -v -count=1`
Expected: All PASS

Run: `cd /path/to/repos/src/go-code && go test ./... -count=1`
Expected: All PASS

**Step 2: Deploy**

```bash
cd ~/deploy/example-server
docker compose build --no-cache go-code && docker compose up -d --no-deps --force-recreate go-code
```

**Step 3: Health check**

Run: `curl -s http://127.0.0.1:8897/health`
Expected: healthy with latest commit hash

**Step 4: Smoke test**

Test 1 — Verify new graph edges with `code_graph`:
- Index a repo with known type hierarchies (e.g. a Java or Python project)
- Run `implementors` template
- Verify INHERITS/IMPLEMENTS edges appear

Test 2 — Verify `code_compare` has `rel_stats_a`/`rel_stats_b`:
- Compare two repos
- Check response JSON includes new fields

**Step 5: Push to origin**

```bash
cd /path/to/repos/src/go-code
sudo -u example git push origin main
```

---

## Summary of Changes

| Task | What | Files | New LOC (approx) |
|------|------|-------|-------------------|
| 1 | TypeRelationship struct + Go extraction | relationships.go, go_rels.scm, handler mods | +180 |
| 2 | Python/TypeScript/Java extraction | 3 query files, 3 handler mods, tests | +200 |
| 3 | Wire into codegraph (INHERITS/IMPLEMENTS edges) | parse.go, graph_build.go, schema.go, index.go | +100 |
| 4 | Cypher templates for type hierarchy | templates.go, templates_test.go | +60 |
| 5 | Wire into code_compare (RelStats) | relstats.go, compare.go, snapshot.go, context.go | +100 |
| 6 | Lint + test + deploy | - | - |
| **Total** | | **~8 new files, ~8 modified** | **~640 LOC** |

## Not in Scope (deferred to P5+)

- Semantic search via embeddings (needs vector DB)
- Identifier-level reference graph + personalized PageRank (separate feature track)
- SCIP backend for Go (3+d effort)
- Compound tools (explore, understand)
- Incremental graph indexing (file-level delta detection)
