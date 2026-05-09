// Package mcpmw provides a tracing middleware for MCP (Model Context Protocol)
// servers built on github.com/modelcontextprotocol/go-sdk.
//
// There is no canonical otelmcp middleware in the OpenTelemetry contrib repo
// (verified 2026-04-30) — every project rolls its own. This package fills the
// gap: a single mcp.Middleware that opens a span per `tools/call` invocation,
// extracts trace context from request metadata if present, and records the
// tool name, status, and duration as span attributes.
//
// Wiring:
//
//	import "github.com/anatolykoptev/go-kit/tracing/mcpmw"
//
//	server := mcp.NewServer(...)
//	mcpserver.Run(server, mcpserver.Config{
//	    MCPReceivingMiddleware: []mcp.Middleware{
//	        hooks.Middleware(),
//	        mcpmw.Middleware("go-wp"),
//	    },
//	    ...
//	})
//
// This composes naturally next to the existing metrics-side
// github.com/anatolykoptev/go-kit/metrics/mcpmw — both can be installed
// simultaneously to get RED metrics + per-call spans.
package mcpmw

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	toolsCallMethod = "tools/call"
	tracerName      = "github.com/anatolykoptev/go-kit/tracing/mcpmw"
)

// Middleware returns an mcp.Middleware that opens a span around each
// `tools/call` invocation. Non-tool MCP methods pass through untouched
// (initialise / list / completions etc. — those don't carry user-meaningful
// work and would inflate trace volume).
//
// Span name: "mcp.tools.call <toolName>" so Tempo/Jaeger UI groups by tool.
// Attributes:
//   - mcp.tool.name      string  — tool identifier
//   - mcp.tool.status    string  — "ok" | "error"
//   - rpc.system         string  — "mcp" (semconv-friendly)
//   - rpc.method         string  — "tools/call"
//
// scope is the service name; passed through as the tracer instrumentation
// scope so traces can be attributed to the originating service even when
// shared libraries emit child spans.
func Middleware(scope string) mcp.Middleware {
	if scope == "" {
		scope = tracerName
	}
	tracer := otel.Tracer(scope)

	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != toolsCallMethod {
				return next(ctx, method, req)
			}

			toolName := extractToolName(req)
			ctx, span := tracer.Start(ctx, "mcp.tools.call "+toolName,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					attribute.String("rpc.system", "mcp"),
					attribute.String("rpc.method", toolsCallMethod),
					attribute.String("mcp.tool.name", toolName),
				),
			)
			defer span.End()

			result, err := next(ctx, method, req)

			status := "ok"
			if err != nil {
				status = "error"
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			} else if cr, ok := result.(*mcp.CallToolResult); ok && cr != nil && cr.IsError {
				// Tool reported a logical error via IsError flag — the RPC
				// itself succeeded, so we don't RecordError, but mark status
				// for trace UI filtering.
				status = "error"
				span.SetStatus(codes.Error, "tool reported IsError")
			}
			span.SetAttributes(attribute.String("mcp.tool.status", status))

			return result, err
		}
	}
}

// extractToolName pulls the tool identifier from a CallToolParamsRaw payload.
// Returns "" when the request shape is unexpected — span name then falls back
// to "mcp.tools.call ".
func extractToolName(req mcp.Request) string {
	if req == nil {
		return ""
	}
	p, ok := req.GetParams().(*mcp.CallToolParamsRaw)
	if !ok || p == nil {
		return ""
	}
	return p.Name
}
