package main

import (
	"context"
	"errors"
	"strings"
	"testing"

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
	ctx := context.Background()
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	srv.AddReceivingMiddleware(scrubErrorsMiddleware())
	mcp.AddTool(srv, &mcp.Tool{Name: "leak"}, func(context.Context, *mcp.CallToolRequest, leakIn) (*mcp.CallToolResult, any, error) {
		return nil, nil, handlerErr
	})
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
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "leak", Arguments: map[string]any{}})
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
