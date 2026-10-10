// Package fsutil holds filesystem helpers shared across packages (stdlib plus
// the Prometheus client for the refusal counter).
package fsutil

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"strings"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Refusal reasons: a bounded label set for gocode_repo_file_refused_total.
const (
	ReasonNotRegular = "not_regular"
	ReasonTooLarge   = "too_large"
	ReasonEscape     = "escape"
	ReasonOther      = "other"
)

var refusedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "gocode_repo_file_refused_total",
	Help: "Repo file reads refused by fsutil (symlink, special file, oversize, escape), by call site and reason.",
}, []string{"site", "reason"})

var (
	// ErrNotRegular is returned when the target is a symlink, device, FIFO,
	// socket or directory rather than a regular file.
	ErrNotRegular = errors.New("fsutil: not a regular file")
	// ErrTooLarge is returned by ReadRepoFile when the file exceeds the limit.
	ErrTooLarge = errors.New("fsutil: file exceeds size limit")
)

// ReadRepoFile reads the regular file rel inside root and fails with
// ErrTooLarge when it holds more than limit bytes. It is for files whose name is
// chosen by an untrusted checkout: it refuses anything that is not one of the
// repository's own regular files and never returns more than limit bytes.
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
// Reads are bounded by limit, so os-level reads need no context: they cannot run
// away. Errors name the path but never carry file contents.
func ReadRepoFile(root, rel string, limit int64) ([]byte, error) {
	data, err := readBounded(root, rel, limit)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: %s (max %d bytes)", ErrTooLarge, rel, limit)
	}
	return data, nil
}

// ReadRepoFilePrefix is ReadRepoFile for callers that only want the head of a
// file: a file larger than limit yields its first limit bytes and no error. All
// other refusals are identical to ReadRepoFile.
func ReadRepoFilePrefix(root, rel string, limit int64) ([]byte, error) {
	data, err := readBounded(root, rel, limit)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		data = data[:limit]
	}
	return data, nil
}

// readBounded returns up to limit+1 bytes so the caller can tell "exactly limit"
// from "more than max".
func readBounded(root, rel string, limit int64) ([]byte, error) {
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
	return io.ReadAll(io.LimitReader(f, limit+1))
}

// ReportRefusal records a refused or failed repo-file read: it bumps
// gocode_repo_file_refused_total{site,reason} and logs a warning. A missing
// file is the normal case for optional files and stays silent. component must
// be a fixed string per call site (it is a metric label); the log carries the
// error text only, never file contents.
func ReportRefusal(component, rel string, err error) {
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return
	}
	CountRefusal(component, Reason(err))
	slog.Warn("repo file read refused", //nolint:sloglint // static message, structured attrs
		slog.String("component", component),
		slog.String("path", rel),
		slog.Any("error", err))
}

// CountRefusal bumps the refusal counter for a caller that skips an entry
// without reading it (for example a directory walk skipping a symlink).
func CountRefusal(component, reason string) {
	refusedTotal.WithLabelValues(component, reason).Inc()
}

// Reason classifies a refusal error into the bounded reason label set.
func Reason(err error) string {
	switch {
	case errors.Is(err, ErrNotRegular):
		return ReasonNotRegular
	case errors.Is(err, ErrTooLarge):
		return ReasonTooLarge
	case strings.Contains(err.Error(), "path escapes"):
		return ReasonEscape
	default:
		return ReasonOther
	}
}
