// Package cli provides a thin generic cobra scaffold for building CLI entry
// points: a root command with common flags (--version, --config) and a
// subcommand registration helper. No application-specific defaults are baked
// in — all parameters are supplied by the caller.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// RootConfig holds parameters for building the root command. All fields are
// optional except Use.
type RootConfig struct {
	Use     string // command usage line, e.g. "myapp"
	Short   string // one-line description
	Long    string // long description
	Version string // value reported by --version; empty disables the flag
	// ConfigFlagName is the name of the config-file flag; defaults to "config".
	ConfigFlagName string
	// Run is the root command's Run function; may be nil for command groups.
	Run func(cmd *cobra.Command, args []string)
}

// NewRoot builds a generic cobra root command with --version (when Version is
// set) and a --config flag, ready to have subcommands attached.
func NewRoot(cfg RootConfig) *cobra.Command {
	root := &cobra.Command{
		Use:     cfg.Use,
		Short:   cfg.Short,
		Long:    cfg.Long,
		Version: cfg.Version,
		Run:     cfg.Run,
	}
	name := cfg.ConfigFlagName
	if name == "" {
		name = "config"
	}
	root.Flags().String(name, "", "path to config file")
	return root
}

// SubcommandConfig describes a subcommand to register under a root command.
type SubcommandConfig struct {
	Name  string
	Short string
	Long  string
	Run   func(cmd *cobra.Command, args []string)
}

// RegisterSubcommand attaches a subcommand described by sub to root and
// returns the created command so callers can add flags to it.
func RegisterSubcommand(root *cobra.Command, sub SubcommandConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   sub.Name,
		Short: sub.Short,
		Long:  sub.Long,
		Run:   sub.Run,
	}
	root.AddCommand(cmd)
	return cmd
}

// PrintMCPConfig prints a 'claude mcp add' config snippet for the given server
// name, url and transport to stdout.
func PrintMCPConfig(name, url, transport string) {
	fmt.Fprintf(os.Stdout, "claude mcp add %s --transport %s %s\n", name, transport, url)
}
