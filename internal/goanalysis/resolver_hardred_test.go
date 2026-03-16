package goanalysis_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/goanalysis"
)

// Hard red tests — edge cases for type-aware resolution.

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolve_EmptyPackages(t *testing.T) {
	edges := goanalysis.Resolve(nil)
	if len(edges) != 0 {
		t.Errorf("expected 0 edges for nil packages, got %d", len(edges))
	}
}

func TestResolve_ClosureCalls(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/closure\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), `package main

func main() {
	fn := func(x int) int { return x + 1 }
	_ = fn(42)
}
`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lr, err := goanalysis.LoadPackages(ctx, dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	edges := goanalysis.Resolve(lr.Packages)
	_ = edges // verify no panic
}

func TestResolve_EmbeddedInterface(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/embed\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), `package main

import "fmt"

type Reader interface { Read() string }
type Writer interface { Write(s string) }
type ReadWriter interface {
	Reader
	Writer
}

type File struct{ name string }
func (f File) Read() string { return f.name }
func (f File) Write(s string) { f.name = s }

func process(rw ReadWriter) { fmt.Println(rw.Read()) }

func main() { process(File{name: "test"}) }
`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lr, err := goanalysis.LoadPackages(ctx, dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	edges := goanalysis.Resolve(lr.Packages)

	found := false
	for _, e := range edges {
		if e.CallerName == "process" && e.CalleeName == "Read" && e.IsInterface {
			found = true
			if e.ReceiverType != "File" {
				t.Errorf("expected receiver File, got %q", e.ReceiverType)
			}
			break
		}
	}
	if !found {
		t.Error("expected interface dispatch process -> Read resolved to File")
	}
}

func TestResolve_PointerReceiver(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/ptr\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), `package main

type Counter struct{ n int }
func (c *Counter) Inc() { c.n++ }

func main() {
	c := &Counter{}
	c.Inc()
}
`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lr, err := goanalysis.LoadPackages(ctx, dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	edges := goanalysis.Resolve(lr.Packages)

	found := false
	for _, e := range edges {
		if e.CallerName == "main" && e.CalleeName == "Inc" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected main -> Inc edge for pointer receiver call")
	}
}

func TestResolve_MultipleInterfacesSameMethod(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/multi\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), `package main

import "fmt"

type Stringer interface { String() string }
type Namer interface { String() string }

type Person struct{ Name string }
func (p Person) String() string { return p.Name }

func printStr(s Stringer) { fmt.Println(s.String()) }
func printName(n Namer) { fmt.Println(n.String()) }

func main() {
	p := Person{Name: "Alice"}
	printStr(p)
	printName(p)
}
`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lr, err := goanalysis.LoadPackages(ctx, dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	edges := goanalysis.Resolve(lr.Packages)

	strCount := 0
	nameCount := 0
	for _, e := range edges {
		if e.CallerName == "printStr" && e.CalleeName == "String" && e.IsInterface {
			strCount++
		}
		if e.CallerName == "printName" && e.CalleeName == "String" && e.IsInterface {
			nameCount++
		}
	}
	if strCount == 0 {
		t.Error("expected interface dispatch from printStr")
	}
	if nameCount == 0 {
		t.Error("expected interface dispatch from printName")
	}
}

func TestResolve_NoMethodsNoPanic(t *testing.T) {
	dir := t.TempDir()
	writeTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/nomethod\n\ngo 1.22\n")
	writeTestFile(t, filepath.Join(dir, "main.go"), `package main

func add(a, b int) int { return a + b }
func main() { _ = add(1, 2) }
`)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	lr, err := goanalysis.LoadPackages(ctx, dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	edges := goanalysis.Resolve(lr.Packages)

	found := false
	for _, e := range edges {
		if e.CallerName == "main" && e.CalleeName == "add" {
			found = true
			if e.IsInterface {
				t.Error("add() is not an interface dispatch")
			}
			break
		}
	}
	if !found {
		t.Error("expected main -> add edge")
	}
}
