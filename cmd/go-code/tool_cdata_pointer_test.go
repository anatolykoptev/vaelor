package main

import (
	"encoding/xml"
	"strings"
	"testing"
)

// assertNoEmptyTag fails if the XML output contains an empty element for tagName
// in any of the three forms: <tag></tag>, <tag/>, or a bare open tag <tag>.
func assertNoEmptyTag(t *testing.T, out, tagName string) {
	t.Helper()
	if strings.Contains(out, "<"+tagName+"></"+tagName+">") {
		t.Fatalf("empty <%s></%s> must be omitted:\n%s", tagName, tagName, out)
	}
	if strings.Contains(out, "<"+tagName+"/>") {
		t.Fatalf("empty <%s/> must be omitted:\n%s", tagName, out)
	}
	if strings.Contains(out, "<"+tagName+">") {
		t.Fatalf("bare <%s> tag must be omitted when content is empty:\n%s", tagName, out)
	}
}

func marshal(t *testing.T, v any) string {
	t.Helper()
	data, err := xml.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("xml.Marshal: %v", err)
	}
	return string(data)
}

// TestCallTrace_EmptyNarrativeOmitted asserts that a trace response with no
// narrative does not emit an empty <narrative> tag (omitempty on a value-type
// xmlCDATA silently failed — pointer form is the fix, same as PR #19/#20).
func TestCallTrace_EmptyNarrativeOmitted(t *testing.T) {
	resp := xmlTraceResponse{
		Trace: xmlTrace{
			Symbol:    "foo",
			Direction: "forward",
		},
	}
	out := marshal(t, resp)
	assertNoEmptyTag(t, out, "narrative")
}

// TestCallTrace_NarrativePresentWhenSet asserts the <narrative> tag is emitted
// when Narrative is non-nil.
func TestCallTrace_NarrativePresentWhenSet(t *testing.T) {
	resp := xmlTraceResponse{
		Trace: xmlTrace{
			Symbol:    "foo",
			Direction: "forward",
			Narrative: &xmlCDATA{Inner: wrapCDATA("call path summary")},
		},
	}
	out := marshal(t, resp)
	if !strings.Contains(out, "call path summary") {
		t.Fatalf("narrative content must be present:\n%s", out)
	}
}

// TestCallTrace_EmptyNodeSignatureOmitted asserts that a trace node without a
// signature (e.g. unresolved call site) does not emit an empty <signature>.
func TestCallTrace_EmptyNodeSignatureOmitted(t *testing.T) {
	node := xmlTraceNode{
		Kind: "function",
		Name: "bar",
		File: "bar.go",
		Line: 10,
		// Signature intentionally nil
	}
	out := marshal(t, node)
	assertNoEmptyTag(t, out, "signature")
}

// TestCallTrace_SignaturePresentWhenSet asserts the <signature> tag is emitted
// when Signature is non-nil.
func TestCallTrace_SignaturePresentWhenSet(t *testing.T) {
	node := xmlTraceNode{
		Kind:      "function",
		Name:      "bar",
		File:      "bar.go",
		Line:      10,
		Signature: &xmlCDATA{Inner: wrapCDATA("func bar(x int) error")},
	}
	out := marshal(t, node)
	if !strings.Contains(out, "func bar(x int) error") {
		t.Fatalf("signature content must be present:\n%s", out)
	}
}

// TestDeadCode_EmptyNarrativeOmitted asserts that a dead_code response with no
// narrative does not emit an empty <narrative> tag.
func TestDeadCode_EmptyNarrativeOmitted(t *testing.T) {
	resp := xmlDeadCodeResponse{
		DeadCode: xmlDeadCode{
			Total: 10,
			Dead:  3,
			Ratio: 0.3,
		},
	}
	out := marshal(t, resp)
	assertNoEmptyTag(t, out, "narrative")
}

