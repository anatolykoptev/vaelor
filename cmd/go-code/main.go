// go-code — Code intelligence MCP server.
//
// Provides multi-language AST parsing via tree-sitter, repository analysis,
// code comparison, and dependency graph visualization.
// Runs as HTTP MCP server (default) or stdio transport (--stdio flag).
//
// Tools: repo_analyze, file_parse, code_compare, dep_graph, symbol_search, call_trace,
// impact_analysis, dead_code, explore, code_health, code_graph, repo_search, code_search,
// semantic_search
package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/anatolykoptev/go-code/internal/designmd"
	"github.com/anatolykoptev/go-code/internal/embeddings"
	"github.com/anatolykoptev/go-mcpserver"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// version is set at build time via -ldflags "-X main.version=...".
// Falls back to "dev" for local `go run` / `go build` without flags.
var version = "dev"

const (
	serviceName = "go-code"
	toolCount   = 16

	defaultPort = "8897"

	// workspaceDirPerm is the permission mode for the workspace directory.
	workspaceDirPerm = 0o750
)

func main() {
	cfg := loadConfig()

	// Handle CLI subcommands before starting MCP server.
	if len(os.Args) >= 3 && os.Args[1] == "index-designs" {
		runIndexDesigns(cfg, os.Args[2])
		return
	}

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

	deps := registerTools(server, cfg)
	slog.Info("tools registered", slog.Int("count", toolCount))

	// Register webhook handler if GITHUB_WEBHOOK_SECRET is set
	if secret := os.Getenv("GITHUB_WEBHOOK_SECRET"); secret != "" {
		enabled := os.Getenv("REVIEW_POST_ENABLED") == "true"
		botUser := os.Getenv("REVIEW_BOT_USER")
		sink := func(event string, payload []byte) {
			DispatchGitHubEvent(event, payload, dispatchDeps{
				botUser: botUser,
				postReview: func(slug string, pr int) error {
					if !enabled {
						log.Printf("review_post disabled; would review %s#%d", slug, pr)
						return nil
					}
					ctx := context.Background()
					_, err := handleReviewPRPost(ctx, ReviewPRPostInput{Repo: slug, PR: pr}, deps)
					return err
				},
			})
		}
		http.Handle("/webhook/github", newGitHubWebhook(secret, sink))
		slog.Info("webhook registered", slog.String("path", "/webhook/github"))
	}

	hooks := mcpserver.MCPHooks{
		OnToolCall: func(_ context.Context, name string) {
			slog.Info("tool_call", slog.String("tool", name))
		},
		OnToolResult: func(_ context.Context, name string, dur time.Duration, isErr bool) {
			slog.Info("tool_result", slog.String("tool", name), slog.Duration("duration", dur), slog.Bool("error", isErr))
		},
	}

	if err := mcpserver.Run(server, mcpserver.Config{
		Name:                   serviceName,
		Version:                version,
		Port:                   cfg.Port,
		SessionTimeout:         10 * time.Minute,
		MCPLogger:              slog.Default(),
		MCPReceivingMiddleware: []mcp.Middleware{hooks.Middleware()},
		ToolTimeouts: map[string]time.Duration{
			"code_research": 90 * time.Second,
			"repo_analyze":  90 * time.Second,
			"code_compare":  90 * time.Second,
			"call_trace":    60 * time.Second,
			"code_health":   60 * time.Second,
		},
	}); err != nil {
		slog.Error("server failed", slog.Any("error", err))
	}
}

func runIndexDesigns(cfg Config, dir string) {
	embedURL := cfg.DesignEmbedURL
	embedModel := cfg.DesignEmbedModel
	if embedURL == "" {
		// Fallback to code embed config.
		embedURL = cfg.EmbedURL
		embedModel = cfg.EmbedModel
	}
	if embedURL == "" {
		slog.Error("DESIGN_EMBED_URL (or EMBED_URL) is required for indexing")
		os.Exit(1)
	}
	if cfg.DatabaseURL == "" {
		slog.Error("DATABASE_URL is required for indexing")
		os.Exit(1)
	}

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		slog.Error("database connect failed", slog.Any("error", err))
		os.Exit(1)
	}
	defer pool.Close()

	client := embeddings.NewClient(embedURL, embedModel)
	store := designmd.NewStore(pool)

	result, err := designmd.Index(context.Background(), dir, client, store)
	if err != nil {
		slog.Error("indexing failed", slog.Any("error", err))
		os.Exit(1)
	}

	slog.Info("indexing complete",
		slog.Int("brands", result.Brands),
		slog.Int("indexed", result.Indexed),
		slog.Int("skipped", result.Skipped),
	)
}
