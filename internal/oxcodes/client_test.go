package oxcodes

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestScopedSearchInput_EmptyLanguage_Omitted asserts that when Language is empty,
// the marshaled JSON does NOT contain a "language" key. This prevents the
// "400: unsupported language: """ error from ox-codes /search/scoped.
//
// Regression guard for investigation 2026-06-12 (ITEM 3): ScopedSearchInput.Language
// lacked the omitempty tag while its sibling SearchInput.Language had it. The missing
// tag caused every impact_analysis call without an explicit language= to send
// "language":"" to ox-codes, returning 400 and silently disabling scoped search.
//
// RED guarantee: remove the omitempty tag from ScopedSearchInput.Language, and this
// test fails because the marshaled JSON contains "\"language\":\"\"".
func TestScopedSearchInput_EmptyLanguage_Omitted(t *testing.T) {
	input := ScopedSearchInput{
		Root:    "/some/repo",
		Pattern: "MyFunc",
		Scope:   "function_bodies",
		// Language deliberately left empty — the common path when impact_analysis
		// is called without an explicit language parameter.
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(data), `"language"`) {
		t.Errorf("empty Language must be omitted from JSON; got: %s", string(data))
	}
}

// TestScopedSearchInput_NonEmptyLanguage_Included asserts that a non-empty Language
// IS included in the marshaled JSON (omitempty must not drop meaningful values).
func TestScopedSearchInput_NonEmptyLanguage_Included(t *testing.T) {
	input := ScopedSearchInput{
		Root:     "/some/repo",
		Pattern:  "MyFunc",
		Scope:    "function_bodies",
		Language: "go",
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"language":"go"`) {
		t.Errorf("non-empty Language must be present in JSON; got: %s", string(data))
	}
}

// TestSearchInput_EmptyLanguage_Omitted is the baseline: SearchInput already has
// omitempty and must not regress.
func TestSearchInput_EmptyLanguage_Omitted(t *testing.T) {
	input := SearchInput{
		Root:    "/some/repo",
		Pattern: "MyFunc",
	}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if strings.Contains(string(data), `"language"`) {
		t.Errorf("SearchInput empty Language must be omitted (baseline); got: %s", string(data))
	}
}
