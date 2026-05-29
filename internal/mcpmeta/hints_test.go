package mcpmeta

import "testing"

func TestHintAfterCodeSearch_SingleHitSuggestsUnderstand(t *testing.T) {
	h := HintAfterCodeSearch("foo", 1, "Bar")
	if h == "" || !containsStr(h, "understand") || !containsStr(h, "Bar") {
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
	if h == "" || !containsStr(h, "get_file_health") || !containsStr(h, "foo.go") {
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

// containsStr is a stdlib-only strings.Contains replacement to avoid
// importing strings in this test file's helper (avoid shadowing).
func containsStr(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
