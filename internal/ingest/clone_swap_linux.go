//go:build linux

package ingest

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// atomicDirectorySwap atomically exchanges tmpDest and finalDest on Linux
// using renameat2(RENAME_EXCHANGE). After the call, tmpDest holds the OLD
// content (which the caller must remove) and finalDest holds the new content.
//
// The exchange is a single kernel syscall — there is no window where finalDest
// is absent. Both paths must be on the same filesystem (same as rename(2)).
//
// On success the OLD directory is removed asynchronously (non-blocking for
// caller). If the exchange syscall fails, tmpDest is cleaned up and an error
// is returned.
func atomicDirectorySwap(tmpDest, finalDest string) error {
	const renameExchange = unix.RENAME_EXCHANGE
	err := unix.Renameat2(unix.AT_FDCWD, tmpDest, unix.AT_FDCWD, finalDest, renameExchange)
	if err != nil {
		// Exchange failed — clean up tmp so caller has no leaked directory.
		_ = os.RemoveAll(tmpDest)
		return fmt.Errorf("atomic directory exchange (renameat2 RENAME_EXCHANGE): %w", err)
	}
	// tmpDest now contains the OLD content. Remove it asynchronously.
	go func() { _ = os.RemoveAll(tmpDest) }()
	return nil
}
