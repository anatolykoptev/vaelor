package goanalysis_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/go-code/internal/goanalysis"
)

// loadSats loads a temp module from src and returns its computed satisfactions.
func loadSats(t *testing.T, src string) []goanalysis.Satisfaction {
	t.Helper()
	dir := t.TempDir()
	gomod := "module example.com/sat\n\ngo 1.21\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := goanalysis.LoadPackages(context.Background(), dir, goanalysis.LoadOpts{})
	if err != nil {
		t.Fatalf("LoadPackages: %v", err)
	}
	return goanalysis.ComputeSatisfactions(result.Packages)
}

// hasSat reports whether sats contains a (concreteType, interface) pair.
func hasSat(sats []goanalysis.Satisfaction, typ, iface string) bool {
	for _, s := range sats {
		if s.Type == typ && s.Interface == iface {
			return true
		}
	}
	return false
}

// TestComputeSatisfactions_TwoImplementers asserts that two distinct concrete
// types implementing the same interface each produce a satisfaction edge.
func TestComputeSatisfactions_TwoImplementers(t *testing.T) {
	src := `package main

type I interface{ M() }

type A struct{}
func (A) M() {}

type B struct{}
func (B) M() {}

func main() {}
`
	sats := loadSats(t, src)
	if !hasSat(sats, "A", "I") {
		t.Errorf("expected A IMPLEMENTS I; got %+v", sats)
	}
	if !hasSat(sats, "B", "I") {
		t.Errorf("expected B IMPLEMENTS I; got %+v", sats)
	}
}

// TestComputeSatisfactions_NonImplementer asserts a type that satisfies nothing
// produces no edge, and the interface it does NOT satisfy is not falsely paired.
func TestComputeSatisfactions_NonImplementer(t *testing.T) {
	src := `package main

type I interface{ M() }

type A struct{}
func (A) M() {}

// C has a different method set and does not satisfy I.
type C struct{}
func (C) Other() {}

func main() {}
`
	sats := loadSats(t, src)
	if !hasSat(sats, "A", "I") {
		t.Errorf("expected A IMPLEMENTS I; got %+v", sats)
	}
	if hasSat(sats, "C", "I") {
		t.Errorf("did not expect C IMPLEMENTS I; got %+v", sats)
	}
}

// TestComputeSatisfactions_PointerReceiver asserts satisfaction is detected when
// the methods are declared on the pointer receiver (the common mutating idiom):
// types.Implements(*T, I) must be checked, not just types.Implements(T, I).
func TestComputeSatisfactions_PointerReceiver(t *testing.T) {
	src := `package main

type Writer interface{ Write(p []byte) (int, error) }

type Buf struct{ data []byte }
func (b *Buf) Write(p []byte) (int, error) { b.data = append(b.data, p...); return len(p), nil }

func main() {}
`
	sats := loadSats(t, src)
	if !hasSat(sats, "Buf", "Writer") {
		t.Errorf("expected Buf IMPLEMENTS Writer (pointer receiver); got %+v", sats)
	}
}

// TestComputeSatisfactions_EmptyInterfaceSkipped asserts the empty interface is
// never emitted: every type trivially satisfies it, so those edges would be pure
// noise.
func TestComputeSatisfactions_EmptyInterfaceSkipped(t *testing.T) {
	src := `package main

type Any interface{}

type T struct{}

func main() {}
`
	sats := loadSats(t, src)
	for _, s := range sats {
		if s.Interface == "Any" {
			t.Errorf("empty interface Any should be skipped; got pair %+v", s)
		}
	}
}

// TestComputeSatisfactions_FileResolved asserts the absolute declaration file is
// populated on each satisfaction so the codegraph keying can map it to the
// concrete type's Symbol vertex.
func TestComputeSatisfactions_FileResolved(t *testing.T) {
	src := `package main

type I interface{ M() }

type A struct{}
func (A) M() {}

func main() {}
`
	sats := loadSats(t, src)
	found := false
	for _, s := range sats {
		if s.Type == "A" && s.Interface == "I" {
			found = true
			if s.TypeFile == "" {
				t.Error("expected non-empty TypeFile for A")
			}
		}
	}
	if !found {
		t.Fatalf("A IMPLEMENTS I not found; got %+v", sats)
	}
}
