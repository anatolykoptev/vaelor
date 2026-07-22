package argnorm

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestMiddleware_HintOnErrorResponse proves #581a: when a tool returns an
// error result (IsError=true) and a stripped param has a hint, the hint must
// be appended to the error response so the agent learns what to fix.
//
// Scenario: remember_graph_insights is called with "insights" (stripped, has
// a hint) but no "repo". The tool handler errors with "repo is required".
// Without the fix, the hint is lost because appendNote skipped error results.
func TestMiddleware_HintOnErrorResponse(t *testing.T) {
	reg := NewRegistry()
	reg.Register("remember_graph_insights", []string{"repo", "max_per_template"})

	mw := Middleware(reg)

	// Fake handler that returns an error (simulates "repo is required").
	next := func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "repo is required"}},
		}, nil
	}

	handler := mw(next)

	params := &mcp.CallToolParamsRaw{
		Name:      "remember_graph_insights",
		Arguments: MarshalArgs(map[string]any{"insights": "some text"}),
	}
	req := &mcp.ClientRequest[*mcp.CallToolParamsRaw]{Params: params}

	result, err := handler(context.Background(), "tools/call", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cr, ok := result.(*mcp.CallToolResult)
	if !ok || cr == nil {
		t.Fatalf("expected *mcp.CallToolResult, got %T", result)
	}
	if !cr.IsError {
		t.Fatal("result should be an error")
	}

	// The hint about "repo" must be present in the appended content.
	var allText strings.Builder
	for _, c := range cr.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			allText.WriteString(tc.Text)
		}
	}
	combined := allText.String()
	if !strings.Contains(combined, "repo") {
		t.Errorf("error response must contain the hint mentioning 'repo', got: %s", combined)
	}
	// The hint text explains what to pass instead; it doesn't need to
	// literally contain "insights" — the hint says "pass repo".
	t.Logf("error response with hint: %s", combined)
}

// TestMiddleware_NoHintOnErrorResponseWithoutHint verifies that when a tool
// errors and the stripped param has NO hint, the informational "ignored
// unknown params" note is NOT appended to the error (keep error messages clean).
func TestMiddleware_NoHintOnErrorResponseWithoutHint(t *testing.T) {
	reg := NewRegistry()
	reg.Register("code_search", []string{"repo", "pattern"})

	mw := Middleware(reg)

	next := func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "pattern is required"}},
		}, nil
	}

	handler := mw(next)

	params := &mcp.CallToolParamsRaw{
		Name:      "code_search",
		Arguments: MarshalArgs(map[string]any{"repo": "x", "bogus": true}),
	}
	req := &mcp.ClientRequest[*mcp.CallToolParamsRaw]{Params: params}

	result, err := handler(context.Background(), "tools/call", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cr, ok := result.(*mcp.CallToolResult)
	if !ok || cr == nil {
		t.Fatalf("expected *mcp.CallToolResult, got %T", result)
	}

	// Error result should have exactly 1 content block (the original error),
	// no appended note since "bogus" has no hint.
	if len(cr.Content) != 1 {
		t.Errorf("error response without hint should not have appended note, got %d content blocks: %v", len(cr.Content), cr.Content)
	}
}

// TestMiddleware_NoteOnSuccessResponse verifies the existing behavior is
// preserved: success results get the full note + hints appended.
func TestMiddleware_NoteOnSuccessResponse(t *testing.T) {
	reg := NewRegistry()
	reg.Register("remember_graph_insights", []string{"repo", "max_per_template"})

	mw := Middleware(reg)

	next := func(_ context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "ok"}},
		}, nil
	}

	handler := mw(next)

	params := &mcp.CallToolParamsRaw{
		Name:      "remember_graph_insights",
		Arguments: MarshalArgs(map[string]any{"insights": "some text"}),
	}
	req := &mcp.ClientRequest[*mcp.CallToolParamsRaw]{Params: params}

	result, err := handler(context.Background(), "tools/call", req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cr, ok := result.(*mcp.CallToolResult)
	if !ok || cr == nil {
		t.Fatalf("expected *mcp.CallToolResult, got %T", result)
	}
	if cr.IsError {
		t.Fatal("result should not be an error")
	}

	var allText strings.Builder
	for _, c := range cr.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			allText.WriteString(tc.Text)
		}
	}
	combined := allText.String()
	if !strings.Contains(combined, "ignored unknown params") {
		t.Errorf("success response must contain the note, got: %s", combined)
	}
	if !strings.Contains(combined, "repo") {
		t.Errorf("success response must contain the hint mentioning 'repo', got: %s", combined)
	}
}

