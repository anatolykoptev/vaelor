package codegraph

import (
	"strings"
	"testing"
)

func TestGraphSchemaTextContainsAllVertexLabels(t *testing.T) {
	t.Parallel()

	schema := GraphSchemaText()
	vertexLabels := []string{"Package", "File", "Symbol", "Layer", "Route"}

	for _, label := range vertexLabels {
		if !strings.Contains(schema, label) {
			t.Errorf("GraphSchemaText() missing vertex label %q", label)
		}
	}
}

func TestGraphSchemaTextContainsAllEdgeLabels(t *testing.T) {
	t.Parallel()

	schema := GraphSchemaText()
	edgeLabels := []string{"CONTAINS", "CALLS", "IMPORTS", "BELONGS_TO", "HANDLES", "FETCHES"}

	for _, label := range edgeLabels {
		if !strings.Contains(schema, label) {
			t.Errorf("GraphSchemaText() missing edge label %q", label)
		}
	}
}

func TestGraphSchemaTextContainsKeyProperties(t *testing.T) {
	t.Parallel()

	schema := GraphSchemaText()
	properties := []string{
		// Package properties
		"name", "path", "repo",
		// File properties
		"language", "lines",
		// Symbol properties
		"kind", "signature", "start_line", "end_line", "complexity",
		// Layer properties
		"role", "root_dir",
		// Route properties
		"method", "framework",
		// Edge properties
		"alias", "line",
	}

	for _, prop := range properties {
		if !strings.Contains(schema, prop) {
			t.Errorf("GraphSchemaText() missing property %q", prop)
		}
	}
}

func TestGraphSchemaTextContainsKindValues(t *testing.T) {
	t.Parallel()

	schema := GraphSchemaText()
	kinds := []string{"function", "method", "type", "struct", "interface", "class", "const", "var", "module"}

	for _, kind := range kinds {
		if !strings.Contains(schema, kind) {
			t.Errorf("GraphSchemaText() missing kind value %q", kind)
		}
	}
}
