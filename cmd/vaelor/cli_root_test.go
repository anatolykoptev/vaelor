package main

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/env"
)

// TestCLI_AllSubcommandsRegistered guards against the regression introduced
// in PR #543 where the "search" subcommand registration was accidentally
// replaced by the "wipe" subcommand instead of being added alongside it.
// The INTEGRATION_VERIFICATION.md smoke test confirmed "search" worked in
// PR #541, but no automated test caught the removal.
//
// This test enumerates the subcommands that MUST be registered on the vaelor
// root command and verifies each is present. Adding a new subcommand?
// Append its name to expectedSubcommands.
func TestCLI_AllSubcommandsRegistered(t *testing.T) {
	t.Parallel()

	cfg := Config{
		DatabaseURL: env.Str("DATABASE_URL", ""),
		EmbedURL:    env.Str("EMBED_URL", ""),
	}
	root := newRootCmd(cfg)

	expectedSubcommands := []string{
		"index-designs",
		"status",
		"init",
		"search",
		"wipe",
	}

	for _, name := range expectedSubcommands {
		cmd, _, err := root.Find([]string{name})
		if err != nil {
			t.Errorf("subcommand %q not found on root: %v", name, err)
			continue
		}
		if cmd == nil {
			t.Errorf("subcommand %q returned nil command", name)
			continue
		}
		if cmd.Name() != name {
			t.Errorf("subcommand %q resolved to %q", name, cmd.Name())
		}
	}
}

// TestCLI_SearchHelp verifies the search subcommand renders help without
// "unknown command" — the exact symptom of the PR #543 regression.
func TestCLI_SearchHelp(t *testing.T) {
	t.Parallel()

	cfg := Config{}
	root := newRootCmd(cfg)

	cmd, _, err := root.Find([]string{"search"})
	if err != nil {
		t.Fatalf("search subcommand not found: %v", err)
	}
	if cmd == nil {
		t.Fatal("search subcommand is nil")
	}

	// The search subcommand's Long help must mention "semantic" — it's the
	// semantic_search CLI path. If the subcommand is missing, Find returns
	// the root command whose Long help does NOT mention "semantic".
	if !strings.Contains(cmd.Long, "semantic") {
		t.Errorf("search subcommand Long help does not mention 'semantic'; got: %s", cmd.Long)
	}
}
