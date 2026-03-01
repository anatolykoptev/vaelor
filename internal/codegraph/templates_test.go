package codegraph

import (
	"strings"
	"testing"
)

func TestTemplateRender(t *testing.T) {
	cases := []struct {
		id     string
		params map[string]string
		want   string // substring expected in output
	}{
		{
			id:     "who_calls",
			params: map[string]string{"name": "MyFunc"},
			want:   "MyFunc",
		},
		{
			id:     "calls_of",
			params: map[string]string{"name": "Handler"},
			want:   "Handler",
		},
		{
			id:     "imports_of",
			params: map[string]string{"path": "internal/foo"},
			want:   "internal/foo",
		},
		{
			id:     "importers_of",
			params: map[string]string{"name": "fmt"},
			want:   "fmt",
		},
		{
			id:     "symbols_in",
			params: map[string]string{"path": "cmd/main.go"},
			want:   "cmd/main.go",
		},
		{
			id:     "call_chain",
			params: map[string]string{"from": "A", "to": "B"},
			want:   "CALLS*1..10",
		},
		{
			id:     "most_connected",
			params: map[string]string{"limit": "10"},
			want:   "10",
		},
		{
			id:     "dead_code",
			params: map[string]string{},
			want:   "function",
		},
		{
			id:     "depends_on",
			params: map[string]string{"pkg": "internal/store"},
			want:   "internal/store",
		},
		{
			id:     "dependents_of",
			params: map[string]string{"name": "database/sql"},
			want:   "database/sql",
		},
		{
			id:     "api_routes",
			params: map[string]string{"path": "/api/users"},
			want:   "/api/users",
		},
		{
			id:     "cross_calls",
			params: map[string]string{"path": "/api"},
			want:   "/api",
		},
		{
			id:     "layer_deps",
			params: map[string]string{},
			want:   "BELONGS_TO",
		},
		{
			id:     "polyglot_overview",
			params: map[string]string{},
			want:   "Layer",
		},
		{
			id:     "complex_symbols",
			params: map[string]string{"limit": "10"},
			want:   "10",
		},
		{
			id:     "hotspots",
			params: map[string]string{"limit": "5"},
			want:   "5",
		},
		{
			id:     "inherits",
			params: map[string]string{"name": "MyReader"},
			want:   "MyReader",
		},
		{
			id:     "implementations",
			params: map[string]string{"name": "Reader"},
			want:   "Reader",
		},
		{
			id:     "type_hierarchy",
			params: map[string]string{"name": "Animal"},
			want:   "Animal",
		},
		{
			id:     "subtypes",
			params: map[string]string{"name": "BaseModel"},
			want:   "BaseModel",
		},
		{
			id:     "important_symbols",
			params: map[string]string{"limit": "10"},
			want:   "10",
		},
	}

	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			tmpl := GetTemplate(tc.id)
			if tmpl == nil {
				t.Fatalf("template %q not found", tc.id)
			}
			got := tmpl.Render(tc.params)
			if !strings.Contains(got, tc.want) {
				t.Errorf("Render(%v) = %q, want substring %q", tc.params, got, tc.want)
			}
			// Verify no unsubstituted {param} placeholders remain for provided params.
			for k := range tc.params {
				if strings.Contains(got, "{"+k+"}") {
					t.Errorf("Render left unsubstituted placeholder {%s} in %q", k, got)
				}
			}
		})
	}
}

func TestTemplateRenderEscaping(t *testing.T) {
	tmpl := GetTemplate("who_calls")
	if tmpl == nil {
		t.Fatal("who_calls template not found")
	}
	got := tmpl.Render(map[string]string{"name": "it's a 'trap'"})
	if strings.Contains(got, "it's") {
		t.Errorf("Render did not escape single quotes: %q", got)
	}
}

func TestTemplateCount(t *testing.T) {
	const want = 21
	got := len(templates)
	if got != want {
		t.Errorf("expected %d templates, got %d", want, got)
	}
}

func TestAllTemplatesHaveColumns(t *testing.T) {
	for id, tmpl := range templates {
		if tmpl.Cols < 1 {
			t.Errorf("template %q has Cols=%d, want >= 1", id, tmpl.Cols)
		}
	}
}

func TestGetTemplate(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		tmpl := GetTemplate("dead_code")
		if tmpl == nil {
			t.Fatal("expected non-nil template for 'dead_code'")
		}
		if tmpl.ID != "dead_code" {
			t.Errorf("got ID=%q, want 'dead_code'", tmpl.ID)
		}
	})

	t.Run("not_found", func(t *testing.T) {
		tmpl := GetTemplate("nonexistent_template")
		if tmpl != nil {
			t.Errorf("expected nil for unknown template, got %+v", tmpl)
		}
	})
}

func TestTemplateList(t *testing.T) {
	list := TemplateList()
	if list == "" {
		t.Fatal("TemplateList() returned empty string")
	}
	// Each template ID should appear in the list.
	for id := range templates {
		if !strings.Contains(list, id) {
			t.Errorf("TemplateList() missing template ID %q", id)
		}
	}
}
