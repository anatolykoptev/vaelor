package main

import (
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/mcpmeta"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func textContentOf(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	if len(r.Content) == 0 {
		return ""
	}
	tc, ok := r.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content[0] is %T, want *mcp.TextContent", r.Content[0])
	}
	return tc.Text
}

// TestMetaResult_EmptyEnvelope_FallsBackToPlainText verifies that a zero-value
// envelope (DurationMS=0, no hint, no warning) produces a result without any
// HTML comment footer — identical to textResult output.
func TestMetaResult_EmptyEnvelope_FallsBackToPlainText(t *testing.T) {
	env := mcpmeta.Envelope{}
	r := metaResult("hello world", env)
	got := textContentOf(t, r)
	if strings.Contains(got, "<!-- meta:") {
		t.Fatalf("empty envelope must not append meta comment, got:\n%s", got)
	}
	if got != "hello world" {
		t.Fatalf("empty envelope must be plain text, got %q", got)
	}
}

// TestMetaResult_NonEmptyEnvelope_AppendsMeta verifies that a populated envelope
// produces a result with the HTML comment footer appended after the body.
func TestMetaResult_NonEmptyEnvelope_AppendsMeta(t *testing.T) {
	env := mcpmeta.Wrap(42*time.Millisecond, "use understand(symbol=Foo)")
	r := metaResult("body text", env)
	got := textContentOf(t, r)
	if !strings.HasPrefix(got, "body text") {
		t.Fatalf("meta result must start with original body, got:\n%s", got)
	}
	if !strings.Contains(got, "<!-- meta:") {
		t.Fatalf("non-empty envelope must append meta comment, got:\n%s", got)
	}
	if !strings.Contains(got, "duration_ms") {
		t.Fatalf("meta comment must contain duration_ms, got:\n%s", got)
	}
}

// TestMetaResult_HintOnlyEnvelope_AppendsMeta verifies that an envelope with
// only a Hint (DurationMS=0) still produces the meta footer.
func TestMetaResult_HintOnlyEnvelope_AppendsMeta(t *testing.T) {
	env := mcpmeta.Envelope{Hint: "next: call understand"}
	r := metaResult("xml here", env)
	got := textContentOf(t, r)
	if !strings.Contains(got, "<!-- meta:") {
		t.Fatalf("hint-only envelope must append meta comment, got:\n%s", got)
	}
	if !strings.Contains(got, "next: call understand") {
		t.Fatalf("hint text must appear in meta comment, got:\n%s", got)
	}
}

// TestMetaXMLMarshalResult_EmptyEnvelope_NoFooter verifies fallback to plain XML.
func TestMetaXMLMarshalResult_EmptyEnvelope_NoFooter(t *testing.T) {
	type dummy struct {
		Val string `xml:"val"`
	}
	r := metaXMLMarshalResult(dummy{Val: "hello"}, "tool", "", mcpmeta.Envelope{})
	got := textContentOf(t, r)
	if strings.Contains(got, "<!-- meta:") {
		t.Fatalf("empty envelope must not append meta comment, got:\n%s", got)
	}
	if !strings.Contains(got, "hello") {
		t.Fatalf("XML body must be present, got:\n%s", got)
	}
}

// TestMetaXMLMarshalResult_NonEmptyEnvelope_AppendsFooter verifies meta comment appears.
func TestMetaXMLMarshalResult_NonEmptyEnvelope_AppendsFooter(t *testing.T) {
	type dummy struct {
		Val string `xml:"val"`
	}
	env := mcpmeta.Wrap(10*time.Millisecond, "")
	r := metaXMLMarshalResult(dummy{Val: "world"}, "tool", "", env)
	got := textContentOf(t, r)
	if !strings.Contains(got, "world") {
		t.Fatalf("XML body must be present, got:\n%s", got)
	}
	if !strings.Contains(got, "<!-- meta:") {
		t.Fatalf("non-empty envelope must append meta comment, got:\n%s", got)
	}
}

// TestAppendMetaFooter_EmptyEnvelopeNoOp verifies the helper returns the
// body unchanged when the envelope carries no signal.
func TestAppendMetaFooter_EmptyEnvelopeNoOp(t *testing.T) {
	got := appendMetaFooter("hello", mcpmeta.Envelope{})
	if got != "hello" {
		t.Fatalf("empty envelope must be no-op, got %q", got)
	}
}

// TestAppendMetaFooter_NonEmptyAppends verifies the helper appends the
// HTML-comment footer with a leading blank-line separator.
func TestAppendMetaFooter_NonEmptyAppends(t *testing.T) {
	env := mcpmeta.Wrap(50*time.Millisecond, "next call X")
	got := appendMetaFooter("body", env)
	want := "body\n\n<!-- meta: " + `{"duration_ms":50,"hint":"next call X"}` + " -->"
	if got != want {
		t.Fatalf("appendMetaFooter:\n got %q\nwant %q", got, want)
	}
}

// TestMetaLargeTextResult_EnvelopeAppearsInSummaryWhenSavedToFile verifies that the
// meta envelope footer is present in the visible summary message even when the body
// is saved to a file (not returned inline).
func TestMetaLargeTextResult_EnvelopeAppearsInSummaryWhenSavedToFile(t *testing.T) {
	bigText := strings.Repeat("X", maxInlineCharsDefault*2)
	env := mcpmeta.Wrap(50*time.Millisecond, "test hint")
	tmpDir := t.TempDir()
	res := metaLargeTextResult(bigText, "test_tool", tmpDir, env)
	if res == nil || len(res.Content) == 0 {
		t.Fatal("nil result")
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("expected *mcp.TextContent, got %T", res.Content[0])
	}
	if !strings.Contains(tc.Text, "<!-- meta:") {
		t.Fatalf("envelope footer must appear in visible summary even when body saved to file, got: %q", tc.Text)
	}
	if !strings.Contains(tc.Text, "test hint") {
		t.Fatalf("hint must surface in visible summary, got: %q", tc.Text)
	}
}
