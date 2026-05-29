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
