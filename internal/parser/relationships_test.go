package parser

import (
	"testing"
)

func TestExtractRelationships_Go(t *testing.T) {
	t.Parallel()
	source := []byte(`package main

import "io"

type Server struct {
	io.Reader
	Writer
	Name string
}

type Handler interface {
	Closer
	io.Writer
	Handle() error
}

type Plain struct {
	X int
	Y int
}
`)
	rels, err := ExtractRelationships("test.go", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractRelationships: %v", err)
	}

	t.Logf("Found %d relationships:", len(rels))
	for _, r := range rels {
		t.Logf("  %s --%s--> %s (line %d)", r.Subject, r.Kind, r.Target, r.Line)
	}

	// Expected: Server embeds Reader, Writer; Handler embeds Closer, Writer; Plain has none
	want := []struct {
		subject string
		target  string
		kind    RelKind
	}{
		{"Server", "Reader", RelEmbeds},
		{"Server", "Writer", RelEmbeds},
		{"Handler", "Closer", RelEmbeds},
		{"Handler", "Writer", RelEmbeds},
	}

	for _, w := range want {
		found := false
		for _, r := range rels {
			if r.Subject == w.subject && r.Target == w.target && r.Kind == w.kind {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing relationship: %s --%s--> %s", w.subject, w.kind, w.target)
		}
	}

	// Verify no spurious matches (like Name:string or X:int)
	for _, r := range rels {
		if r.Target == "string" || r.Target == "int" || r.Target == "error" {
			t.Errorf("unexpected relationship to primitive type: %s --%s--> %s", r.Subject, r.Kind, r.Target)
		}
	}

	// Plain struct should have no relationships
	for _, r := range rels {
		if r.Subject == "Plain" {
			t.Errorf("Plain should have no relationships, got: %s --%s--> %s", r.Subject, r.Kind, r.Target)
		}
	}
}

func TestExtractRelationships_Go_PointerEmbedding(t *testing.T) {
	t.Parallel()
	source := []byte(`package main

type Base struct{}

type Child struct {
	*Base
}
`)
	rels, err := ExtractRelationships("test.go", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractRelationships: %v", err)
	}

	if len(rels) == 0 {
		t.Fatal("expected at least 1 relationship for pointer embedding")
	}

	found := false
	for _, r := range rels {
		if r.Subject == "Child" && r.Target == "Base" && r.Kind == RelEmbeds {
			found = true
		}
	}
	if !found {
		t.Error("missing: Child --embeds--> Base")
	}
}

func TestExtractRelationships_Go_NoEmbedding(t *testing.T) {
	t.Parallel()
	source := []byte(`package main

type Config struct {
	Host string
	Port int
}
`)
	rels, err := ExtractRelationships("test.go", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractRelationships: %v", err)
	}
	if len(rels) != 0 {
		t.Errorf("expected 0 relationships for plain struct, got %d", len(rels))
		for _, r := range rels {
			t.Logf("  %s --%s--> %s", r.Subject, r.Kind, r.Target)
		}
	}
}

func TestExtractRelationships_Python(t *testing.T) {
	t.Parallel()
	source := []byte(`
class Base:
    pass

class Child(Base):
    pass

class Multi(Base, Mixin):
    pass

class Dotted(module.Base):
    pass
`)
	rels, err := ExtractRelationships("test.py", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractRelationships: %v", err)
	}

	t.Logf("Found %d relationships:", len(rels))
	for _, r := range rels {
		t.Logf("  %s --%s--> %s (line %d)", r.Subject, r.Kind, r.Target, r.Line)
	}

	want := []struct {
		subject string
		target  string
		kind    RelKind
	}{
		{"Child", "Base", RelExtends},
		{"Multi", "Base", RelExtends},
		{"Multi", "Mixin", RelExtends},
		{"Dotted", "Base", RelExtends},
	}

	for _, w := range want {
		found := false
		for _, r := range rels {
			if r.Subject == w.subject && r.Target == w.target && r.Kind == w.kind {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing relationship: %s --%s--> %s", w.subject, w.kind, w.target)
		}
	}

	// Base class itself should have no relationships
	for _, r := range rels {
		if r.Subject == "Base" {
			t.Errorf("Base should have no relationships, got: %s --%s--> %s", r.Subject, r.Kind, r.Target)
		}
	}
}

func TestExtractRelationships_TypeScript(t *testing.T) {
	t.Parallel()
	source := []byte(`
class Base {}
class Child extends Base {}
class Service extends Base implements IHandler, ILogger {}
interface IBase {}
interface IChild extends IBase {}
`)
	rels, err := ExtractRelationships("test.ts", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractRelationships: %v", err)
	}

	t.Logf("Found %d relationships:", len(rels))
	for _, r := range rels {
		t.Logf("  %s --%s--> %s (line %d)", r.Subject, r.Kind, r.Target, r.Line)
	}

	want := []struct {
		subject string
		target  string
		kind    RelKind
	}{
		{"Child", "Base", RelExtends},
		{"Service", "Base", RelExtends},
		{"Service", "IHandler", RelImplements},
		{"Service", "ILogger", RelImplements},
		{"IChild", "IBase", RelExtends},
	}

	for _, w := range want {
		found := false
		for _, r := range rels {
			if r.Subject == w.subject && r.Target == w.target && r.Kind == w.kind {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing relationship: %s --%s--> %s", w.subject, w.kind, w.target)
		}
	}
}

func TestExtractRelationships_Java(t *testing.T) {
	t.Parallel()
	source := []byte(`
class Animal {}
class Dog extends Animal implements Runnable, Serializable {}
interface Base {}
interface Child extends Base, Cloneable {}
`)
	rels, err := ExtractRelationships("test.java", source, ParseOpts{})
	if err != nil {
		t.Fatalf("ExtractRelationships: %v", err)
	}

	t.Logf("Found %d relationships:", len(rels))
	for _, r := range rels {
		t.Logf("  %s --%s--> %s (line %d)", r.Subject, r.Kind, r.Target, r.Line)
	}

	want := []struct {
		subject string
		target  string
		kind    RelKind
	}{
		{"Dog", "Animal", RelExtends},
		{"Dog", "Runnable", RelImplements},
		{"Dog", "Serializable", RelImplements},
		{"Child", "Base", RelExtends},
		{"Child", "Cloneable", RelExtends},
	}

	for _, w := range want {
		found := false
		for _, r := range rels {
			if r.Subject == w.subject && r.Target == w.target && r.Kind == w.kind {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing relationship: %s --%s--> %s", w.subject, w.kind, w.target)
		}
	}
}

func TestExtractRelationships_Unsupported(t *testing.T) {
	t.Parallel()
	rels, err := ExtractRelationships("readme.txt", []byte("hello"), ParseOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rels) != 0 {
		t.Errorf("got %d rels for unsupported file, want 0", len(rels))
	}
}
