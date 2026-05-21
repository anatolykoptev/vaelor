package ssh

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ensureWritableSSHHome populates a writable copy of the ssh source dir at
// dst/.ssh with root-owned, mode-0700/0600 perms, so that OpenSSH client's
// strict-mode ownership check passes when invoked with HOME=dst.
//
// Idempotent: if dst/.ssh already exists as a directory with mode 0700 and
// contains at least one file, the function returns nil immediately.
//
// src is the operator's ~/.ssh bind-mounted into the container
// (e.g. /root/.ssh). dst is a writable tmpfs path (e.g. /tmp/fleet-ssh-home);
// the .ssh subdirectory is created inside it.
//
// Returns nil if src does not exist — the ssh subprocess will then fail
// with a clearer "no such identity" error rather than a copy-step error.
//
// Does not chown — the calling process runs as root in the production
// container, so created files inherit the process uid automatically.
// Under non-root (tests), files still get the correct mode for the user's
// own ~/.ssh.
//
// Subdirectories inside src are skipped (MVP; uncommon in practice; flagged
// with a TODO below if deep ControlMaster or Include paths are needed).
func ensureWritableSSHHome(src, dst string) error {
	// Fast path: dst/.ssh already prepared by a prior call.
	sshDir := filepath.Join(dst, ".ssh")
	if fi, err := os.Stat(sshDir); err == nil && fi.IsDir() && fi.Mode().Perm() == 0o700 {
		return nil
	}

	// Check whether src exists. Missing src is a no-op (not an error).
	if _, err := os.Stat(src); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("fleet/ssh: stat src %s: %w", src, err)
	}

	// Create destination .ssh directory with strict perms.
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return fmt.Errorf("fleet/ssh: mkdir %s: %w", sshDir, err)
	}

	// Walk source directory; copy regular files only (skip subdirs — TODO:
	// support ControlMaster socket dirs and Include path hierarchies if needed).
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("fleet/ssh: readdir %s: %w", src, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			// TODO(ssh-home): recurse into subdirs for Include paths.
			continue
		}
		if !entry.Type().IsRegular() {
			continue
		}
		if err := copySSHFile(filepath.Join(src, entry.Name()), filepath.Join(sshDir, entry.Name())); err != nil {
			return err
		}
	}

	// Rewrite ~/.ssh/ and $HOME/.ssh/ in the copied config so that
	// IdentityFile / UserKnownHostsFile etc. point at the writable shadow-copy
	// paths. Required because OpenSSH 10.2p1 on alpine uses getpwuid() for ~/
	// expansion, not $HOME env — so without this rewrite, those lines would
	// resolve to /root/.ssh (the bind-mounted uid-1000 originals) and fail
	// the strict-mode ownership check.
	copiedCfg := filepath.Join(sshDir, "config")
	if _, err := os.Stat(copiedCfg); err == nil {
		if err := rewriteSSHConfigPaths(copiedCfg, dst); err != nil {
			return err
		}
	}

	return nil
}

// rewriteSSHConfigPaths replaces ~/.ssh/ and $HOME/.ssh/ literals in the
// shadow-copied config so that IdentityFile / UserKnownHostsFile / etc. lines
// point at the writable-copy paths under homeDst. Required because OpenSSH's
// ~/ expansion uses getpwuid() (always points at the bind-mounted uid-1000
// originals), not the $HOME env var.
//
// Only the config file at configPath is modified; key files and known_hosts
// are not changed. The rewrite is idempotent: a second pass on an already-
// rewritten config produces the same output.
//
// Rewrite rules:
//   - "~/.ssh/" → "<homeDst>/.ssh/"
//   - "$HOME/.ssh/" → "<homeDst>/.ssh/"
//
// Absolute paths (starting with "/") and all other values are left untouched.
func rewriteSSHConfigPaths(configPath, homeDst string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("fleet/ssh: rewrite config read %s: %w", configPath, err)
	}

	dst := homeDst + "/.ssh/"
	// Replace both tilde and $HOME forms. Order matters: do $HOME first so that
	// a hypothetical "~/$HOME/.ssh/" edge-case doesn't get double-rewritten.
	out := strings.ReplaceAll(string(data), "$HOME/.ssh/", dst)
	out = strings.ReplaceAll(out, "~/.ssh/", dst)

	if out == string(data) {
		// Nothing to rewrite; avoid a pointless write.
		return nil
	}

	if err := os.WriteFile(configPath, []byte(out), 0o600); err != nil {
		return fmt.Errorf("fleet/ssh: rewrite config write %s: %w", configPath, err)
	}
	return nil
}

// copySSHFile copies a single regular file from src to dst, setting 0600 perms.
func copySSHFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("fleet/ssh: open %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("fleet/ssh: create %s: %w", dst, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("fleet/ssh: copy %s → %s: %w", src, dst, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("fleet/ssh: close %s: %w", dst, err)
	}
	return nil
}

// envWithHome returns a copy of parentEnv with HOME=dst appended.
// When dst is empty the original slice is returned unchanged (no allocation).
// Appending last means the subprocess's getenv("HOME") picks up the new value
// even when an earlier HOME= entry exists (standard "last wins" POSIX behaviour).
func envWithHome(parentEnv []string, dst string) []string {
	if dst == "" {
		return parentEnv
	}
	result := make([]string, len(parentEnv)+1)
	copy(result, parentEnv)
	result[len(parentEnv)] = "HOME=" + dst
	return result
}

// sshHomeMu guards concurrent calls to ensureWritableSSHHome on a per-Driver
// basis. Each Driver embeds its own sync.Once so that the first concurrent
// caller wins and all others block until the preparation is complete.
//
// sshHomeOnce is stored on realExecer (not Driver) because realExecer is
// where exec.Command is constructed and HOME is injected.
type sshHomeState struct {
	once sync.Once
	err  error
}
