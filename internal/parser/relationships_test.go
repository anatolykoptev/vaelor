package parser

import (
	"testing"
)

func TestExtractRelationships_Go(t *testing.T) {
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

func TestExtractRelationships_Unsupported(t *testing.T) {
	rels, err := ExtractRelationships("readme.txt", []byte("hello"), ParseOpts{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rels) != 0 {
		t.Errorf("got %d rels for unsupported file, want 0", len(rels))
	}
}