// TestDeadCode_NarrativePresentWhenSet asserts the <narrative> tag is emitted
// when Narrative is non-nil.
func TestDeadCode_NarrativePresentWhenSet(t *testing.T) {
	resp := xmlDeadCodeResponse{
		DeadCode: xmlDeadCode{
			Total:     10,
			Dead:      3,
			Ratio:     0.3,
			Narrative: &xmlCDATA{Inner: wrapCDATA("3 dead exports found")},
		},
	}
	out := marshal(t, resp)
	if !strings.Contains(out, "3 dead exports found") {
		t.Fatalf("narrative content must be present:\n%s", out)
	}
}

// TestCodeSearch_EmptyBodyOmitted asserts that an xmlExpandedBlock with an
// empty body does not emit an empty <body> tag.
func TestCodeSearch_EmptyBodyOmitted(t *testing.T) {
	block := xmlExpandedBlock{
		SymbolName: "Login",
		SymbolKind: "function",
		LineStart:  10,
		LineEnd:    20,
		// Body intentionally nil
	}
	out := marshal(t, block)
	assertNoEmptyTag(t, out, "body")
}

// TestCodeSearch_BodyPresentWhenSet asserts the <body> tag is emitted when
// Body is non-nil.
func TestCodeSearch_BodyPresentWhenSet(t *testing.T) {
	block := xmlExpandedBlock{
		SymbolName: "Login",
		SymbolKind: "function",
		LineStart:  10,
		LineEnd:    20,
		Body:       &xmlCDATA{Inner: wrapCDATA("func Login() {}")},
	}
	out := marshal(t, block)
	if !strings.Contains(out, "func Login() {}") {
		t.Fatalf("body content must be present:\n%s", out)
	}
}

// TestCodeSearch_EmptyContextLinesFiltered asserts that formatCodeSearchXML
// does not produce empty <ctx> entries for empty context strings.
func TestCodeSearch_EmptyContextLinesFiltered(t *testing.T) {
	input := CodeSearchInput{Pattern: "foo"}
	matches := []searchMatchForTest{
		{file: "a.go", line: 5, text: "foo()", context: []string{"", "// comment", ""}},
	}
	resp := xmlSearchResponse{
		Search: xmlSearch{
			Pattern: input.Pattern,
			Matches: 1,
			Items: []xmlSearchMatch{
				{
					File: matches[0].file,
					Line: matches[0].line,
					Text: xmlCDATA{Inner: wrapCDATA(matches[0].text)},
				},
			},
		},
	}
	for _, c := range matches[0].context {
		if c != "" {
			resp.Search.Items[0].Context = append(resp.Search.Items[0].Context, xmlCDATA{Inner: wrapCDATA(c)})
		}
	}
	out := marshal(t, resp)
	// Only one non-empty context line: "// comment"
	if !strings.Contains(out, "// comment") {
		t.Fatalf("non-empty context line must be present:\n%s", out)
	}
	ctxCount := strings.Count(out, "<ctx>")
	if ctxCount != 1 {
		t.Fatalf("expected 1 <ctx> entry (empty ones filtered), got %d:\n%s", ctxCount, out)
	}
}

// TestRewrite_EmptyDiffOmitted asserts that an xmlRewriteFile with an empty
// diff does not emit an empty <diff> tag.
func TestRewrite_EmptyDiffOmitted(t *testing.T) {
	f := xmlRewriteFile{
		Path:    "cmd/main.go",
		Matches: 2,
		// Diff intentionally nil
	}
	out := marshal(t, f)
	assertNoEmptyTag(t, out, "diff")
}

// TestRewrite_DiffPresentWhenSet asserts the <diff> tag is emitted when Diff
// is non-nil.
func TestRewrite_DiffPresentWhenSet(t *testing.T) {
	f := xmlRewriteFile{
		Path:    "cmd/main.go",
		Matches: 2,
		Diff:    &xmlCDATA{Inner: wrapCDATA("--- a\n+++ b\n@@ -1 +1 @@\n-old\n+new")},
	}
	out := marshal(t, f)
	if !strings.Contains(out, "-old") {
		t.Fatalf("diff content must be present:\n%s", out)
	}
}

// searchMatchForTest is a local helper to avoid importing codesearch in test.
type searchMatchForTest struct {
	file    string
	line    int
	text    string
	context []string
}
