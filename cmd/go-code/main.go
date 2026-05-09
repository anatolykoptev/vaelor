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
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/designmd"
	"github.com/anatolykoptev/go-kit/env"
	kitmetrics "github.com/anatolykoptev/go-kit/metrics"
	"github.com/anatolykoptev/go-kit/metrics/mcpmw"
	"github.com/anatolykoptev/go-kit/tracing"
	"github.com/anatolykoptev/go-kit/tracing/httpmw"
	tracemcpmw "github.com/anatolykoptev/go-kit/tracing/mcpmw"
	"github.com/anatolykoptev/go-kit/tracing/slogh"
	"github.com/anatolykoptev/go-mcpserver"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/attribute"
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
	cfg, err := loadConfig()
	if err != nil {
		slog.Error("failed to load config", slog.Any("error", err))
		os.Exit(1)
	}

	// Wire the analyze package's fusion config + publish gocode_analyze_fusion_mode
	// gauge before any analyze call path runs. Default minmax = byte-identical
	// legacy; rrf is opt-in via ANALYZE_RANK_FUSION_MODE=rrf.
	analyze.SetFusionConfig(analyze.FusionConfig{
		Mode:           cfg.AnalyzeRankFusionMode,
		WeightBM25:     cfg.AnalyzeRankWeightBM25,
		WeightPageRank: cfg.AnalyzeRankWeightPageRank,
		WeightSeed:     cfg.AnalyzeRankWeightSeed,
	})

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

	// Lifecycle context: cancelled on SIGINT/SIGTERM. Passed to mcpserver.Run
	// (so it owns shutdown) and to EagerWarmRepos (so in-flight `go build`
	// subprocesses are cancelled on graceful shutdown instead of running to
	// the per-repo 5-min timeout).
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTracing, err := tracing.Setup(ctx, serviceName,
		tracing.WithSampleRatio(1.0),
		tracing.WithAttributes(attribute.String("version", version)),
	)
	if err != nil {
		slog.Warn("otel tracing setup failed; continuing without traces", "err", err)
	} else {
		defer func() {
			sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer scancel()
			_ = shutdownTracing(sctx)
		}()
	}

	// Wrap the default slog handler with slogh.Handler so every log record
	// emitted with a context carrying an active span gets trace_id + span_id
	// attrs. Enables log↔trace correlation in Jaeger without changing call sites.
	slog.SetDefault(slog.New(slogh.NewHandler(slog.NewTextHandler(os.Stderr, nil))))

	reg := kitmetrics.NewPrometheusRegistry("gocode")
	startPrometheusScrape(ctx, slog.Default())

	server := mcp.NewServer(&mcp.Implementation{
		Name:    serviceName,
		Version: version,
	}, nil)

	deps := registerTools(server, cfg)
	slog.Info("tools registered", slog.Int("count", toolCount))

	// Eager GOCACHE pre-warm for AUTO_INDEX_DIRS Go repos. Runs in a
	// background goroutine so it does not block MCP serve. Eliminates the
	// cold-cache `tier: basic` window on the first call_trace per repo.
	// Default-on when AUTO_INDEX_DIRS is set (the explicit indexing signal);
	// set EAGER_WARM=false to disable. Accepts "false", "0", "f", "FALSE",
	// etc. via strconv.ParseBool; invalid values warn and default to true.
	eager := true
	if v := os.Getenv("EAGER_WARM"); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			eager = parsed
		} else {
			slog.Warn("invalid EAGER_WARM value, defaulting to true", "value", v)
		}
	}
	if len(cfg.AutoIndexDirs) > 0 && eager {
		go callgraph.EagerWarmRepos(ctx, cfg.AutoIndexDirs)
	}

	// Webhook handler registered via mcpserver.Config.Routes below so it shares
	// the server's mux (http.DefaultServeMux is unused by mcpserver).
	var webhookHandler http.Handler
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
					_, err := handleReviewPR(ctx, ReviewPRInput{Repo: slug, PR: pr, Event: "COMMENT"}, deps, nil)
					return err
				},
				postPushReview: func(slug, before, after string) error {
					if !enabled {
						log.Printf("review_post disabled; would review push %s %s..%s", slug, before[:8], after[:8])
						return nil
					}
					return handlePushReview(slug, before, after, deps)
				},
			})
		}
		handler := newGitHubWebhook(secret, sink)
		webhookHandler = handler
		slog.Info("webhook registered", slog.String("path", "/webhook/github"))
	}

	// Build the combined HTTP routes: webhook (conditional) + resolve (conditional).
	// Wrap mux in httpmw.Mux so each Handle call auto-registers code.* OTEL
	// attrs via reflect — no manual RegisterRoute calls needed.
	resolveHosts := cfg.SourcemapAllowedHosts
	combinedRoutes := func(mux *http.ServeMux) {
		wrapped := &httpmw.Mux{ServeMux: mux}
		if webhookHandler != nil {
			wrapped.Handle("POST /webhook/github", webhookHandler)
		}
		if len(resolveHosts) > 0 && resolveFrameResolver != nil {
			wrapped.Handle("POST /resolve", resolveHTTPHandler(resolveHosts, resolveFrameResolver))
			slog.Info("resolve endpoint registered", slog.String("path", "/resolve"))
		}
	}

	hooks := mcpserver.MCPHooks{
		OnToolCall: func(ctx context.Context, name string) {
			slog.InfoContext(ctx, "tool_call", slog.String("tool", name))
		},
		OnToolResult: func(ctx context.Context, name string, dur time.Duration, isErr bool) {
			slog.InfoContext(ctx, "tool_result", slog.String("tool", name), slog.Duration("duration", dur), slog.Bool("error", isErr))
		},
	}

	if err := mcpserver.Run(server, mcpserver.Config{
		Name:                   serviceName,
		Version:                version,
		Port:                   cfg.Port,
		Context:                ctx,
		SessionTimeout:         10 * time.Minute,
		Logger:                 slog.Default(), // preserve slogh wrapper; mcpserver would otherwise replace it
		MCPLogger:              slog.Default(),
		MCPReceivingMiddleware: []mcp.Middleware{tracemcpmw.Middleware(serviceName), hooks.Middleware(), mcpmw.Middleware(reg, "tool")},
		Middleware:             []mcpserver.Middleware{func(next http.Handler) http.Handler { return httpmw.Handler(serviceName, next) }},
		RESTBridge:             true,
		Routes:                 combinedRoutes,
		LogSkipPaths:           []string{"/health", "/health/live", "/health/ready", "/metrics"},
		ToolTimeouts: map[string]time.Duration{
			"code_research":     90 * time.Second,
			"repo_analyze":      90 * time.Second,
			"code_compare":      90 * time.Second,
			"call_trace":        60 * time.Second,
			"code_health":       60 * time.Second,
			"debug_investigate": 5 * time.Minute,
		},
	}); err != nil {
		slog.Error("server failed", slog.Any("error", err))
	}
}

