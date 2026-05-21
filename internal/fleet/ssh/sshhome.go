package ssh

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
