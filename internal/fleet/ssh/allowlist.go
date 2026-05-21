package ssh

import (
	"fmt"
	"strings"
)

// allowedDockerArgs is the only permitted docker invocation on the remote host.
var allowedDockerArgs = []string{"docker", "ps", "--no-trunc", "--format={{json .}}"}

// forbiddenSubstrings are shell metacharacters that must never appear in any
// argument passed to ssh. We check every element regardless of position.
var forbiddenSubstrings = []string{";", "|", "$(", "`", "&&", "||", ">", "<", "\n", "\r"}

// Validate checks that args (the full argv that would be passed to ssh,
// including the host and any -p port prefix) match the allowlist.
//
// Allowed shapes:
//
//	[host, "docker", "ps", "--no-trunc", "--format={{json .}}"]
//	["-p", "<port>", host, "docker", "ps", "--no-trunc", "--format={{json .}}"]
//
// All other shapes -> wrapped ErrAllowlistViolation.
//
// Metacharacter check runs first (before shape validation) so injection
// attempts in the host position are caught.
func Validate(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: empty args", ErrAllowlistViolation)
	}

	// Step 1: metacharacter sweep -- check EVERY element.
	for _, a := range args {
		for _, bad := range forbiddenSubstrings {
			if strings.Contains(a, bad) {
				return fmt.Errorf("%w: argument %q contains forbidden substring %q",
					ErrAllowlistViolation, a, bad)
			}
		}
	}

	// Step 2: strip optional -p <port> prefix.
	rest := args
	if len(args) >= 2 && args[0] == "-p" {
		port := args[1]
		if !isAllDigits(port) {
			return fmt.Errorf("%w: -p value %q is not a valid port number", ErrAllowlistViolation, port)
		}
		rest = args[2:]
	}

	// Step 3: after optional port prefix, expect exactly 5 elements:
	//   host, "docker", "ps", "--no-trunc", "--format={{json .}}"
	if len(rest) != 5 {
		return fmt.Errorf("%w: expected 5 args after port prefix, got %d", ErrAllowlistViolation, len(rest))
	}

	// Step 4: validate host (first element of rest).
	host := rest[0]
	if !isValidHost(host) {
		return fmt.Errorf("%w: host %q contains invalid characters (allowed: [a-zA-Z0-9._@-], must not start with '-')",
			ErrAllowlistViolation, host)
	}

	// Step 5: validate the exact docker invocation.
	for i, want := range allowedDockerArgs {
		got := rest[1+i]
		if got != want {
			return fmt.Errorf("%w: arg[%d] = %q; want %q",
				ErrAllowlistViolation, i, got, want)
		}
	}

	return nil
}

// isValidHost reports whether s is a non-empty string matching [a-zA-Z0-9._@-]+
// that does NOT start with '-'. A leading '-' would cause ssh to interpret the
// value as a flag rather than a destination hostname.
// No regexp -- hand-rolled per constraint.
func isValidHost(s string) bool {
	if len(s) == 0 {
		return false
	}
	// Reject any host whose first byte is '-'.
	// url.Parse("ssh://-v") returns Hostname()=="-v"; without this guard,
	// ssh would interpret "-v" as its verbose flag, not a destination.
	if s[0] == '-' {
		return false
	}
	for _, c := range s {
		if !isHostChar(c) {
			return false
		}
	}
	return true
}

// isHostChar reports whether c is in [a-zA-Z0-9._@-].
func isHostChar(c rune) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '.' || c == '_' || c == '@' || c == '-'
}

// isAllDigits reports whether s consists solely of ASCII digit characters.
func isAllDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
