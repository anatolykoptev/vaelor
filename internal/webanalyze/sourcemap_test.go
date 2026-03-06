package webanalyze

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSourceMap(t *testing.T) {
	raw := `{
		"version": 3,
		"sources": ["src/App.tsx", "src/utils/helper.ts"],
		"sourcesContent": ["import React from 'react';\n", "export function helper() {}\n"]
	}`
	sm, err := ParseSourceMap([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(sm.Sources) != 2 {
		t.Errorf("expected 2 sources, got %d", len(sm.Sources))
	}
	if sm.Sources[0] != "src/App.tsx" {
		t.Errorf("expected src/App.tsx, got %s", sm.Sources[0])
	}
}

func TestWriteSourceTree(t *testing.T) {
	dir := t.TempDir()
	sm := &SourceMap{
		Sources:        []string{"src/App.tsx", "src/utils/helper.ts"},
		SourcesContent: []string{"import React from 'react';\n", "export function helper() {}\n"},
	}
	stats, err := WriteSourceTree(dir, sm)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Files != 2 {
		t.Errorf("expected 2 files, got %d", stats.Files)
	}
	// Verify files exist.
	data, err := os.ReadFile(filepath.Join(dir, "src", "App.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "import React from 'react';\n" {
		t.Errorf("unexpected content: %s", data)
	}
}

func TestParseSourceMap_Empty(t *testing.T) {
	raw := `{"version": 3, "sources": [], "sourcesContent": []}`
	sm, err := ParseSourceMap([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(sm.Sources) != 0 {
		t.Errorf("expected 0 sources, got %d", len(sm.Sources))
	}
}

func TestWriteSourceTree_Mismatch(t *testing.T) {
	dir := t.TempDir()
	sm := &SourceMap{
		Sources:        []string{"a.js", "b.js"},
		SourcesContent: []string{"content"},
	}
	stats, err := WriteSourceTree(dir, sm)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Files != 1 {
		t.Errorf("expected 1 file (skipped mismatched), got %d", stats.Files)
	}
}

func TestFindSourceMapURL(t *testing.T) {
	tests := []struct {
		body string
		want string
	}{
		{"var x=1;\n//# sourceMappingURL=app.js.map", "app.js.map"},
		{"var x=1;\n//@ sourceMappingURL=old.js.map", "old.js.map"},
		{"var x=1;", ""},
		{"//# sourceMappingURL=data:application/json;base64,abc", ""},
	}
	for _, tt := range tests {
		got := FindSourceMapURL(tt.body)
		if got != tt.want {
			t.Errorf("FindSourceMapURL(%q...) = %q, want %q", tt.body[:min(20, len(tt.body))], got, tt.want)
		}
	}
}
