package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	kitmetrics "github.com/anatolykoptev/go-kit/metrics"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	sentinelTok  = "ghs_SENTINELBOUNDARY123"
	sentinelPass = "SENTINELPASS456"
)

type leakIn struct{}

// callLeakyTool registers a tool whose handler returns err, wraps the server
// with scrubErrorsMiddleware, and returns the text of the error result.
func callLeakyTool(t *testing.T, handlerErr error) string {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	srv.AddReceivingMiddleware(scrubErrorsMiddleware())
	mcp.AddTool(srv, &mcp.Tool{Name: "leak"}, func(context.Context, *mcp.CallToolRequest, leakIn) (*mcp.CallToolResult, any, error) {
		return nil, nil, handlerErr
	})
	return callTool(t, srv, "leak")
}

// callTool connects an in-memory client to srv, calls the named tool and
// returns the text of its error result (or the protocol error text).
func callTool(t *testing.T, srv *mcp.Server, name string) string {
	t.Helper()
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
	if err != nil {
		return err.Error()
	}
	if !res.IsError {
		t.Fatalf("expected error result, got %+v", res)
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

func TestScrubErrorsMiddleware_MasksToolErrors(t *testing.T) {
	t.Parallel()
	leak := errors.New("clone: fatal: could not read Password for 'https://" + sentinelTok +
		"@github.com': terminal prompts disabled; also https://user:" + sentinelPass + "@host.example/x.git")
	got := callLeakyTool(t, leak)
	for _, s := range []string{sentinelTok, sentinelPass} {
		if strings.Contains(got, s) {
			t.Errorf("sentinel %q leaked through the MCP boundary: %q", s, got)
		}
	}
	if !strings.Contains(got, "terminal prompts disabled") {
		t.Errorf("non-secret context was lost: %q", got)
	}
}

// TestProductionMiddlewareChain_MasksToolErrors builds the server with the
// exact chain main installs (receivingMiddleware) and a leaky tool registered
// through the production addTool; deleting scrubErrorsMiddleware from that
// chain must turn this RED.
func TestProductionMiddlewareChain_MasksToolErrors(t *testing.T) {
	srv := mcpserver.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, mcpserver.Config{SchemaCache: mcp.NewSchemaCache()})
	srv.AddReceivingMiddleware(receivingMiddleware(kitmetrics.NewPrometheusRegistry("scrubtest"), mcpserver.MCPHooks{})...)
	addTool(srv, &mcp.Tool{Name: "leak"}, func(context.Context, *mcp.CallToolRequest, leakIn) (*mcp.CallToolResult, error) {
		return nil, errors.New("clone: fatal: could not read Password for 'https://" + sentinelTok + "@github.com'")
	})
	got := callTool(t, srv, "leak")
	if strings.Contains(got, sentinelTok) {
		t.Errorf("sentinel leaked through the production middleware chain: %q", got)
	}
	if !strings.Contains(got, "terminal prompts") && !strings.Contains(got, "could not read Password") {
		t.Errorf("expected the (masked) error text, got %q", got)
	}
}

func TestScrubToolResult_StructuredAndSuccess(t *testing.T) {
	t.Parallel()
	r := &mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: "ok " + sentinelTok}},
		StructuredContent: map[string]any{"u": "https://x:" + sentinelPass + "@h/y"},
	}
	scrubToolResult(r)
	if strings.Contains(r.Content[0].(*mcp.TextContent).Text, sentinelTok) {
		t.Error("text content not scrubbed")
	}
	raw, _ := json.Marshal(r.StructuredContent)
	if strings.Contains(string(raw), sentinelPass) {
		t.Errorf("structured content not scrubbed: %s", raw)
	}
	bad := &mcp.CallToolResult{StructuredContent: make(chan int)}
	scrubToolResult(bad)
	if bad.StructuredContent != nil {
		t.Error("unencodable structured content must be dropped (fail closed)")
	}
}

func TestScrubError_ProtocolErrors(t *testing.T) {
	t.Parallel()
	wire := &jsonrpc.Error{Code: -32603, Message: "boom https://" + sentinelTok + "@github.com"}
	got := scrubError(wire)
	if strings.Contains(got.Error(), sentinelTok) {
		t.Errorf("jsonrpc error leaked: %v", got)
	}
	var out *jsonrpc.Error
	if !errors.As(got, &out) || out.Code != -32603 {
		t.Errorf("jsonrpc code not preserved: %v", got)
	}

	plain := scrubError(errors.Join(context.DeadlineExceeded, errors.New(sentinelTok)))
	if strings.Contains(plain.Error(), sentinelTok) {
		t.Errorf("plain error leaked: %v", plain)
	}
	if !errors.Is(plain, context.DeadlineExceeded) {
		t.Errorf("errors.Is chain lost")
	}
}
