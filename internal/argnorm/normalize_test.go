package argnorm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNormalizeArgs_StripsUnknownAndKeepsKnown(t *testing.T) {
	accepted := map[string]struct{}{"repo": {}, "pattern": {}, "max_results": {}}
	raw := map[string]any{
		"repo":         "owner/repo",
		"pattern":      "func main",
		"max_results":  float64(10),
		"include_body": true, // unknown
		"compact":      true, // unknown
	}
	res := NormalizeArgs("code_search", raw, accepted)
	if _, ok := res.Args["include_body"]; ok {
		t.Errorf("include_body should be stripped, still present")
	}
	if _, ok := res.Args["compact"]; ok {
		t.Errorf("compact should be stripped, still present")
	}
	if res.Args["repo"] != "owner/repo" {
		t.Errorf("known prop repo mutated: %v", res.Args["repo"])
	}
	if len(res.Stripped) != 2 {
		t.Fatalf("expected 2 stripped, got %d (%v)", len(res.Stripped), res.Stripped)
	}
	// Aliased should be empty (no alias applied).
	if len(res.Aliased) != 0 {
		t.Errorf("expected no aliases, got %v", res.Aliased)
	}
	note := res.Note(accepted)
	if !strings.Contains(note, "ignored unknown params") {
		t.Errorf("note missing prefix: %q", note)
	}
	if !strings.Contains(note, `"include_body"`) || !strings.Contains(note, `"compact"`) {
		t.Errorf("note missing stripped names: %q", note)
	}
	if !strings.Contains(note, "supported:") {
		t.Errorf("note missing supported list: %q", note)
	}
}

func TestNormalizeArgs_DoesNotMutateInput(t *testing.T) {
	accepted := map[string]struct{}{"repo": {}}
	raw := map[string]any{"repo": "x", "bogus": true}
	res := NormalizeArgs("t", raw, accepted)
	if _, ok := raw["bogus"]; !ok {
		t.Errorf("NormalizeArgs mutated the input map (bogus removed from input)")
	}
	if _, ok := res.Args["bogus"]; ok {
		t.Errorf("bogus present in output")
	}
}

func TestNormalizeArgs_OpenSchemaNoStrip(t *testing.T) {
	// nil accepted = open schema: nothing stripped, no note.
	raw := map[string]any{"anything": 1, "whatever": true}
	res := NormalizeArgs("open_tool", raw, nil)
	if len(res.Stripped) != 0 {
		t.Errorf("open schema should not strip, got %v", res.Stripped)
	}
	if res.Note(nil) != "" {
		t.Errorf("open schema should produce no note")
	}
}

func TestNormalizeArgs_NoteEmptyWhenNothingStripped(t *testing.T) {
	accepted := map[string]struct{}{"repo": {}, "pattern": {}}
	res := NormalizeArgs("code_search", map[string]any{"repo": "x", "pattern": "y"}, accepted)
	if res.Note(accepted) != "" {
		t.Errorf("note should be empty when nothing stripped, got %q", res.Note(accepted))
	}
}

func TestNormalizeArgs_LimitAliasToMaxResults(t *testing.T) {
	accepted := map[string]struct{}{"repo": {}, "pattern": {}, "max_results": {}}
	raw := map[string]any{"repo": "x", "pattern": "y", "limit": float64(20)}
	res := NormalizeArgs("code_search", raw, accepted)
	if _, ok := res.Args["limit"]; ok {
		t.Errorf("limit should be renamed away")
	}
	if v, _ := res.Args["max_results"].(float64); v != 20 {
		t.Errorf("max_results should be 20, got %v", res.Args["max_results"])
	}
	if len(res.Aliased) != 1 || res.Aliased[0] != "limit→max_results" {
		t.Errorf("expected limit→max_results alias, got %v", res.Aliased)
	}
	// limit is not reported as stripped (it was aliased, not stripped).
	for _, s := range res.Stripped {
		if s == "limit" {
			t.Errorf("limit must not appear in stripped after alias")
		}
	}
}

func TestNormalizeArgs_LimitAliasDoesNotOverrideExistingMaxResults(t *testing.T) {
	accepted := map[string]struct{}{"repo": {}, "max_results": {}}
	raw := map[string]any{"repo": "x", "max_results": float64(5), "limit": float64(20)}
	res := NormalizeArgs("code_search", raw, accepted)
	if v, _ := res.Args["max_results"].(float64); v != 5 {
		t.Errorf("existing max_results should win, got %v", res.Args["max_results"])
	}
	if _, ok := res.Args["limit"]; ok {
		t.Errorf("limit copy should be dropped when canonical present")
	}
}

func TestNormalizeArgs_LimitNativeNotRenamed(t *testing.T) {
	// find_duplicates declares `limit` natively — must NOT be renamed.
	accepted := map[string]struct{}{"repo": {}, "limit": {}, "tier": {}}
	raw := map[string]any{"repo": "x", "limit": float64(7)}
	res := NormalizeArgs("find_duplicates", raw, accepted)
	if _, ok := res.Args["limit"]; !ok {
		t.Errorf("native limit must remain, got %v", res.Args)
	}
	if _, ok := res.Args["max_results"]; ok {
		t.Errorf("limit must not be renamed to max_results on native-limit tool")
	}
	if len(res.Aliased) != 0 {
		t.Errorf("no alias expected, got %v", res.Aliased)
	}
}

