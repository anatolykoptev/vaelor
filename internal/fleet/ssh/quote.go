package ssh

import "strings"

// shellQuote wraps s in POSIX single quotes, escaping any embedded single
// quotes by ending the quote, inserting an escaped single quote, and
// reopening the quote.
//
// Usage: safeguard args that traverse ssh's join+remote-shell-tokenise
// behaviour. OpenSSH joins remote-command args with spaces and sends them as
// a single string; the remote sshd runs the user's shell which re-tokenises on
// whitespace. Any arg containing a space (e.g. "--format={{json .}}") is split
// into multiple tokens by the remote shell unless protected by quoting.
//
// Idempotent on alphanumeric-only strings: the fast path returns s unchanged
// when all bytes are in [a-zA-Z0-9._/=,-]. This keeps the common args
// readable in debug output.
//
// The allowlist still validates UNQUOTED args — quoting is a wire-layer
// concern only; it runs after allowlist validation inside realExecer.Run.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	// Fast path: only shell-safe characters; no quoting needed.
	if isSafeArg(s) {
		return s
	}
	// POSIX single-quote: close quote, escape the single-quote char, reopen.
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

const safeArgChars = "_-./=,abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// isSafeArg reports whether every byte in s is in the safe set
// [a-zA-Z0-9._/=,-], which never requires quoting in a POSIX shell.
func isSafeArg(s string) bool {
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(safeArgChars, s[i]) < 0 {
			return false
		}
	}
	return true
}
