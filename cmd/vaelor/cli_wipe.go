package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/anatolykoptev/go-kit/cli"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"
)

// newWipeSubcommand builds the "vaelor wipe" subcommand (ADR-8). It deletes
// ALL indexed data for a repo — both code_embeddings and code_repo_state rows
// — via Store.WipeRepo, an atomic dual-table DELETE. Deletion is irreversible,
// so the command requires either an interactive "yes" confirmation (typing the
// full word, not just "y") or the --confirm flag. --dry-run prints what would
// be deleted without executing any DELETE. Audit-logs the operation to slog
// before executing. Exits non-zero with a clear message on DB unreachable or
// aborted confirmation; never panics.
//
// The --dry-run and --confirm flags are attached to the returned command in
// cli_root.go (RegisterSubcommand returns the *cobra.Command).
func newWipeSubcommand(cfg Config) cli.SubcommandConfig {
	return cli.SubcommandConfig{
		Name:  "wipe",
		Short: "Delete all indexed data for a repo (irreversible)",
		Long:  "Deletes code_embeddings + code_repo_state rows for <repo> (owner/repo). Requires interactive yes confirmation or --confirm. Use --dry-run to preview without deleting.",
		Run: func(cmd *cobra.Command, args []string) {
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			confirm, _ := cmd.Flags().GetBool("confirm")
			runWipe(cfg, args, dryRun, confirm)
		},
	}
}

func runWipe(cfg Config, args []string, dryRun, confirm bool) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "wipe: <repo> argument required (owner/repo format)")
		fmt.Fprintln(os.Stderr, "usage: vaelor wipe <owner/repo> [--dry-run] [--confirm]")
		os.Exit(2)
	}
	repoKey := args[0]

	fmt.Printf("repo_key: %s\n", repoKey)
	fmt.Println("tables: code_embeddings, code_repo_state")

	if dryRun {
		fmt.Println("dry-run: no rows will be deleted")
		return
	}

	if !confirm {
		fmt.Print("This will IRREVERSIBLY delete all indexed data for this repo. Type yes to proceed: ")
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		if strings.TrimSpace(line) != "yes" {
			fmt.Fprintln(os.Stderr, "wipe: aborted (confirmation did not match 'yes')")
			os.Exit(1)
		}
	}

	if cfg.DatabaseURL == "" {
		fmt.Fprintln(os.Stderr, "wipe: DATABASE_URL is not set — cannot reach the index store")
		os.Exit(1)
	}

	slog.Info("wipe: starting",
		slog.String("repo_key", repoKey),
		slog.String("op", "wipe_repo"),
		slog.Bool("dry_run", dryRun))

	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wipe: database connect failed: %v\n", err)
		os.Exit(1)
	}
	defer pool.Close()

	var one int
	if err := pool.QueryRow(context.Background(), "SELECT 1").Scan(&one); err != nil {
		fmt.Fprintf(os.Stderr, "wipe: database unreachable: %v\n", err)
		os.Exit(1)
	}

	store := embeddings.NewStore(pool)
	if err := store.WipeRepo(context.Background(), repoKey); err != nil {
		fmt.Fprintf(os.Stderr, "wipe: delete failed: %v\n", err)
		os.Exit(1)
	}

	slog.Info("wipe: complete", slog.String("repo_key", repoKey))
	fmt.Printf("wipe: deleted all indexed data for %s\n", repoKey)
}
