# Phase 2.1: New Language Handlers Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add tree-sitter parsing support for Rust, Java, C/C++, and Ruby to go-code.

**Architecture:** Each language follows the same pattern: `.scm` query file + `handler_<lang>.go` + `testdata/sample.<ext>` + tests. The `smacker/go-tree-sitter` library already ships all needed grammars — no new dependencies beyond subpackage imports. All handlers implement the existing `LanguageHandler` interface and register via `init()`.

**Tech Stack:** tree-sitter via `smacker/go-tree-sitter/{rust,java,c,cpp,ruby,csharp}`, existing parser framework.

**Pattern (identical for every language):**
1. Write `queries/<lang>.scm` — tree-sitter query extracting symbols + imports
2. Write `handler_<lang>.go` — struct implementing `LanguageHandler`, `init()` registers handler
3. Write `testdata/sample.<ext>` — realistic test fixture with all symbol kinds
4. Write test in `parser_test.go` — `TestParse<Lang>File` following existing Go/Python/TS test pattern
5. Run tests, fix query issues, commit

**Capture constants (from `handler.go`):**
- `captureFunction` = `"symbol.function"`
- `captureMethod` = `"symbol.method"`
- `captureClass` = `"symbol.class"`
- `captureInterface` = `"symbol.interface"`
- `captureType` = `"symbol.type"`
- `captureConst` = `"symbol.const"`
- `captureVar` = `"symbol.var"`
- `captureImport` = `"import.path"`

**Existing handler pattern (copy from `handler_go.go`):**
```go
package parser

import (
    _ "embed"
    sitter "github.com/smacker/go-tree-sitter"
    "<grammar-package>"
)

//go:embed queries/<lang>.scm
var <lang>QueryBytes []byte

type <lang>Handler struct {
    lang  *sitter.Language
    query *sitter.Query
}

var <lang>Lang = &<lang>Handler{}

func init() {
    lang := <grammar>.GetLanguage()
    q, err := sitter.NewQuery(<lang>QueryBytes, lang)
    if err != nil {
        panic("<lang>.scm query compile error: " + err.Error())
    }
    <lang>Lang.lang = lang
    <lang>Lang.query = q
    registerHandler(<lang>Lang)
}

func (h *<lang>Handler) Language() string       { return "<lang>" }
func (h *<lang>Handler) Extensions() []string   { return []string{".<ext>"} }
func (h *<lang>Handler) SitterLanguage() *sitter.Language { return h.lang }
func (h *<lang>Handler) TagsQuery() *sitter.Query         { return h.query }

func (h *<lang>Handler) MapCapture(captureName string, node *sitter.Node, source []byte) *Symbol {
    switch captureName {
    case captureFunction:
        return h.mapFunction(node, source)
    // ... other captures
    }
    return nil
}
```

**Test pattern (copy from `TestParseGoFile`):**
```go
func TestParse<Lang>File(t *testing.T) {
    source, err := os.ReadFile(filepath.Join("testdata", "sample.<ext>"))
    // ... parse, check language, check imports, check symbols by name+kind,
    // check signatures non-empty, check StartLine/EndLine
}
```

---

### Task 1: Rust handler

**Files:**
- Create: `internal/parser/queries/rust.scm`
- Create: `internal/parser/handler_rust.go`
- Create: `internal/parser/testdata/sample.rs`
- Modify: `internal/parser/parser_test.go` — add `TestParseRustFile`

**Step 1: Write `queries/rust.scm`**

