package mcpmeta

import (
	"strings"
	"testing"
)

func TestHintAfterCodeSearch_SingleHitSuggestsUnderstand(t *testing.T) {
	h := HintAfterCodeSearch("foo", 1, "Bar")
	if h == "" || !strings.Contains(h, "understand") || !strings.Contains(h, "Bar") {
		t.Fatalf("expected understand+Bar, got %q", h)
	}
}

func TestHintAfterCodeSearch_ZeroHitsSilent(t *testing.T) {
	if h := HintAfterCodeSearch("foo", 0, ""); h != "" {
		t.Fatalf("zero hits must be silent, got %q", h)
	}
}

func TestHintAfterCodeSearch_ManyHitsSilent(t *testing.T) {
	if h := HintAfterCodeSearch("foo", 42, "Sym"); h != "" {
		t.Fatalf("42 hits must be silent, got %q", h)
	}
}

func TestHintAfterCodeSearch_EmptySymbolSilent(t *testing.T) {
	if h := HintAfterCodeSearch("foo", 1, ""); h != "" {
		t.Fatalf("empty symbol must be silent, got %q", h)
	}
}

func TestHintAfterCodeSearch_ExplainQuerySilent(t *testing.T) {
	cases := []string{"why does X do Y", "how is this implemented", "describe X", "explain the loop"}
	for _, q := range cases {
		if h := HintAfterCodeSearch(q, 1, "Sym"); h != "" {
			t.Fatalf("explain-class query %q must be silent, got %q", q, h)
		}
	}
}

func TestHintAfterDeadCode_HighCountSuggestsHealth(t *testing.T) {
	h := HintAfterDeadCode("foo.go", 7)
	if h == "" || !strings.Contains(h, "get_file_health") || !strings.Contains(h, "foo.go") {
		t.Fatalf("expected get_file_health+foo.go, got %q", h)
	}
}

func TestHintAfterDeadCode_LowCountSilent(t *testing.T) {
	if h := HintAfterDeadCode("foo.go", 4); h != "" {
		t.Fatalf("4 below threshold must be silent, got %q", h)
	}
}

func TestHintAfterDeadCode_EmptyFileSilent(t *testing.T) {
	if h := HintAfterDeadCode("", 10); h != "" {
		t.Fatalf("empty file must be silent, got %q", h)
	}
}

func TestHintAfterDeadCode_ExactlyThreshold_Fires(t *testing.T) {
	// threshold=5, exactly 5 → fires (condition is <, so 5 is NOT below threshold)
	h := HintAfterDeadCode("foo.go", deadCodeHotspotThreshold)
	if h == "" {
		t.Fatalf("exactly threshold must fire a hint, got empty string")
	}
}

func TestExtractSymbolFromHit(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"foo.go:42:func Bar(", "Bar"},
		{"foo.go:42:func Bar() {", "Bar"},
		{"foo.go:42:type Foo struct", "Foo"},
		{"foo.go:42:", ""},
		{"plain text", ""},
	}
	for _, c := range cases {
		got := ExtractSymbolFromHit(c.in)
		if got != c.want {
			t.Fatalf("ExtractSymbolFromHit(%q): got %q want %q", c.in, got, c.want)
		}
	}
}

func TestHintAfterCodeSearch_RussianExplainQuerySilent(t *testing.T) {
	cases := []string{"почему X не работает", "как устроен Y", "опиши Z", "объясни flow", "расскажи про auth"}
	for _, q := range cases {
		if h := HintAfterCodeSearch(q, 1, "Sym"); h != "" {
			t.Fatalf("russian explain query %q must be silent, got %q", q, h)
		}
	}
}

func TestExtractSymbolFromHit_NonDeclarationSilent(t *testing.T) {
	cases := []string{
		"foo.go:42:\tfoo.Bar(x)",           // call site
		`foo.go:42:msg := "feature-flag"`,  // string literal line
		"foo.go:42:// comment with Foo",    // comment
		"foo.go:42:    if foo {",           // control flow
	}
	for _, c := range cases {
		if got := ExtractSymbolFromHit(c); got != "" {
			t.Fatalf("non-declaration %q must return \"\", got %q", c, got)
		}
	}
}

func TestExtractSymbolFromHit_DeclarationsStillWork(t *testing.T) {
	cases := []struct{ in, want string }{
		{"foo.go:42:func Bar(", "Bar"},
		{"foo.go:42:type Foo struct", "Foo"},
		{"foo.go:42:var MaxSize = 100", "MaxSize"},
		{"foo.go:42:const Pi = 3.14", "Pi"},
	}
	for _, c := range cases {
		if got := ExtractSymbolFromHit(c.in); got != c.want {
			t.Fatalf("decl %q: got %q want %q", c.in, got, c.want)
		}
	}
}
