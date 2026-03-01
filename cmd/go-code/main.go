// go-code — Code intelligence MCP server.
//
// Provides multi-language AST parsing via tree-sitter, repository analysis,
// code comparison, and dependency graph visualization.
// Runs as HTTP MCP server (default) or stdio transport (--stdio flag).
//
// Tools: repo_analyze, file_parse, code_compare, dep_graph, symbol_search, call_trace, code_graph, repo_search
package main

import (
	"log/slog"
	"os"

	"github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// version is set at build time via -ldflags "-X main.version=...".
// Falls back to "dev" for local `go run` / `go build` without flags.
var version = "dev"

const (
	serviceName = "go-code"
	toolCount   = 8

	defaultPort = "8897"

	// workspaceDirPerm is the permission mode for the workspace directory.
	workspaceDirPerm = 0o750
)

func main() {
	cfg := loadConfig()

	if err := os.MkdirAll(cfg.WorkspaceDir, workspaceDirPerm); err != nil {
		slog.Error("failed to create workspace dir", slog.Any("error", err))
		os.Exit(1)
	}

	slog.Info("starting "+serviceName,
		slog.String("llm_model", cfg.LLMModel),
		slog.String("llm_url", cfg.LLMURL),
	)

	server := mcp.NewServer(&mcp.Implementation{
		Name:    serviceName,
		Version: version,
	}, nil)

	registerTools(server, cfg)
	slog.Info("tools registered", slog.Int("count", toolCount))

	if err := mcpserver.Run(server, mcpserver.Config{
		Name:    serviceName,
		Version: version,
		Port:    cfg.Port,
	}); err != nil {
		slog.Error("server failed", slog.Any("error", err))
	}
}
