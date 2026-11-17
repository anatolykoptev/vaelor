# WONT_DO — Out-of-Scope Decisions

## CLI Tool

**Decision:** We will not implement a CLI for gogenfilter.

**Rationale:**

- gogenfilter is a **library**, not a tool. The root package is the API surface.
- A CLI would add maintenance surface (argument parsing, output formats, exit codes, shell completion, OS-specific packaging) with no meaningful gain over a 10-line consumer program.
- Directory traversal, JSON output, and `--invert` semantics are all one-liners for callers. A CLI would be a thin, opinionated wrapper that we would then be unable to change without breaking scripts.
- The pre-commit hook use case is better served by a dedicated, minimal wrapper repo if demand emerges.

**What users should do instead:**

```go
// 10-line "CLI" for your exact needs
package main

import (
    "fmt"
    "os"
    "path/filepath"

    "github.com/LarsArtmann/gogenfilter/v3"
)

func main() {
    f, _ := gogenfilter.NewFilter(gogenfilter.WithFilterOptions(gogenfilter.FilterAll))
    filepath.WalkDir(os.Args[1], func(path string, d os.DirEntry, err error) error {
        if err != nil || d.IsDir() || filepath.Ext(path) != ".go" {
            return nil
        }
        filtered, _ := f.Filter(path)
        fmt.Printf("%v %s\n", filtered, path)
        return nil
    })
}
```

If a community CLI wrapper is needed, it should live in a separate module (e.g., `github.com/LarsArtmann/gogenfilter/cmd/gogenfilter`) so the core library remains dependency-free.
