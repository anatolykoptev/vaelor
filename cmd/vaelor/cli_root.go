package main

import (
	"fmt"
	"os"

	"github.com/anatolykoptev/go-kit/cli"
	"github.com/spf13/cobra"
)

// newRootCmd wires the go-kit/cli generic cobra scaffold as the vaelor root
// command (ADR-3 strangler-fig). The default path — no subcommand — calls
// runMCPServe, which is the byte-identical legacy main() MCP serve body.
// Subcommands index-designs (migrated from raw os.Args), status, and init are
// registered here; each reuses existing domain seams instead of duplicating
// logic.
func newRootCmd(cfg Config) *cobra.Command {
	root := cli.NewRoot(cli.RootConfig{
		Use:     "vaelor",
		Short:   "Code intelligence MCP server & CLI",
		Long:    "vaelor runs as an MCP server by default. Subcommands provide standalone CLI tools (index-designs, status, init).",
		Version: version,
		Run: func(cmd *cobra.Command, args []string) {
			runMCPServe(cfg)
		},
	})

	// index-designs: primary cobra path. The legacy os.Args fallback in main()
	// still intercepts "vaelor index-designs <dir>" for one release; both call
	// the same runIndexDesigns(cfg, dir) so behavior is identical.
	cli.RegisterSubcommand(root, cli.SubcommandConfig{
		Name:  "index-designs",
		Short: "Index design markdown files into the design_embeddings table",
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) < 1 {
				fmt.Fprintln(os.Stderr, "index-designs: <dir> argument required")
				os.Exit(1)
			}
			runIndexDesigns(cfg, args[0])
		},
	})

	cli.RegisterSubcommand(root, newStatusSubcommand(cfg))
	cli.RegisterSubcommand(root, newInitSubcommand(cfg))
	cli.RegisterSubcommand(root, newSearchSubcommand(cfg))

	wipeCmd := cli.RegisterSubcommand(root, newWipeSubcommand(cfg))
	wipeCmd.Flags().Bool("dry-run", false, "print what would be deleted without executing any DELETE")
	wipeCmd.Flags().Bool("confirm", false, "non-interactive confirmation (skips the y/n prompt)")

	return root
}