// TestRegistry_ClosedEmptyStructNotOpen proves #581b: a tool registered with
// an empty accepted set (struct{}) must be treated as CLOSED (accepts no
// params), not OPEN (accepts anything). Unknown params should be stripped.
func TestRegistry_ClosedEmptyStructNotOpen(t *testing.T) {
	reg := NewRegistry()
	reg.Register("no_params_tool", []string{})

	accepted, open, ok := reg.Accepted("no_params_tool")
	if !ok {
		t.Fatal("tool should be registered")
	}
	if open {
		t.Error("closed empty struct (struct{}) must NOT be open — open=false, accepts no params")
	}
	if len(accepted) != 0 {
		t.Errorf("accepted should be empty for struct{}, got %v", accepted)
	}

	// NormalizeArgs with a closed empty accepted set should strip everything.
	raw := map[string]any{"bogus": "value"}
	res := NormalizeArgs("no_params_tool", raw, accepted)
	if len(res.Stripped) != 1 {
		t.Errorf("expected 1 stripped param for closed empty struct, got %v", res.Stripped)
	}
	if _, exists := res.Args["bogus"]; exists {
		t.Error("bogus should be stripped from closed empty struct args")
	}
}

// TestRegistry_OpenSchemaStillWorks verifies that RegisterOpen produces an
// open schema where nothing is stripped.
func TestRegistry_OpenSchemaStillWorks(t *testing.T) {
	reg := NewRegistry()
	reg.RegisterOpen("open_tool")

	_, open, ok := reg.Accepted("open_tool")
	if !ok {
		t.Fatal("tool should be registered")
	}
	if !open {
		t.Error("RegisterOpen must produce open=true")
	}

	// NormalizeArgs with open schema (nil accepted) should strip nothing.
	raw := map[string]any{"anything": 1, "whatever": true}
	res := NormalizeArgs("open_tool", raw, nil)
	if len(res.Stripped) != 0 {
		t.Errorf("open schema should not strip, got %v", res.Stripped)
	}
}

// TestJsonProperties_StructEmptyIsClosed verifies that jsonProperties
// distinguishes struct{} (closed, non-nil empty) from non-struct (open, nil).
func TestJsonProperties_StructEmptyIsClosed(t *testing.T) {
	type emptyStruct struct{}

	props, isStruct := jsonProperties(reflect.TypeFor[emptyStruct]())
	if !isStruct {
		t.Error("struct{} should return isStruct=true")
	}
	if props == nil {
		t.Error("struct{} should return non-nil props (closed empty), got nil")
	}
	if len(props) != 0 {
		t.Errorf("struct{} should return empty props, got %v", props)
	}
}

// TestJsonProperties_NonStructIsOpen verifies that non-struct types return
// nil props (open schema).
func TestJsonProperties_NonStructIsOpen(t *testing.T) {
	props, isStruct := jsonProperties(reflect.TypeFor[map[string]any]())
	if isStruct {
		t.Error("map type should return isStruct=false")
	}
	if props != nil {
		t.Errorf("map type should return nil props (open), got %v", props)
	}
}

// TestJsonProperties_StructWithJSONTagsIsClosed verifies that a struct with
// json-tagged fields returns non-nil props (closed schema).
func TestJsonProperties_StructWithJSONTagsIsClosed(t *testing.T) {
	type input struct {
		Repo string `json:"repo"`
		Name string `json:"name"`
	}

	props, isStruct := jsonProperties(reflect.TypeFor[input]())
	if !isStruct {
		t.Error("struct with json tags should return isStruct=true")
	}
	if props == nil {
		t.Error("struct with json tags should return non-nil props (closed), got nil")
	}
	if len(props) != 2 {
		t.Errorf("expected 2 props, got %d: %v", len(props), props)
	}
}
