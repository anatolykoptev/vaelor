// Package lextoken_test pins the exact observable behaviour of the lextoken
// functions.  These characterization tests are the contract: if any test goes
// RED after the extract, the behaviour changed and the extract is incorrect.
package lextoken_test

import (
	"reflect"
	"testing"

	"github.com/anatolykoptev/go-code/internal/lextoken"
)

// ---------------------------------------------------------------------------
// SplitCamelCase — derived from splitCamelCase (analyze/context.go:138)
// ---------------------------------------------------------------------------

func TestSplitCamelCase_Basic(t *testing.T) {
	t.Parallel()
	got := lextoken.SplitCamelCase("parseJSONString")
	want := []string{"parse", "json", "string"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SplitCamelCase(%q) = %v, want %v", "parseJSONString", got, want)
	}
}

func TestSplitCamelCase_Empty(t *testing.T) {
	t.Parallel()
	got := lextoken.SplitCamelCase("")
	if got != nil {
		t.Errorf("SplitCamelCase(%q) = %v, want nil", "", got)
	}
}

func TestSplitCamelCase_AllCaps(t *testing.T) {
	t.Parallel()
	// "HTTP" → one part, len<2 parts dropped; "S" dropped
	got := lextoken.SplitCamelCase("HTTPServer")
	want := []string{"http", "server"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SplitCamelCase(%q) = %v, want %v", "HTTPServer", got, want)
	}
}

func TestSplitCamelCase_SingleWord(t *testing.T) {
	t.Parallel()
	got := lextoken.SplitCamelCase("parse")
	want := []string{"parse"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SplitCamelCase(%q) = %v, want %v", "parse", got, want)
	}
}

func TestSplitCamelCase_LenFilter(t *testing.T) {
	t.Parallel()
	// "aB" → ["a","b"] but len<2 → both dropped → nil
	got := lextoken.SplitCamelCase("aB")
	if len(got) != 0 {
		t.Errorf("SplitCamelCase(%q) = %v, want empty", "aB", got)
	}
}

// ---------------------------------------------------------------------------
// SplitIdentifier — derived from splitIdentifier (analyze/context.go:186)
// ---------------------------------------------------------------------------

func TestSplitIdentifier_SnakeCase(t *testing.T) {
	t.Parallel()
	got := lextoken.SplitIdentifier("parse_json_string")
	want := []string{"parse", "json", "string"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SplitIdentifier(%q) = %v, want %v", "parse_json_string", got, want)
	}
}

func TestSplitIdentifier_CamelSnakeMix(t *testing.T) {
	t.Parallel()
	got := lextoken.SplitIdentifier("parseJSONString")
	want := []string{"parse", "json", "string"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SplitIdentifier(%q) = %v, want %v", "parseJSONString", got, want)
	}
}

func TestSplitIdentifier_Empty(t *testing.T) {
	t.Parallel()
	got := lextoken.SplitIdentifier("")
	if got != nil {
		t.Errorf("SplitIdentifier(%q) = %v, want nil", "", got)
	}
}

func TestSplitIdentifier_LeadingTrailingUnderscores(t *testing.T) {
	t.Parallel()
	got := lextoken.SplitIdentifier("_get_value_")
	// "_" splits → ["", "get", "value", ""] — empty parts skipped
	want := []string{"get", "value"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SplitIdentifier(%q) = %v, want %v", "_get_value_", got, want)
	}
}

// ---------------------------------------------------------------------------
// Tokenize — derived from extractQueryTerms (analyze/context.go:202)
// Note: extractQueryTerms does NOT filter stopwords — it does identifier-split.
// ---------------------------------------------------------------------------

func TestTokenize_SimpleQuery(t *testing.T) {
	t.Parallel()
	got := lextoken.Tokenize("What functions are defined in util?")
	// "what", "functions", "are", "defined", "util" — no identifier split on
	// plain words; "are" and "what" are short or kept (no stopword filter here)
	want := map[string]bool{
		"what":      true,
		"functions": true,
		"are":       true,
		"defined":   true,
		"util":      true,
	}
	if len(got) != len(want) {
		t.Errorf("Tokenize: got %v (len=%d), want len=%d", got, len(got), len(want))
		return
	}
	gotSet := make(map[string]bool, len(got))
	for _, t2 := range got {
		gotSet[t2] = true
	}
	for w := range want {
		if !gotSet[w] {
			t.Errorf("Tokenize: missing %q, got %v", w, got)
		}
	}
}

func TestTokenize_CamelCase(t *testing.T) {
	t.Parallel()
	got := lextoken.Tokenize("find the HTTPServer handler")
	want := map[string]bool{
		"find":       true,
		"the":        true,
		"httpserver": true,
		"http":       true,
		"server":     true,
		"handler":    true,
	}
	gotSet := make(map[string]bool, len(got))
	for _, t2 := range got {
		gotSet[t2] = true
	}
	for w := range want {
		if !gotSet[w] {
			t.Errorf("Tokenize: missing %q, got %v", w, got)
		}
	}
}