func TestNormalizeArgs_LimitAliasToTopKForSemanticSearch(t *testing.T) {
	accepted := map[string]struct{}{"repo": {}, "query": {}, "top_k": {}}
	raw := map[string]any{"repo": "x", "query": "q", "limit": float64(8)}
	res := NormalizeArgs("semantic_search", raw, accepted)
	if _, ok := res.Args["limit"]; ok {
		t.Errorf("limit should be renamed away for semantic_search")
	}
	if v, _ := res.Args["top_k"].(float64); v != 8 {
		t.Errorf("top_k should be 8, got %v", res.Args["top_k"])
	}
	if len(res.Aliased) != 1 || res.Aliased[0] != "limit→top_k" {
		t.Errorf("expected limit→top_k, got %v", res.Aliased)
	}
}

func TestNormalizeArgs_InsightsStrippedWithHintOnRememberGraphInsights(t *testing.T) {
	// Session evidence (#568): agents send free-text CONTENT in `insights`
	// expecting a note-store — an alias onto `repo` would map content garbage
	// into a repo lookup. It must be stripped, and a hint must explain the
	// tool's actual purpose.
	accepted := map[string]struct{}{"repo": {}, "max_per_template": {}}
	raw := map[string]any{"insights": "Competitor recon 2026-07-18: 8 players surveyed..."}
	res := NormalizeArgs("remember_graph_insights", raw, accepted)
	if _, ok := res.Args["insights"]; ok {
		t.Errorf("insights should be stripped")
	}
	if _, ok := res.Args["repo"]; ok {
		t.Errorf("repo must NOT be fabricated from insights content, got %v", res.Args["repo"])
	}
	if len(res.Aliased) != 0 {
		t.Errorf("no alias expected, got %v", res.Aliased)
	}
	if len(res.Stripped) != 1 || res.Stripped[0] != "insights" {
		t.Errorf("expected stripped [insights], got %v", res.Stripped)
	}
	hint := StrippedHint("remember_graph_insights", "insights")
	if hint == "" || !strings.Contains(hint, "repo") {
		t.Errorf("expected a hint naming repo, got %q", hint)
	}
	if StrippedHint("code_search", "insights") != "" {
		t.Errorf("hint must be tool-scoped")
	}
}

func TestNormalizeArgs_InsightsAliasOnlyOnRememberGraphInsights(t *testing.T) {
	// insights on a different tool is just an unknown → stripped, not aliased.
	accepted := map[string]struct{}{"repo": {}, "pattern": {}}
	raw := map[string]any{"repo": "x", "insights": "blah"}
	res := NormalizeArgs("code_search", raw, accepted)
	if _, ok := res.Args["repo"]; !ok || res.Args["repo"] != "x" {
		t.Errorf("repo should be untouched, got %v", res.Args["repo"])
	}
	if _, ok := res.Args["insights"]; ok {
		t.Errorf("insights should be stripped on code_search")
	}
	found := false
	for _, s := range res.Stripped {
		if s == "insights" {
			found = true
		}
	}
	if !found {
		t.Errorf("insights should appear in stripped, got %v", res.Stripped)
	}
	if len(res.Aliased) != 0 {
		t.Errorf("no alias expected on code_search, got %v", res.Aliased)
	}
}

func TestNormalizeRawMessage_ByteIdenticalWhenNoChange(t *testing.T) {
	accepted := map[string]struct{}{"repo": {}, "pattern": {}}
	raw := json.RawMessage(`{"repo":"x","pattern":"y"}`)
	out, res, err := normalizeRawMessage("code_search", raw, accepted)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if string(out) != string(raw) {
		t.Errorf("expected byte-identical output when no change, got %q", string(out))
	}
	if len(res.Stripped) != 0 || len(res.Aliased) != 0 {
		t.Errorf("expected no changes, got %+v", res)
	}
}

func TestNormalizeRawMessage_MalformedPassthrough(t *testing.T) {
	// Not a JSON object — pass through so framework reports the real error.
	accepted := map[string]struct{}{"repo": {}}
	raw := json.RawMessage(`[1,2,3]`)
	out, _, err := normalizeRawMessage("code_search", raw, accepted)
	if err == nil {
		t.Errorf("expected parse error for non-object args")
	}
	if string(out) != string(raw) {
		t.Errorf("malformed input should be returned unchanged")
	}
}

func TestNormalizeRawMessage_AliasAndStripTogether(t *testing.T) {
	accepted := map[string]struct{}{"repo": {}, "pattern": {}, "max_results": {}}
	raw := json.RawMessage(`{"repo":"x","pattern":"y","limit":15,"bogus":true}`)
	out, res, err := normalizeRawMessage("code_search", raw, accepted)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("re-unmarshal: %v", err)
	}
	if _, ok := m["limit"]; ok {
		t.Errorf("limit should be aliased away")
	}
	if _, ok := m["bogus"]; ok {
		t.Errorf("bogus should be stripped")
	}
	if m["max_results"].(float64) != 15 {
		t.Errorf("max_results should be 15, got %v", m["max_results"])
	}
	if len(res.Aliased) != 1 || len(res.Stripped) != 1 {
		t.Errorf("expected 1 alias + 1 strip, got %+v", res)
	}
}
