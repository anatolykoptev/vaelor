package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestNewRootHasCommonFlags(t *testing.T) {
	root := NewRoot(RootConfig{Use: "myapp", Version: "1.2.3"})

	// The --config flag is added explicitly and is available immediately.
	if got := root.Flags().Lookup("config"); got == nil {
		t.Fatal("root command missing --config flag")
	}

	// cobra adds the --version flag lazily during Execute; verify it by
	// running the root with --version and checking the printed version.
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetArgs([]string{"--version"})
	if err := root.Execute(); err != nil {
		t.Fatalf("root.Execute --version: %v", err)
	}
	if !strings.Contains(buf.String(), "1.2.3") {
		t.Fatalf("--version output = %q, missing version %q", buf.String(), "1.2.3")
	}
}

func TestRegisterSubcommand(t *testing.T) {
	root := NewRoot(RootConfig{Use: "myapp"})

	ran := false
	sub := RegisterSubcommand(root, SubcommandConfig{
		Name:  "greet",
		Short: "greet someone",
		Long:  "greet someone in a friendly way",
		Run: func(cmd *cobra.Command, args []string) {
			ran = true
		},
	})

	// The subcommand must appear under the root.
	found := false
	for _, c := range root.Commands() {
		if c.Name() == "greet" {
			found = true
		}
	}
	if !found {
		t.Fatal("registered subcommand not present under root")
	}

	// The subcommand must be runnable: dispatch via the root.
	root.SetArgs([]string{"greet"})
	if err := root.Execute(); err != nil {
		t.Fatalf("root.Execute: %v", err)
	}
	if !ran {
		t.Fatal("subcommand Run func was not invoked")
	}
	_ = sub // keep reference; returned command is the registered one
}

func TestPrintMCPConfig(t *testing.T) {
	// Capture stdout so we exercise the real PrintMCPConfig code path.
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	PrintMCPConfig("myserver", "https://example.com/mcp", "sse")

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("io.Copy: %v", err)
	}
	out := buf.String()

	for _, want := range []string{"claude mcp add", "myserver", "https://example.com/mcp", "sse"} {
		if !strings.Contains(out, want) {
			t.Errorf("PrintMCPConfig output = %q, missing %q", out, want)
		}
	}
}
