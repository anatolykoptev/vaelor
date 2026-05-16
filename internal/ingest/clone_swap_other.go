//go:build !linux

package ingest

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// atomicDirectorySwap replaces finalDest with tmpDest using a two-step rename.
// On non-Linux platforms we cannot use renameat2(RENAME_EXCHANGE), so we accept
// a brief window where finalDest is absent between the two renames. This fallback
// is only used on macOS/Windows dev environments; production runs on Linux.
//
// Sequence: rename(finalDest → stale) → rename(tmpDest → finalDest) → rm stale.
// The window between the two renames is <1µs in practice on local filesystems.
func atomicDirectorySwap(tmpDest, finalDest string) error {
	stale := finalDest + ".stale." + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := os.Rename(finalDest, stale); err != nil && !os.IsNotExist(err) {
		_ = os.RemoveAll(tmpDest)
		return fmt.Errorf("atomic swap (non-linux fallback): rename old: %w", err)
	}
	if err := os.Rename(tmpDest, finalDest); err != nil {
		// Attempt to restore the old directory before returning the error.
		_ = os.Rename(stale, finalDest)
		_ = os.RemoveAll(tmpDest)
		return fmt.Errorf("atomic swap (non-linux fallback): rename new into place: %w", err)
	}
	go func() { _ = os.RemoveAll(stale) }()
	return nil
}
