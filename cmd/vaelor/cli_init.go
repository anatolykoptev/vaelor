package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/anatolykoptev/go-kit/cli"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

// newInitSubcommand builds the "vaelor init" subcommand. It takes an optional
// <path> argument and triggers AutoIndex over that directory; when no path is
// given it falls back to autoIndexDirs(cfg) (AUTO_INDEX_DIRS, optionally
// translated). Reuses the existing embeddings.AutoIndex machinery and
// Pipeline — no domain logic is duplicated. Works without a running MCP server.
func newInitSubcommand(cfg Config) cli.SubcommandConfig {
	return cli.SubcommandConfig{
		Name:  "init",
		Short: "Trigger AutoIndex for a directory (or configured AUTO_INDEX_DIRS)",
		Long:  "Builds the embeddings pipeline and runs AutoIndex over <path> (or AUTO_INDEX_DIRS when no path is given). Works without a running MCP server.",
		Run: func(cmd *cobra.Command, args []string) {
			runInit(cfg, args)
		},
	}
}

func runInit(cfg Config, args []string) {
	if cfg.DatabaseURL == "" {
		fmt.Fprintln(os.Stderr, "init: DATABASE_URL is required for indexing")
		os.Exit(1)
	}
	if cfg.EmbedURL == "" {
		fmt.Fprintln(os.Stderr, "init: EMBED_URL is required for indexing")
		os.Exit(1)
	}

	dirs := autoIndexDirs(cfg)
	if len(args) > 0 {
		dirs = []string{args[0]}
	}
	if len(dirs) == 0 {
		fmt.Fprintln(os.Stderr, "init: no directory given and AUTO_INDEX_DIRS is empty")
		os.Exit(1)
	}

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init: database connect failed: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	ec, err := newCodeEmbedder(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init: embed client failed: %v\n", err)
		os.Exit(1)
	}
	store := embeddings.NewStore(pool)
	pipeline := embeddings.NewPipeline(ec, store, cfg.EmbedModel)

	opts := embeddings.DefaultAutoIndexOpts()
	opts.Concurrency = cfg.AutoIndexConcurrency
	opts.RetryMax = cfg.AutoIndexRetryMax
	opts.RetryBase = cfg.AutoIndexRetryBase

	embeddings.AutoIndex(pipeline, dirs, codegraph.GraphNameFor, opts)
	slog.Info("init: autoindex complete", slog.Int("dirs", len(dirs)))
}
