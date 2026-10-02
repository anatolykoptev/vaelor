package main

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/anatolykoptev/vaelor/internal/credscrub"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// scrubErrorsMiddleware masks credentials (URL userinfo, token-shaped strings,
// Authorization values) in everything that leaves a tool call: every
// CallToolResult (error results are what tool handler errors become) and
// protocol-level errors returned by the method handler. It is the last line of
// defence behind per-source sanitisation such as ingest.sanitizeGitOutput.
func scrubErrorsMiddleware() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			res, err := next(ctx, method, req)
			if err != nil {
				return res, scrubError(err)
			}
			if ctr, ok := res.(*mcp.CallToolResult); ok && ctr != nil {
				scrubToolResult(ctr)
			}
			return res, nil
		}
	}
}

// scrubbedError carries the masked message while keeping the original in the
// Unwrap chain so errors.Is/As (e.g. context.DeadlineExceeded) still work.
type scrubbedError struct {
	msg   string
	cause error
}

func (e *scrubbedError) Error() string { return e.msg }
func (e *scrubbedError) Unwrap() error { return e.cause }

func scrubError(err error) error {
	var wire *jsonrpc.Error
	if errors.As(err, &wire) {
		if masked := credscrub.Scrub(wire.Message); masked != wire.Message {
			cp := *wire
			cp.Message = masked
			return &cp
		}
		return err
	}
	if masked := credscrub.Scrub(err.Error()); masked != err.Error() {
		return &scrubbedError{msg: masked, cause: err}
	}
	return err
}

func scrubToolResult(r *mcp.CallToolResult) {
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			tc.Text = credscrub.Scrub(tc.Text)
		}
	}
	if r.StructuredContent == nil {
		return
	}
	// Fail closed: if the structured payload cannot be re-encoded after
	// masking, drop it rather than return it unscrubbed.
	raw, err := json.Marshal(r.StructuredContent)
	if err != nil {
		r.StructuredContent = nil
		return
	}
	var v any
	if json.Unmarshal([]byte(credscrub.Scrub(string(raw))), &v) != nil {
		r.StructuredContent = nil
		return
	}
	r.StructuredContent = v
}
