package forge

import (
	"strings"
	"testing"
)

func TestSanitizeSearchFragment_MarkdownFurniture(t *testing.T) {
	t.Parallel()
	frag := strings.Join([]string{
		`<p align="center">`,
		`<img src="https://example.com/badge.svg" alt="build" />`,
		`</p>`,
		`[![CI](https://img.shields.io/badge/ci)](https://ci.example.com)`,
		`[Docs](https://docs.example.com) • [API](https://api.example.com) • [Site](https://example.com)`,
		`<!-- TODO: replace with your badge -->`,
		`---`,
		`https://example.com/some/page`,
		`&nbsp;`,
		`The trending extractor normalizes markdown tables.`,
		`See [README](https://example.com/readme) for details.`,
	}, "\n")

	// The match GitHub would report: "trending extractor" inside the prose line.
	matchStart := strings.Index(frag, "trending extractor")
	matches := []ghMatchSpan{{Text: "trending extractor", Indices: []int{matchStart, matchStart + len("trending extractor")}}}

	got := sanitizeSearchFragment(frag, matches, []string{"trending"})

	for _, gone := range []string{"<p align", "img.shields", "Docs](", "TODO", "example.com/some/page", "&nbsp;"} {
		if strings.Contains(got, gone) {
			t.Errorf("furniture survived %q:\n%s", gone, got)
		}
	}
	for _, kept := range []string{"trending extractor", "See", "for details"} {
		if !strings.Contains(got, kept) {
			t.Errorf("prose lost %q:\n%s", kept, got)
		}
	}
}

func TestSanitizeSearchFragment_MatchSpanProtects(t *testing.T) {
	t.Parallel()
	// A badge line is furniture in general, but when the GitHub match span
	// lands on it, it is the evidence itself and must survive — no term
	// guessing involved.
	frag := "[![coverage](https://img.shields.io/coverage.svg)](https://ci.example.com)\nuseful prose"
	matches := []ghMatchSpan{{Text: "coverage", Indices: []int{4, 12}}}

	got := sanitizeSearchFragment(frag, matches, nil)
	if !strings.Contains(got, "coverage](https://img.shields") {
		t.Errorf("matched badge line dropped:\n%s", got)
	}
	// Same fragment, match on the prose line only: badge line goes.
	frag2 := frag
	m2 := []ghMatchSpan{{Text: "prose", Indices: []int{strings.Index(frag2, "prose"), strings.Index(frag2, "prose") + 5}}}
	got2 := sanitizeSearchFragment(frag2, m2, nil)
	if strings.Contains(got2, "img.shields") {
		t.Errorf("unmatched badge line kept:\n%s", got2)
	}
	if !strings.Contains(got2, "prose") {
		t.Errorf("matched prose lost:\n%s", got2)
	}
}

func TestSanitizeSearchFragment_QueryTermProtects(t *testing.T) {
	t.Parallel()
	// Term fallback covers the case where the API returned no match spans.
	frag := "[![coverage badge](https://img.shields.io/coverage.svg)](https://ci.example.com)\nuseful prose"

	noTerm := sanitizeSearchFragment(frag, nil, []string{"extractor"})
	if strings.Contains(noTerm, "coverage badge") {
		t.Errorf("badge line kept without match or term:\n%s", noTerm)
	}
	withTerm := sanitizeSearchFragment(frag, nil, []string{"badge"})
	if !strings.Contains(withTerm, "coverage badge") {
		t.Errorf("badge line dropped despite query term:\n%s", withTerm)
	}
	// Number-only line: furniture for another query, evidence for this one.
	num := "12345"
	if got := sanitizeSearchFragment(num, nil, []string{"readme"}); got != "" {
		t.Errorf("number-only line kept: %q", got)
	}
	if got := sanitizeSearchFragment(num, nil, []string{"12345"}); !strings.Contains(got, "12345") {
		t.Errorf("number-only match line dropped: %q", got)
	}
}

func TestSanitizeSearchFragment_PreservesFencedCode(t *testing.T) {
	t.Parallel()
	frag := strings.Join([]string{
		"Usage example:",
		"```go",
		"if x {",
		"}",
		"```",
		"---",
		"Footer text",
	}, "\n")

	got := sanitizeSearchFragment(frag, nil, []string{"usage"})

	for _, kept := range []string{"```go", "if x {", "}", "Footer text"} {
		if !strings.Contains(got, kept) {
			t.Errorf("fenced code lost %q:\n%s", kept, got)
		}
	}
	if strings.Contains(got, "---") {
		t.Errorf("hr line survived outside fence:\n%s", got)
	}
}

func TestBuildCodeSearchContent_MarkdownSanitized(t *testing.T) {
	t.Parallel()
	var item ghCodeSearchItem
	item.Name = "README.md"
	item.Path = "README.md"
	frag := "[![CI](https://img.shields.io/ci.svg)](https://ci.example.com)\n---\nThe extractor parses markdown.\n"
	item.TextMatches = []ghCodeSearchTextMatch{
		{
			Fragment: frag,
			Matches:  []ghMatchSpan{{Text: "extractor", Indices: []int{strings.Index(frag, "extractor"), strings.Index(frag, "extractor") + 9}}},
		},
	}

	got := buildCodeSearchContent(item, []string{"extractor"}, 0, 0)
	if !strings.Contains(got, "extractor parses markdown") {
		t.Fatalf("prose lost: %q", got)
	}
	if strings.Contains(got, "img.shields") || strings.Contains(got, "\n---") {
		t.Errorf("furniture survived md path: %q", got)
	}
}

func TestBuildCodeSearchContent_NonMarkdownVerbatim(t *testing.T) {
	t.Parallel()
	var item ghCodeSearchItem
	item.Name = "page.html"
	item.Path = "templates/page.html"
	item.TextMatches = []ghCodeSearchTextMatch{
		{Fragment: "<div class=\"wrap\">\n  ---\n</div>"},
	}

	got := buildCodeSearchContent(item, []string{"navbar"}, 0, 0)
	if !strings.Contains(got, "---") || !strings.Contains(got, "<div") {
		t.Errorf("code-file fragment was sanitized:\n%s", got)
	}
}

func TestSearchQueryTerms(t *testing.T) {
	t.Parallel()
	got := searchQueryTerms(`"quoted phrase" extractor repo:a/b language:go -path:vendor OR filter`)
	want := []string{"quoted phrase", "extractor", "filter"}
	if len(got) != len(want) {
		t.Fatalf("terms = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("terms = %v, want %v", got, want)
		}
	}
}

func TestSanitizeSearchFragment_BlankCollapse(t *testing.T) {
	t.Parallel()
	frag := "line one\n\n\n\n\nline two"
	got := sanitizeSearchFragment(frag, nil, []string{"nomatch"})
	if got != "line one\n\nline two" {
		t.Errorf("blank runs not collapsed: %q", got)
	}
}
