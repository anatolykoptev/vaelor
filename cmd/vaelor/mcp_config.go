package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	kitmetrics "github.com/anatolykoptev/go-kit/metrics"
	"github.com/anatolykoptev/go-kit/tracing/httpmw"
	"github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpConfigDeps is everything buildMCPConfig needs from runMCPServe.
type mcpConfigDeps struct {
	ctx      context.Context
	port     string
	stdio    bool
	reg      *kitmetrics.Registry
	hooks    mcpserver.MCPHooks
	routes   func(*http.ServeMux)
	timeouts map[string]time.Duration
}

// buildMCPConfig assembles the mcpserver.Config the production server runs
// with, INCLUDING the env-driven HTTP bearer gate and the session policy. It is
// the single place both runMCPServe and the tests go through, so the wiring
// (gate first in the middleware chain, boot refusal on a bad auth config) is
// observable by tests rather than only by reading main.
func buildMCPConfig(d mcpConfigDeps) (mcpserver.Config, error) {
	gate, err := newHTTPAuthFromEnv()
	if err != nil {
		return mcpserver.Config{}, fmt.Errorf("http auth misconfigured: %w", err)
	}
	if gate != nil {
		slog.Info("http auth active", slog.String("mode", gate.mode))
	}

	cfg := mcpserver.Config{
		Name:                       serviceName,
		Version:                    version,
		Port:                       d.port,
		Transport:                  mcpTransport(d.stdio),
		Context:                    d.ctx,
		SchemaCache:                mcp.NewSchemaCache(),
		DisableLocalhostProtection: true,
		Logger:                     slog.Default(), // preserve slogh wrapper; mcpserver would otherwise replace it
		MCPLogger:                  slog.Default(),
		MCPReceivingMiddleware:     receivingMiddleware(d.reg, d.hooks),
		Middleware: []mcpserver.Middleware{
			gate.Middleware(), // first of ours: gates every non-exempt path before tracing/handlers
			func(next http.Handler) http.Handler { return httpmw.Handler(serviceName, next) },
		},
		RESTBridge:   true,
		Routes:       d.routes,
		LogSkipPaths: []string{"/health", "/health/live", "/health/ready", "/metrics"}, //nolint:goconst // route paths, not worth a shared constant
		ToolTimeouts: d.timeouts,
		// SSE (text/event-stream) mode. Long tool calls (code_research, debug_investigate,
		// code_graph, etc.) emit no bytes until they finish; in stateless mode the
		// server can't send ping requests, so a client/proxy idle-timeout would
		// abandon the call while the server keeps working. Instead,
		// ToolKeepaliveInterval emits a progress notification on the request
		// stream every 10s to keep it warm. (The old KeepAlive:30s ping was
		// inert in stateless mode — rejected, then closed the session at 30s and
		// truncated the response; removed.) Caddy forces HTTP/1.1 to the
		// upstream, fixing the h2 stream-reset that originally motivated JSON.
		JSONResponse:          false,
		ToolKeepaliveInterval: 10 * time.Second,
	}
	// Stateless on purpose (the go-mcpserver default; Stateless is left nil).
	// GET /mcp answers 405 + Allow: POST, which rmcp-based clients treat as "no
	// standalone stream". The premise of #912/#938 (stateful mode + session TTL to
	// stop the rmcp SSE error loop) was disproved: the loop rate never changed,
	// and sessions only introduced "session expired" errors.
	return cfg, nil
}
