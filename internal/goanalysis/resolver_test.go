package goanalysis_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
)

func loadTestPkgs(t *testing.T, dir string) []*goanalysis.TypedEdge {
	t.Helper()
	result, err := goanalysis.LoadPackages(context.Background(), dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	edges := goanalysis.Resolve(result.Packages)
	return edgePtrs(edges)
}

func edgePtrs(edges []goanalysis.TypedEdge) []*goanalysis.TypedEdge {
	out := make([]*goanalysis.TypedEdge, len(edges))
	for i := range edges {
		out[i] = &edges[i]
	}
	return out
}

func hasEdge(edges []*goanalysis.TypedEdge, caller, callee string) bool {
	return slices.ContainsFunc(edges, func(e *goanalysis.TypedEdge) bool {
		return e.CallerName == caller && e.CalleeName == callee
	})
}

func hasInterfaceEdge(edges []*goanalysis.TypedEdge, caller, callee, recv string) bool {
	return slices.ContainsFunc(edges, func(e *goanalysis.TypedEdge) bool {
		return e.CallerName == caller && e.CalleeName == callee &&
			e.ReceiverType == recv && e.IsInterface
	})
}

func hasMethodEdge(edges []*goanalysis.TypedEdge, caller, callee, recv string) bool {
	return slices.ContainsFunc(edges, func(e *goanalysis.TypedEdge) bool {
		return e.CallerName == caller && e.CalleeName == callee && e.ReceiverType == recv
	})
}

func TestResolve_DirectCalls(t *testing.T) {
	dir := t.TempDir()

	gomod := "module example.com/direct\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}

	src := `package main

func greet(name string) string {
	return "Hello, " + name
}

func main() {
	_ = greet("world")
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	edges := loadTestPkgs(t, dir)
	if len(edges) == 0 {
		t.Fatal("expected at least one edge")
	}
	if !hasEdge(edges, "main", "greet") {
		t.Errorf("expected main -> greet edge; got edges: %v", summarize(edges))
	}
}

func TestResolve_InterfaceCalls(t *testing.T) {
	dir := t.TempDir()

	gomod := "module example.com/iface\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}

	src := `package main

type Greeter interface {
	Greet(name string) string
}

type EnglishGreeter struct{}

func (e EnglishGreeter) Greet(name string) string {
	return "Hello, " + name
}

type SpanishGreeter struct{}

func (s SpanishGreeter) Greet(name string) string {
	return "Hola, " + name
}

func run(g Greeter) string {
	return g.Greet("world")
}

func main() {
	_ = run(EnglishGreeter{})
	_ = run(SpanishGreeter{})
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	edges := loadTestPkgs(t, dir)
	if !hasInterfaceEdge(edges, "run", "Greet", "EnglishGreeter") {
		t.Errorf("expected run -> Greet (EnglishGreeter, interface); got: %v", summarize(edges))
	}
	if !hasInterfaceEdge(edges, "run", "Greet", "SpanishGreeter") {
		t.Errorf("expected run -> Greet (SpanishGreeter, interface); got: %v", summarize(edges))
	}
}

func TestResolve_MethodCalls(t *testing.T) {
	dir := t.TempDir()

	gomod := "module example.com/method\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}

	src := `package main

type Counter struct {
	n int
}

func (c *Counter) Inc() {
	c.n++
}

func main() {
	c := &Counter{}
	c.Inc()
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}

	edges := loadTestPkgs(t, dir)
	if !hasMethodEdge(edges, "main", "Inc", "Counter") {
		t.Errorf("expected main -> Inc (Counter); got: %v", summarize(edges))
	}
}

func summarize(edges []*goanalysis.TypedEdge) []string {
	out := make([]string, 0, len(edges))
	for _, e := range edges {
		s := e.CallerName + " -> " + e.CalleeName
		if e.ReceiverType != "" {
			s += " (" + e.ReceiverType + ")"
		}
		if e.IsInterface {
			s += " [iface]"
		}
		out = append(out, s)
	}
	return out
}
