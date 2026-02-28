package main

import (
	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/llm"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerTools registers all MCP tool handlers on the server.
// Each tool has its own file: tool_<name>.go
func registerTools(server *mcp.Server, cfg Config) {
	deps := analyze.Deps{
		LLM: llm.NewClient(llm.Config{
			BaseURL: cfg.LLMURL,
			APIKey:  cfg.LLMAPIKey,
			Model:   cfg.LLMModel,
		}),
		MaxFileBytes: cfg.MaxFileBytes,
		GithubToken:  cfg.GithubToken,
		WorkspaceDir: cfg.WorkspaceDir,
	}

	registerRepoAnalyze(server, cfg, deps)
	registerFileParse(server, cfg)
	registerCodeCompare(server, cfg)
	registerDepGraph(server, cfg, deps)
	registerSymbolSearch(server, cfg, deps)
}
