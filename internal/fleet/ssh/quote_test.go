package ssh

import (
	"os/exec"
	"strings"
	"testing"
)

func TestShellQuote(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"docker", "docker"},
		{"ps", "ps"},
		{"--no-trunc", "--no-trunc"},
		{"alpha_123.path-x", "alpha_123.path-x"},
		{"--format={{json .}}", `'--format={{json .}}'`}, // space inside braces
		{"a b", `'a b'`},                                 // plain space
		{"it's", `'it'\''s'`},                            // embedded single quote
		{"", "''"},
		{"$(whoami)", `'$(whoami)'`}, // metachars
	}
	for _, c := range cases {
		got := shellQuote(c.in)
		if got != c.want {
			t.Errorf("shellQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestShellQuoteRoundTrip verifies that shellQuote output survives a real
// shell tokenisation round-trip: args quoted individually and joined with
// spaces are split back to the original args by the shell.
// This is the exact failure mode fixed in this PR: "--format={{json .}}"
// contains a space that causes sshd's remote shell to split it into two args.
func TestShellQuoteRoundTrip(t *testing.T) {
	t.Parallel()
	args := []string{
		"docker",
		"ps",
		"--no-trunc",
		"--format={{json .}}", // the originally failing arg
		"has space",
		"it's quoted",
	}

	// Build a shell command: echo <quoted-arg-1> <quoted-arg-2> ...
	// The shell expands the quoted args back to individual words.
	// We print each arg on its own line via printf '%s\n' to avoid echo's
	// whitespace collapsing.
	var quoted []string
	for _, a := range args {
		quoted = append(quoted, shellQuote(a))
	}
	joined := strings.Join(quoted, " ")
	// printf '%s\n' arg1 arg2 ... prints each positional as its own line
	cmd := exec.Command("sh", "-c", "printf '%s\\n' "+joined)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("shell round-trip failed: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != len(args) {
		t.Fatalf("got %d lines, want %d; output=%q", len(lines), len(args), string(out))
	}
	for i, want := range args {
		if lines[i] != want {
			t.Errorf("arg[%d]: round-trip got %q, want %q", i, lines[i], want)
		}
	}
}
