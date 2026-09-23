package codegraph

import (
	"context"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/llm"
)

// TestClassifyAndBuildCypher_ExplicitTemplateSkipsLLM asserts a caller-chosen
// template renders Cypher with zero LLM calls — the path that keeps
// code_graph working while the free model chain is down.
func TestClassifyAndBuildCypher_ExplicitTemplateSkipsLLM(t *testing.T) {
	t.Parallel()

	cc := &countingCompleter{inner: llm.NoOp{}}
	explicit, err := ExplicitClassification("who_calls", map[string]string{"name": "ParseFile"})
	if err != nil {
		t.Fatalf("ExplicitClassification: %v", err)
	}
	cls, cypher, cols, err := classifyAndBuildCypher(context.Background(), cc, "who_calls", explicit)
	if err != nil {
		t.Fatalf("classifyAndBuildCypher: %v", err)
	}
	if n := cc.calls.Load(); n != 0 {
		t.Errorf("Complete called %d times, want 0", n)
	}
	if cls.Template != "who_calls" || cols != 1 {
		t.Errorf("template=%q cols=%d, want who_calls/1", cls.Template, cols)
	}
	if !strings.Contains(cypher, "{name: 'ParseFile'}") || !strings.Contains(cypher, "caller:Symbol)-[:CALLS]->(target") {
		t.Errorf("cypher does not look up callers of ParseFile: %s", cypher)
	}
}

func TestExplicitClassification(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		id      string
		params  map[string]string
		wantErr string
	}{
		{"ok with required param", "call_chain", map[string]string{"from": "main", "to": "Serve"}, ""},
		{"ok without params", "dead_code", nil, ""},
		{"optional path may be omitted", "api_routes", nil, ""},
		{"unknown template", "who_callz", nil, `unknown template "who_callz"`},
		{"freeform needs the LLM", "freeform", nil, "needs the LLM"},
		{"param typo is rejected, not dropped", "who_calls", map[string]string{"symbol": "Parse"}, `does not take param "symbol"`},
		{"missing identity param", "who_calls", nil, `requires param "name"`},
		{"blank identity param", "call_chain", map[string]string{"from": "main", "to": "  "}, `requires param "to"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cls, err := ExplicitClassification(tc.id, tc.params)
			if tc.wantErr == "" {
				if err != nil || cls == nil || cls.Template != tc.id {
					t.Fatalf("got (%v, %v), want template %s", cls, err, tc.id)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestExplicitClassification_CopiesParams(t *testing.T) {
	t.Parallel()

	in := map[string]string{"name": "A"}
	cls, err := ExplicitClassification("who_calls", in)
	if err != nil {
		t.Fatal(err)
	}
	in["name"] = "B"
	if cls.Params["name"] != "A" {
		t.Fatalf("classification aliases the caller's map: name=%q", cls.Params["name"])
	}
}

// TestRenderSanitizesLimit: {limit} is rendered unquoted, so escaping cannot
// stop a non-numeric value from extending the query.
func TestRenderSanitizesLimit(t *testing.T) {
	t.Parallel()

	tmpl := GetTemplate("most_connected")
	cases := map[string]string{
		"10":                         "LIMIT 10",
		"":                           "LIMIT 20",
		"ten":                        "LIMIT 20",
		"0":                          "LIMIT 20",
		"-5":                         "LIMIT 20",
		"99999":                      "LIMIT 500",
		"10 MATCH (n) RETURN n":      "LIMIT 20",
		"1 UNION MATCH (n) RETURN n": "LIMIT 20",
	}
	for in, want := range cases {
		got := tmpl.Render(map[string]string{"limit": in})
		if !strings.HasSuffix(got, want) {
			t.Errorf("limit %q rendered %q, want suffix %q", in, got, want)
		}
	}
}

func TestTemplateSignaturesListsEveryTemplate(t *testing.T) {
	t.Parallel()

	sigs := TemplateSignatures()
	for id, tmpl := range templates {
		if !strings.Contains(sigs, id+signature(tmpl)) {
			t.Errorf("TemplateSignatures missing %s%s", id, signature(tmpl))
		}
	}
}