// startPrometheusScrape runs an HTTP server exposing /metrics on PROM_PORT
// (default 9897 = MCP_PORT+1000) for prometheus scrape. Separate port avoids
// BearerAuth on scrape traffic; bound to all interfaces for container scrape.
func startPrometheusScrape(ctx context.Context, logger *slog.Logger) {
	promPort := env.Str("PROM_PORT", "9897")
	mux := http.NewServeMux()
	mux.Handle("/metrics", kitmetrics.MetricsHandler())
	srv := &http.Server{
		Addr:              ":" + promPort,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("prometheus scrape endpoint", slog.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("prom endpoint", slog.Any("error", err))
		}
	}()
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
}

func runIndexDesigns(cfg Config, dir string) {
	// Fallback to code embed config when design-specific URL is unset (legacy
	// behavior). The factory uses cfg.DesignEmbedURL / cfg.DesignEmbedModel, so
	// patch cfg in place for the CLI path only.
	if cfg.DesignEmbedURL == "" {
		cfg.DesignEmbedURL = cfg.EmbedURL
		cfg.DesignEmbedModel = cfg.EmbedModel
	}
	if cfg.DesignEmbedURL == "" {
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

	client, err := newDesignEmbedder(cfg)
	if err != nil {
		slog.Error("embed client failed", slog.Any("error", err))
		os.Exit(1)
	}
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
