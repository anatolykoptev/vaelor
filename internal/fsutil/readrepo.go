// Package fsutil holds stdlib-only filesystem helpers shared across packages.
package fsutil

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"syscall"
)

var (
	// ErrNotRegular is returned when the target is a symlink, device, FIFO,
	// socket or directory rather than a regular file.
	ErrNotRegular = errors.New("fsutil: not a regular file")
	// ErrTooLarge is returned by ReadRepoFile when the file exceeds the limit.
	ErrTooLarge = errors.New("fsutil: file exceeds size limit")
)

// ReadRepoFile reads the regular file rel inside root and fails with
// ErrTooLarge when it holds more than max bytes. It is for files whose name is
// chosen by an untrusted checkout: it refuses anything that is not one of the
// repository's own regular files and never returns more than max bytes.
//
// Policy:
//   - root confinement: the file is opened through os.OpenRoot, so ".." and
//     symlinks (in any component) cannot lead outside root.
//   - the final path component must not be a symlink, even one that points
//     inside root: callers want the repo's own files, not an alias. Symlinked
//     intermediate directories that stay inside root are still allowed.
//   - the opened descriptor must be a regular file (devices, FIFOs, sockets and
//     directories are refused). The check runs on the open fd and is
//     cross-checked against an Lstat of the same name with os.SameFile, so a
//     file swapped for a link between the two calls is refused rather than read.
//   - the open uses O_NONBLOCK so a FIFO cannot hang the caller.
//
// Reads are bounded by max, so os-level reads need no context: they cannot run
// away. Errors name the path but never carry file contents.
func ReadRepoFile(root, rel string, max int64) ([]byte, error) {
	data, err := readBounded(root, rel, max)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("%w: %s (max %d bytes)", ErrTooLarge, rel, max)
	}
	return data, nil
}

// ReadRepoFilePrefix is ReadRepoFile for callers that only want the head of a
// file: a file larger than max yields its first max bytes and no error. All
// other refusals are identical to ReadRepoFile.
func ReadRepoFilePrefix(root, rel string, max int64) ([]byte, error) {
	data, err := readBounded(root, rel, max)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		data = data[:max]
	}
	return data, nil
}

// readBounded returns up to max+1 bytes so the caller can tell "exactly max"
// from "more than max".
func readBounded(root, rel string, max int64) ([]byte, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	li, err := r.Lstat(rel)
	if err != nil {
		return nil, err
	}
	if !li.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: %s", ErrNotRegular, rel)
	}
	f, err := r.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || !os.SameFile(fi, li) {
		return nil, fmt.Errorf("%w: %s", ErrNotRegular, rel)
	}
	return io.ReadAll(io.LimitReader(f, max+1))
}

// ReportRefusal logs a refused or failed repo-file read. A missing file is the
// normal case for optional files and stays silent; everything else is a
// warning. The message carries the error text only, never file contents.
func ReportRefusal(component, rel string, err error) {
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return
	}
	slog.Warn("repo file read refused", //nolint:sloglint // static message, structured attrs
		slog.String("component", component),
		slog.String("path", rel),
		slog.Any("error", err))
}
