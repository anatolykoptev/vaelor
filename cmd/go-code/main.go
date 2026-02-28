// go-code — Code intelligence MCP server.
//
// Provides multi-language AST parsing via tree-sitter, repository analysis,
// code comparison, and dependency graph visualization.
// Runs as HTTP MCP server (default) or stdio transport (--stdio flag).
//
// Tools: repo_analyze, file_parse, code_compare, dep_graph, symbol_search
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// version is set at build time via -ldflags "-X main.version=...".
// Falls back to "dev" for local `go run` / `go build` without flags.
var version = "dev"

const (
	serviceName = "go-code"
	toolCount   = 5

	defaultPort = "8897"

	// Server timeouts.
	readTimeout     = 30 * time.Second
	writeTimeout    = 120 * time.Second
	shutdownTimeout = 10 * time.Second

	// workspaceDirPerm is the permission mode for the workspace directory.
	workspaceDirPerm = 0o750
)

func isStdio() bool {
	for _, arg := range os.Args[1:] {
		if arg == "--stdio" {
			return true
		}
	}
	return false
}

func main() {
	stdio := isStdio()

	logWriter := os.Stdout
	if stdio {
		logWriter = os.Stderr
	}
	logger := slog.New(slog.NewTextHandler(logWriter, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	sigCtx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg := loadConfig()

	if err := os.MkdirAll(cfg.WorkspaceDir, workspaceDirPerm); err != nil {
		logger.Error("failed to create workspace dir", slog.Any("error", err))
		os.Exit(1) //nolint:gocritic // defer cancel() is fine to skip on startup failure
	}

	logger.Info("starting "+serviceName,
		slog.Bool("stdio", stdio),
		slog.String("llm_model", cfg.LLMModel),
		slog.String("llm_url", cfg.LLMURL),
	)

	server := mcp.NewServer(&mcp.Implementation{
		Name:    serviceName,
		Version: version,
	}, nil)

	registerTools(server, cfg)
	logger.Info("tools registered", slog.Int("count", toolCount))

	if stdio {
		logger.Info("running in stdio mode")
		if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			logger.Error("stdio server failed", slog.Any("error", err))
			os.Exit(1) //nolint:gocritic // explicit cleanup called above; os.Exit after defer is intentional in stdio mode
		}
		return
	}

	// HTTP mode (default).
	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{
		Stateless: true,
	})

	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)
	mux.Handle("/mcp/", handler)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"` + serviceName + `","version":"` + version + `"}`))
	})

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      recoveryMiddleware(mux, logger),
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", slog.String("addr", srv.Addr))
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", slog.Any("error", err))
			os.Exit(1)
		}
	case <-sigCtx.Done():
		logger.Info("shutting down")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown error", slog.Any("error", err))
		}
		logger.Info("stopped")
	}
}

func recoveryMiddleware(next http.Handler, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rv := recover(); rv != nil {
				logger.Error("panic recovered",
					slog.Any("panic", rv),
					slog.String("path", r.URL.Path),
				)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