```scheme
; tree-sitter query for Rust symbol extraction.

; Function definitions: fn foo() {}
(function_item
  name: (identifier) @symbol.name) @symbol.function

; Method definitions inside impl blocks.
(impl_item
  body: (declaration_list
    (function_item
      name: (identifier) @symbol.name) @symbol.method))

; Struct definitions: struct Foo {}
(struct_item
  name: (type_identifier) @symbol.name) @symbol.struct

; Enum definitions: enum Foo {}
(enum_item
  name: (type_identifier) @symbol.name) @symbol.type

; Trait definitions: trait Foo {}
(trait_item
  name: (type_identifier) @symbol.name) @symbol.interface

; Type aliases: type Foo = Bar;
(type_item
  name: (type_identifier) @symbol.name) @symbol.type

; Const items: const MAX: i32 = 10;
(const_item
  name: (identifier) @symbol.name) @symbol.const

; Static items: static FOO: i32 = 10;
(static_item
  name: (identifier) @symbol.name) @symbol.var

; Use declarations: use std::io;
(use_declaration
  argument: (scoped_identifier) @import.path)

; Use declarations (simple): use foo;
(use_declaration
  argument: (identifier) @import.path)
```

**Step 2: Write `handler_rust.go`**

Grammar import: `github.com/smacker/go-tree-sitter/rust`
Language: `"rust"`, Extensions: `[]string{".rs"}`
MapCapture cases: `captureFunction`, `captureMethod`, struct (use `KindStruct` — check node type via `captureType` but we use `@symbol.struct` which doesn't exist in our constants, so use the generic approach).

Important: We need a `captureStruct` constant. But our query uses `@symbol.struct` which doesn't match any existing capture constant. **Solution**: use `@symbol.type` for structs/enums/type-aliases and detect struct vs type via node type check in MapCapture. Use `@symbol.interface` for traits. This matches how Go does it (all types use `@symbol.type`, then `detectTypeKind` differentiates).

**Revised query approach**: Use `@symbol.type` for struct, enum, type_item. Then in MapCapture, check node type:
- `struct_item` → KindStruct
- `trait_item` → keep as `@symbol.interface`
- `enum_item`, `type_item` → KindType

**Updated `queries/rust.scm`:**
```scheme
; tree-sitter query for Rust symbol extraction.

; Function definitions: fn foo() {}
(function_item
  name: (identifier) @symbol.name) @symbol.function

; Method definitions inside impl blocks.
(impl_item
  body: (declaration_list
    (function_item
      name: (identifier) @symbol.name) @symbol.method))

; Struct definitions: struct Foo {}
(struct_item
  name: (type_identifier) @symbol.name) @symbol.type

; Enum definitions: enum Foo {}
(enum_item
  name: (type_identifier) @symbol.name) @symbol.type

; Trait definitions: trait Foo {}
(trait_item
  name: (type_identifier) @symbol.name) @symbol.interface

; Type aliases: type Foo = Bar;
(type_item
  name: (type_identifier) @symbol.name) @symbol.type

; Const items: const MAX: i32 = 10;
(const_item
  name: (identifier) @symbol.name) @symbol.const

; Static items: static FOO: i32 = 10;
(static_item
  name: (identifier) @symbol.name) @symbol.var

; Use declarations: use std::io;
(use_declaration
  argument: (scoped_identifier) @import.path)

; Use declarations (simple): use foo;
(use_declaration
  argument: (identifier) @import.path)
```

MapCapture in handler detects struct kind:
```go
case captureType:
    return h.mapType(node, source)
```
where `mapType` checks `node.Type()`:
- `"struct_item"` → KindStruct
- otherwise → KindType

**Step 3: Write `testdata/sample.rs`**

```rust
use std::io;
use std::collections::HashMap;

const MAX_RETRIES: i32 = 3;

static DEFAULT_PORT: u16 = 8080;

struct Config {
    host: String,
    port: u16,
}

enum Status {
    Active,
    Inactive,
}

trait Handler {
    fn handle(&self) -> Result<(), io::Error>;
}

type AliasConfig = Config;

impl Config {
    fn new(host: String, port: u16) -> Self {
        Config { host, port }
    }

    fn address(&self) -> String {
        format!("{}:{}", self.host, self.port)
    }
}

fn create_config() -> Config {
    Config::new("localhost".to_string(), 8080)
}
```

**Step 4: Write `TestParseRustFile` in `parser_test.go`**

Expected symbols:
- `MAX_RETRIES` → KindConst
- `DEFAULT_PORT` → KindVar
- `Config` → KindStruct
- `Status` → KindType
- `Handler` → KindInterface
- `AliasConfig` → KindType
- `new` → KindMethod
- `address` → KindMethod
- `create_config` → KindFunction

Expected imports: `std::io`, `std::collections::HashMap`

**Step 5: Run tests**

```bash
cd /home/krolik/src/go-code && PATH=/usr/local/go/bin:$PATH go test -v ./internal/parser/ -run TestParseRustFile
```

**Step 6: Commit**

```bash
git add internal/parser/queries/rust.scm internal/parser/handler_rust.go internal/parser/testdata/sample.rs internal/parser/parser_test.go
git commit -m "feat(parser): add Rust language handler with tree-sitter"
```

---

### Task 2: Java handler

**Files:**
- Create: `internal/parser/queries/java.scm`
- Create: `internal/parser/handler_java.go`
- Create: `internal/parser/testdata/sample.java`
- Modify: `internal/parser/parser_test.go` — add `TestParseJavaFile`

**Step 1: Write `queries/java.scm`**

```scheme
; tree-sitter query for Java symbol extraction.

; Class declarations.
(class_declaration
  name: (identifier) @symbol.name) @symbol.class

; Interface declarations.
(interface_declaration
  name: (identifier) @symbol.name) @symbol.interface

; Enum declarations.
(enum_declaration
  name: (identifier) @symbol.name) @symbol.type

; Method declarations inside classes.
(class_declaration
  body: (class_body
    (method_declaration
      name: (identifier) @symbol.name) @symbol.method))

; Constructor declarations inside classes.
(class_declaration
  body: (class_body
    (constructor_declaration
      name: (identifier) @symbol.name) @symbol.method))

; Field declarations with final (constants).
(class_declaration
  body: (class_body
    (field_declaration
      (modifiers "static" "final")
      declarator: (variable_declarator
        name: (identifier) @symbol.name)) @symbol.const))

; Import declarations.
(import_declaration
  (scoped_identifier) @import.path)
```

Note: Java field constants query may need adjustment based on exact tree-sitter grammar. The subagent should test and fix.

**Step 2: Write `handler_java.go`**

Grammar import: `github.com/smacker/go-tree-sitter/java`
Language: `"java"`, Extensions: `[]string{".java"}`
MapCapture cases: `captureClass`, `captureInterface`, `captureType`, `captureMethod`, `captureConst`

**Step 3: Write `testdata/sample.java`**

```java
import java.util.List;
import java.util.HashMap;

public class Config {
    public static final int MAX_RETRIES = 3;

    private String host;
    private int port;

    public Config(String host, int port) {
        this.host = host;
        this.port = port;
    }

    public String address() {
        return host + ":" + port;
    }
}

interface Handler {
    void handle(String request);
}

enum Status {
    ACTIVE,
    INACTIVE
}
```

**Step 4: Write `TestParseJavaFile`**

Expected symbols:
- `Config` → KindClass
- `Handler` → KindInterface
- `Status` → KindType
- `Config` (constructor) → KindMethod
- `address` → KindMethod

Expected imports: `java.util.List`, `java.util.HashMap`

Note: The `static final` constant query may not work out of the box. The subagent should test the query against the tree-sitter Java grammar AST and fix as needed. If constants are too complex, skip them (Java doesn't have top-level const like Go).

**Step 5: Run tests, fix, commit**

```bash
cd /home/krolik/src/go-code && PATH=/usr/local/go/bin:$PATH go test -v ./internal/parser/ -run TestParseJavaFile
git add internal/parser/queries/java.scm internal/parser/handler_java.go internal/parser/testdata/sample.java internal/parser/parser_test.go
git commit -m "feat(parser): add Java language handler with tree-sitter"
```

---

### Task 3: C handler

**Files:**
- Create: `internal/parser/queries/c.scm`
- Create: `internal/parser/handler_c.go`
- Create: `internal/parser/testdata/sample.c`
- Modify: `internal/parser/parser_test.go` — add `TestParseCFile`

**Step 1: Write `queries/c.scm`**

```scheme
; tree-sitter query for C symbol extraction.

; Function definitions: int foo() {}
(function_definition
  declarator: (function_declarator
    declarator: (identifier) @symbol.name)) @symbol.function

; Function declarations (prototypes): int foo();
(declaration
  declarator: (function_declarator
    declarator: (identifier) @symbol.name)) @symbol.function

; Struct definitions: struct Foo {};
(struct_specifier
  name: (type_identifier) @symbol.name) @symbol.type

; Enum definitions: enum Foo {};
(enum_specifier
  name: (type_identifier) @symbol.name) @symbol.type

; Typedef: typedef struct {} Foo;
(type_definition
  declarator: (type_identifier) @symbol.name) @symbol.type

; Preprocessor #include
(preproc_include
  path: (system_lib_string) @import.path)

(preproc_include
  path: (string_literal) @import.path)
```

Note: C tree-sitter grammar node types need verification. The subagent should run tree-sitter playground or `sitter.NewParser()` debug output to validate exact node types and fix the query.

**Step 2: Write `handler_c.go`**

Grammar import: `github.com/smacker/go-tree-sitter/c`
Language: `"c"`, Extensions: `[]string{".c", ".h"}`
MapCapture cases: `captureFunction`, `captureType`

Note: C has no classes/methods/interfaces. Only functions, structs, enums, typedefs.

**Step 3: Write `testdata/sample.c`**

```c
#include <stdio.h>
#include "config.h"

#define MAX_RETRIES 3

typedef struct {
    char* host;
    int port;
} Config;

struct Server {
    Config config;
    int running;
};

enum Status {
    ACTIVE,
    INACTIVE
};

Config* create_config(const char* host, int port);

void run_server(Config* config) {
    printf("%s:%d\n", config->host, config->port);
}
```

**Step 4: Write `TestParseCFile`**

Expected symbols:
- `Config` → KindType (typedef)
- `Server` → KindType (struct)
- `Status` → KindType (enum)
- `create_config` → KindFunction (prototype)
- `run_server` → KindFunction (definition)

Expected imports: `<stdio.h>`, `"config.h"`

**Step 5: Run tests, fix, commit**

```bash
cd /home/krolik/src/go-code && PATH=/usr/local/go/bin:$PATH go test -v ./internal/parser/ -run TestParseCFile
git add internal/parser/queries/c.scm internal/parser/handler_c.go internal/parser/testdata/sample.c internal/parser/parser_test.go
git commit -m "feat(parser): add C language handler with tree-sitter"
```

---

### Task 4: C++ handler

**Files:**
- Create: `internal/parser/queries/cpp.scm`
- Create: `internal/parser/handler_cpp.go`
- Create: `internal/parser/testdata/sample.cpp`
- Modify: `internal/parser/parser_test.go` — add `TestParseCppFile`

**Step 1: Write `queries/cpp.scm`**

C++ extends C with classes, methods, namespaces, templates.

```scheme
; tree-sitter query for C++ symbol extraction.

; Function definitions.
(function_definition
  declarator: (function_declarator
    declarator: (identifier) @symbol.name)) @symbol.function

; Qualified function definitions (namespace::func or Class::method).
(function_definition
  declarator: (function_declarator
    declarator: (qualified_identifier
      name: (identifier) @symbol.name))) @symbol.method

; Class definitions: class Foo {};
(class_specifier
  name: (type_identifier) @symbol.name) @symbol.class

; Struct definitions: struct Foo {};
(struct_specifier
  name: (type_identifier) @symbol.name) @symbol.type

; Enum definitions.
(enum_specifier
  name: (type_identifier) @symbol.name) @symbol.type

; Method declarations inside class body.
(class_specifier
  body: (field_declaration_list
    (function_definition
      declarator: (function_declarator
        declarator: (field_identifier) @symbol.name)) @symbol.method))

; Method declarations (prototypes) inside class body.
(class_specifier
  body: (field_declaration_list
    (declaration
      declarator: (function_declarator
        declarator: (field_identifier) @symbol.name)) @symbol.method))

; Namespace definitions.
(namespace_definition
  name: (identifier) @symbol.name) @symbol.type

; #include
(preproc_include
  path: (system_lib_string) @import.path)

(preproc_include
  path: (string_literal) @import.path)
```

Note: C++ tree-sitter grammar is complex. The subagent must test against actual AST output and fix node types as needed.

**Step 2: Write `handler_cpp.go`**

Grammar import: `github.com/smacker/go-tree-sitter/cpp`
Language: `"cpp"`, Extensions: `[]string{".cpp", ".cc", ".cxx", ".hpp"}`
MapCapture: `captureFunction`, `captureMethod`, `captureClass`, `captureType`

Important: differentiate `struct_specifier` (KindStruct) vs others in mapType.

**Step 3: Write `testdata/sample.cpp`**

```cpp
#include <iostream>
#include <string>

const int MAX_RETRIES = 3;

struct Point {
    double x;
    double y;
};

class Config {
public:
    Config(const std::string& host, int port);
    std::string address() const;

private:
    std::string host_;
    int port_;
};

Config::Config(const std::string& host, int port)
    : host_(host), port_(port) {}

std::string Config::address() const {
    return host_ + ":" + std::to_string(port_);
}

enum Status {
    ACTIVE,
    INACTIVE
};

namespace server {
    void run(const Config& config) {
        std::cout << config.address() << std::endl;
    }
}
```

**Step 4: Write `TestParseCppFile`**

Expected symbols:
- `Point` → KindType (struct)
- `Config` → KindClass
- `Status` → KindType (enum)
- `Config` (out-of-line constructor) → KindMethod
- `address` (out-of-line method) → KindMethod
- `server` → KindType (namespace)
- `run` → KindFunction (inside namespace, but may appear as function)
- `address`, `Config` constructor (in-class declarations) → KindMethod

Expected imports: `<iostream>`, `<string>`

Note: Some symbols may appear twice (declaration + definition). The dedup by `kind:name:startLine` prevents this.

**Step 5: Run tests, fix, commit**

```bash
cd /home/krolik/src/go-code && PATH=/usr/local/go/bin:$PATH go test -v ./internal/parser/ -run TestParseCppFile
git add internal/parser/queries/cpp.scm internal/parser/handler_cpp.go internal/parser/testdata/sample.cpp internal/parser/parser_test.go
git commit -m "feat(parser): add C++ language handler with tree-sitter"
```

---

### Task 5: Ruby handler

**Files:**
- Create: `internal/parser/queries/ruby.scm`
- Create: `internal/parser/handler_ruby.go`
- Create: `internal/parser/testdata/sample.rb`
- Modify: `internal/parser/parser_test.go` — add `TestParseRubyFile`

**Step 1: Write `queries/ruby.scm`**

```scheme
; tree-sitter query for Ruby symbol extraction.

; Method definitions: def foo; end
(method
  name: (identifier) @symbol.name) @symbol.function

; Singleton method (class method): def self.foo; end
(singleton_method
  name: (identifier) @symbol.name) @symbol.function

; Class definitions: class Foo; end
(class
  name: (constant) @symbol.name) @symbol.class

; Module definitions: module Foo; end
(module
  name: (constant) @symbol.name) @symbol.type

; Constant assignment: MAX_RETRIES = 3
(assignment
  left: (constant) @symbol.name) @symbol.const

; Require statements: require 'json'
(call
  method: (identifier) @_method
  arguments: (argument_list
    (string
      (string_content) @import.path))
  (#eq? @_method "require"))

; Require relative: require_relative 'config'
(call
  method: (identifier) @_method
  arguments: (argument_list
    (string
      (string_content) @import.path))
  (#eq? @_method "require_relative"))
```

Note: Ruby tree-sitter grammar node types need verification. The `method` node inside a class may need special handling to differentiate between top-level `def` (function) vs class-level `def` (method).

**Step 2: Write `handler_ruby.go`**

Grammar import: `github.com/smacker/go-tree-sitter/ruby`
Language: `"ruby"`, Extensions: `[]string{".rb"}`
MapCapture: `captureFunction`, `captureClass`, `captureType` (module), `captureConst`

For methods inside classes: the query uses `@symbol.function` for top-level `def`. To distinguish class methods, the subagent can either:
- Accept all `def` as function (simpler, Ruby doesn't strongly differentiate)
- Use a separate query with class scope for methods

Recommended: keep it simple — all `def` as KindFunction. Class-level detection is complex in Ruby.

**Step 3: Write `testdata/sample.rb`**

```ruby
require 'json'
require_relative 'config'

MAX_RETRIES = 3

module Server
  class Config
    def initialize(host, port)
      @host = host
      @port = port
    end

    def address
      "#{@host}:#{@port}"
    end

    def self.default
      new("localhost", 8080)
    end
  end
end

def create_config
  Server::Config.new("localhost", 8080)
end
```

**Step 4: Write `TestParseRubyFile`**

Expected symbols:
- `MAX_RETRIES` → KindConst
- `Server` → KindType (module)
- `Config` → KindClass
- `initialize` → KindFunction (method, but we simplify)
- `address` → KindFunction
- `default` → KindFunction (singleton method)
- `create_config` → KindFunction

Expected imports: `json`, `config`

**Step 5: Run tests, fix, commit**

```bash
cd /home/krolik/src/go-code && PATH=/usr/local/go/bin:$PATH go test -v ./internal/parser/ -run TestParseRubyFile
git add internal/parser/queries/ruby.scm internal/parser/handler_ruby.go internal/parser/testdata/sample.rb internal/parser/parser_test.go
git commit -m "feat(parser): add Ruby language handler with tree-sitter"
```

---

### Task 6: C# handler (bonus)

**Files:**
- Create: `internal/parser/queries/csharp.scm`
- Create: `internal/parser/handler_csharp.go`
- Create: `internal/parser/testdata/sample.cs`
- Modify: `internal/parser/parser_test.go` — add `TestParseCSharpFile`

**Step 1: Write `queries/csharp.scm`**

```scheme
; tree-sitter query for C# symbol extraction.

; Class declarations.
(class_declaration
  name: (identifier) @symbol.name) @symbol.class

; Interface declarations.
(interface_declaration
  name: (identifier) @symbol.name) @symbol.interface

; Struct declarations.
(struct_declaration
  name: (identifier) @symbol.name) @symbol.type

; Enum declarations.
(enum_declaration
  name: (identifier) @symbol.name) @symbol.type

; Method declarations.
(method_declaration
  name: (identifier) @symbol.name) @symbol.method

; Constructor declarations.
(constructor_declaration
  name: (identifier) @symbol.name) @symbol.method

; Namespace declarations.
(namespace_declaration
  name: (identifier) @symbol.name) @symbol.type

; Using directives: using System;
(using_directive
  (identifier) @import.path)

(using_directive
  (qualified_name) @import.path)
```

**Step 2: Write `handler_csharp.go`**

Grammar import: `github.com/smacker/go-tree-sitter/csharp`
Language: `"csharp"`, Extensions: `[]string{".cs"}`
MapCapture: `captureClass`, `captureInterface`, `captureType`, `captureMethod`

In mapType, check node type:
- `struct_declaration` → KindStruct
- otherwise → KindType

**Step 3: Write `testdata/sample.cs`**

```csharp
using System;
using System.Collections.Generic;

namespace Server
{
    public const int MaxRetries = 3;

    public struct Point
    {
        public double X;
        public double Y;
    }

    public interface IHandler
    {
        void Handle(string request);
    }

    public class Config
    {
        private string _host;
        private int _port;

        public Config(string host, int port)
        {
            _host = host;
            _port = port;
        }

        public string Address()
        {
            return $"{_host}:{_port}";
        }
    }

    public enum Status
    {
        Active,
        Inactive
    }
}
```

**Step 4: Write `TestParseCSharpFile`**

Expected symbols:
- `Server` → KindType (namespace)
- `Point` → KindStruct (via mapType with struct_declaration check)
- `IHandler` → KindInterface
- `Config` → KindClass
- `Config` (constructor) → KindMethod
- `Address` → KindMethod
- `Status` → KindType

Expected imports: `System`, `System.Collections.Generic`

**Step 5: Run tests, fix, commit**

```bash
cd /home/krolik/src/go-code && PATH=/usr/local/go/bin:$PATH go test -v ./internal/parser/ -run TestParseCSharpFile
git add internal/parser/queries/csharp.scm internal/parser/handler_csharp.go internal/parser/testdata/sample.cs internal/parser/parser_test.go
git commit -m "feat(parser): add C# language handler with tree-sitter"
```

---

### Task 7: Final integration — build, lint, deploy

**Step 1: Run all parser tests**

```bash
cd /home/krolik/src/go-code && PATH=/usr/local/go/bin:$PATH go test -v -race ./internal/parser/
```

All tests must pass including existing Go/Python/TS tests.

**Step 2: Run full test suite**

```bash
cd /home/krolik/src/go-code && PATH=/usr/local/go/bin:$PATH go test -race ./...
```

**Step 3: Lint**

```bash
cd /home/krolik/src/go-code && PATH=/usr/local/go/bin:$PATH make lint
```

Zero issues.

**Step 4: Build binary**

```bash
cd /home/krolik/src/go-code && PATH=/usr/local/go/bin:$PATH go build ./cmd/go-code/
```

**Step 5: Docker build and deploy**

```bash
cd /home/krolik/deploy/krolik-server
docker compose build --no-cache go-code
docker compose up -d --no-deps --force-recreate go-code
sleep 3
curl -s http://127.0.0.1:8897/health
```

**Step 6: Smoke test with MCP**

Test `file_parse` on each new language's testdata file via curl or MCP tool.

**Step 7: Commit any remaining fixes, update ROADMAP.md**

Mark Phase 2.1 items as complete in `docs/ROADMAP.md`.

---

## Subagent Distribution

Tasks 1-6 are **fully independent** — each creates new files only and appends a test function. They can run in parallel.

**Recommended grouping (3 parallel subagents):**

| Subagent | Tasks | Languages |
|----------|-------|-----------|
| A | 1, 2 | Rust, Java |
| B | 3, 4 | C, C++ |
| C | 5, 6 | Ruby, C# |

After A+B+C complete → Task 7 (integration) runs as final step.

**Critical note for subagents:** Tree-sitter `.scm` query syntax is finicky. The queries in this plan are best-effort based on grammar documentation. Subagents MUST:
1. Write the query
2. Run the test
3. If query fails to compile or returns wrong results, debug by examining the tree-sitter AST output
4. Fix and re-run until tests pass

Debug command to inspect AST for a file:
```go
// Temporary debug snippet — print raw AST
p := sitter.NewParser()
p.SetLanguage(<grammar>.GetLanguage())
tree, _ := p.ParseCtx(context.Background(), nil, source)
fmt.Println(tree.RootNode().String())
```
