package codegraph

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       string
		wantTmpl    string
		wantParams  map[string]string
		wantErr     bool
	}{
		{
			name:     "valid known template with params",
			input:    `{"template": "who_calls", "params": {"name": "HandleRequest"}}`,
			wantTmpl: "who_calls",
			wantParams: map[string]string{"name": "HandleRequest"},
		},
		{
			name:     "freeform template passthrough",
			input:    `{"template": "freeform", "params": {}}`,
			wantTmpl: "freeform",
			wantParams: map[string]string{},
		},
		{
			name: "markdown-wrapped JSON extracted correctly",
			input: "```json\n{\"template\": \"calls_of\", \"params\": {\"name\": \"Serve\"}}\n```",
			wantTmpl: "calls_of",
			wantParams: map[string]string{"name": "Serve"},
		},
		{
			name:    "garbage text returns error",
			input:   "I cannot determine the template for this query.",
			wantErr: true,
		},
		{
			name:    "empty string returns error",
			input:   "",
			wantErr: true,
		},
		{
			name:     "unknown template falls back to freeform",
			input:    `{"template": "nonexistent_template", "params": {"x": "y"}}`,
			wantTmpl: "freeform",
			wantParams: map[string]string{"x": "y"},
		},
		{
			name:     "nil params becomes empty map",
			input:    `{"template": "dead_code"}`,
			wantTmpl: "dead_code",
			wantParams: map[string]string{},
		},
		{
			name:     "markdown fence without language tag",
			input:    "```\n{\"template\": \"imports_of\", \"params\": {\"path\": \"internal/llm\"}}\n```",
			wantTmpl: "imports_of",
			wantParams: map[string]string{"path": "internal/llm"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := parseClassification(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (result: %+v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Template != tc.wantTmpl {
				t.Errorf("template: got %q, want %q", got.Template, tc.wantTmpl)
			}
			if len(got.Params) != len(tc.wantParams) {
				t.Errorf("params length: got %d, want %d (params=%v)", len(got.Params), len(tc.wantParams), got.Params)
			}
			for k, v := range tc.wantParams {
				if got.Params[k] != v {
					t.Errorf("param[%q]: got %q, want %q", k, got.Params[k], v)
				}
			}
		})
	}
}

func TestClassify_SchemaAwarePrompt(t *testing.T) {
	t.Parallel()

	prompt := classifierSystemPrompt()
	if !strings.Contains(prompt, "INHERITS") {
		t.Error("classifier prompt should contain INHERITS edge from schema")
	}
	if !strings.Contains(prompt, "IMPLEMENTS") {
		t.Error("classifier prompt should contain IMPLEMENTS edge from schema")
	}
	if !strings.Contains(prompt, "Symbol") {
		t.Error("classifier prompt should contain Symbol vertex from schema")
	}
}

func TestClassificationJSON(t *testing.T) {
	t.Parallel()

	original := &Classification{
		Template: "call_chain",
		Params:   map[string]string{"from": "main", "to": "handleRequest"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Classification
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Template != original.Template {
		t.Errorf("template: got %q, want %q", decoded.Template, original.Template)
	}
	for k, v := range original.Params {
		if decoded.Params[k] != v {
			t.Errorf("param[%q]: got %q, want %q", k, decoded.Params[k], v)
		}
	}
}
