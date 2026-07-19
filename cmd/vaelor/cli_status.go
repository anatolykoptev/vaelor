package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/anatolykoptev/go-kit/cli"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

// newStatusSubcommand builds the "vaelor status" subcommand. It connects to
// the DB directly via DATABASE_URL (no running MCP server required), lists
// every repo known to code_repo_state, and prints indexed_at + stored
// embed_model per repo plus the watcher state. Exits non-zero with a clear
// message when DATABASE_URL is empty or the DB is unreachable (never panics).
func newStatusSubcommand(cfg Config) cli.SubcommandConfig {
	return cli.SubcommandConfig{
		Name:  "status",
		Short: "Show indexed repo state and watcher status",
		Long:  "Connects to the configured database and prints a table of indexed repos (repo_key, indexed_at, embed_model) plus watcher state. Works without a running MCP server.",
		Run: func(cmd *cobra.Command, args []string) {
			runStatus(cfg)
		},
	}
}

func runStatus(cfg Config) {
	if cfg.DatabaseURL == "" {
		fmt.Fprintln(os.Stderr, "status: DATABASE_URL is not set — cannot query repo state")
		os.Exit(1)
	}
	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "status: database connect failed: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Ping so we fail fast with a clear message instead of a per-repo timeout.
	var one int
	if err := pool.QueryRow(context.Background(), "SELECT 1").Scan(&one); err != nil {
		fmt.Fprintf(os.Stderr, "status: database unreachable: %v\n", err)
		os.Exit(1)
	}

	store := embeddings.NewStore(pool)
	ctx := context.Background()
	keys, err := store.ListRepoKeys(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "status: ListRepoKeys failed: %v\n", err)
		os.Exit(1)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "REPO_KEY\tINDEXED_AT\tEMBED_MODEL")
	for _, key := range keys {
		indexedAt := store.GetIndexedAt(ctx, key)
		model := store.GetStoredModel(ctx, key)
		ts := "never"
		if !indexedAt.IsZero() {
			ts = indexedAt.Format(time.RFC3339)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", key, ts, model)
	}
	w.Flush()

	// Watcher wiring is Phase 6; until then report the env signal or disabled.
	watcher := "disabled"
	if v := os.Getenv("WATCH_ENABLED"); v != "" {
		watcher = v
	}
	fmt.Printf("\nwatcher: %s\n", watcher)
}
