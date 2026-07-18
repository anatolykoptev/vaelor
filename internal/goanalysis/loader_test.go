package goanalysis_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/goanalysis"
)

// makeTestModule creates a temp dir with a valid go.mod and main.go.
func makeTestModule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	gomod := `module example.com/testmod

go 1.21
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	maingo := `package main

// Greeter defines a greeting interface.
type Greeter interface {
	Greet(name string) string
}

// SimpleGreeter implements Greeter.
type SimpleGreeter struct {
	Prefix string
}

// Greet returns a greeting string.
func (g *SimpleGreeter) Greet(name string) string {
	return g.Prefix + name
}

func main() {}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(maingo), 0o600); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	return dir
}

func TestLoadPackages_ValidRepo(t *testing.T) {
	dir := makeTestModule(t)

	result, err := goanalysis.LoadPackages(context.Background(), dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(result.Packages) == 0 {
		t.Fatal("expected at least one package")
	}

	pkg := result.Packages[0]
	if pkg.TypesInfo == nil {
		t.Fatal("expected TypesInfo to be populated")
	}
	if len(pkg.TypesInfo.Defs) == 0 {
		t.Error("expected Defs to be non-empty")
	}
}

func TestLoadPackages_NoGoMod(t *testing.T) {
	dir := t.TempDir()

	_, err := goanalysis.LoadPackages(context.Background(), dir, goanalysis.LoadOpts{})
	if err == nil {
		t.Fatal("expected error for missing go.mod")
	}
}

func TestLoadPackages_Timeout(t *testing.T) {
	dir := makeTestModule(t)

	_, err := goanalysis.LoadPackages(context.Background(), dir, goanalysis.LoadOpts{
		Timeout: time.Nanosecond,
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestLoadPackages_Focus(t *testing.T) {
	dir := makeTestModule(t)

	result, err := goanalysis.LoadPackages(context.Background(), dir, goanalysis.LoadOpts{
		Patterns: []string{"./..."},
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(result.Packages) == 0 {
		t.Fatal("expected at least one package with explicit pattern")
	}
	if result.Packages[0].TypesInfo == nil {
		t.Fatal("expected TypesInfo to be populated with explicit pattern")
	}
}