func TestTokenize_SnakeCase(t *testing.T) {
	t.Parallel()
	got := lextoken.Tokenize("parse_file_content")
	want := map[string]bool{
		"parse":              true,
		"file":               true,
		"content":            true,
		"parse_file_content": true,
	}
	gotSet := make(map[string]bool, len(got))
	for _, t2 := range got {
		gotSet[t2] = true
	}
	for w := range want {
		if !gotSet[w] {
			t.Errorf("Tokenize: missing %q, got %v", w, got)
		}
	}
}

func TestTokenize_Empty(t *testing.T) {
	t.Parallel()
	got := lextoken.Tokenize("")
	if len(got) != 0 {
		t.Errorf("Tokenize(%q) = %v, want empty", "", got)
	}
}

func TestTokenize_Dedup(t *testing.T) {
	t.Parallel()
	got := lextoken.Tokenize("foo foo foo")
	count := 0
	for _, t2 := range got {
		if t2 == "foo" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Tokenize: expected 'foo' exactly once, got %d times in %v", count, got)
	}
}

// ---------------------------------------------------------------------------
// FilterStopwords — derived from extractKeywordsForBoost / ExtractQueryKeywords
// (analyze/rank.go:286, embeddings/store_keyword.go:76 — byte-identical logic)
// ---------------------------------------------------------------------------

func TestFilterStopwords_RemovesStopwords(t *testing.T) {
	t.Parallel()
	// These are exactly the stopwords from both callers.
	stopwords := []string{"the", "and", "for", "that", "with", "this", "from",
		"are", "not", "have", "function", "method", "code", "file",
		"which", "where", "when", "how", "what"}
	for _, sw := range stopwords {
		got := lextoken.FilterStopwords([]string{sw})
		if len(got) != 0 {
			t.Errorf("FilterStopwords([%q]) = %v, want empty (should be filtered)", sw, got)
		}
	}
}

func TestFilterStopwords_KeepsNonStopwords(t *testing.T) {
	t.Parallel()
	input := []string{"parse", "config", "handler"}
	got := lextoken.FilterStopwords(input)
	if !reflect.DeepEqual(got, input) {
		t.Errorf("FilterStopwords(%v) = %v, want %v", input, got, input)
	}
}

func TestFilterStopwords_Nil(t *testing.T) {
	t.Parallel()
	got := lextoken.FilterStopwords(nil)
	if got != nil {
		t.Errorf("FilterStopwords(nil) = %v, want nil", got)
	}
}

// ---------------------------------------------------------------------------
// KeywordTokenize — derived from extractKeywordsForBoost / ExtractQueryKeywords
// (the full pipeline: lowercase → alnum-split → min-3-char → stopword filter → dedup)
// ---------------------------------------------------------------------------

func TestKeywordTokenize_Basic(t *testing.T) {
	t.Parallel()
	// "find the HTTPServer handler" → ["find", "httpserver", "handler"] (no id-split)
	got := lextoken.KeywordTokenize("find the HTTPServer handler")
	want := map[string]bool{"find": true, "httpserver": true, "handler": true}
	gotSet := make(map[string]bool, len(got))
	for _, t2 := range got {
		gotSet[t2] = true
	}
	for w := range want {
		if !gotSet[w] {
			t.Errorf("KeywordTokenize: missing %q, got %v", w, got)
		}
	}
	// "the" must be absent
	if gotSet["the"] {
		t.Errorf("KeywordTokenize: 'the' should be filtered as stopword, got %v", got)
	}
}

func TestKeywordTokenize_StopwordFromRankGo(t *testing.T) {
	t.Parallel()
	// "what code is in file function" — all stopwords → empty
	got := lextoken.KeywordTokenize("what code is in file function")
	// "is" and "in" are <3 chars → already filtered by len; rest are stopwords
	if len(got) != 0 {
		t.Errorf("KeywordTokenize: expected empty, got %v", got)
	}
}

func TestKeywordTokenize_Dedup(t *testing.T) {
	t.Parallel()
	got := lextoken.KeywordTokenize("parse parse parse")
	count := 0
	for _, t2 := range got {
		if t2 == "parse" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("KeywordTokenize: 'parse' expected once, got %d in %v", count, got)
	}
}

func TestKeywordTokenize_Empty(t *testing.T) {
	t.Parallel()
	got := lextoken.KeywordTokenize("")
	if len(got) != 0 {
		t.Errorf("KeywordTokenize(%q) = %v, want empty", "", got)
	}
}

// ---------------------------------------------------------------------------
// Stopwords map membership — the EXACT set from both callers (documented contract)
// ---------------------------------------------------------------------------

func TestStopwordsSet_ExactMembership(t *testing.T) {
	t.Parallel()
	// These must all be in the set (from analyze/rank.go and embeddings/store_keyword.go).
	must := []string{
		"the", "and", "for", "that", "with", "this", "from", "are", "not", "have",
		"function", "method", "code", "file", "which", "where", "when", "how", "what",
	}
	for _, w := range must {
		if !lextoken.IsStopword(w) {
			t.Errorf("IsStopword(%q) = false, want true", w)
		}
	}
}

func TestStopwordsSet_NonMembers(t *testing.T) {
	t.Parallel()
	nonStop := []string{"parse", "config", "handler", "server", "query"}
	for _, w := range nonStop {
		if lextoken.IsStopword(w) {
			t.Errorf("IsStopword(%q) = true, want false", w)
		}
	}
}
