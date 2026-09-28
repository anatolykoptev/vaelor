package biomarkers

import (
	"os"
	"strconv"
	"time"
)

// osWriteFile is the test-package wrapper used by helper test fixtures.
var osWriteFile = os.WriteFile

// gitTestEnv isolates fixture repos from the ambient git environment:
// inherited GIT_*_DATE vars would shift commits relative to the --since
// window, and a host ~/.gitconfig carrying commit.gpgsign or core.hooksPath
// could make commits fail or misbehave (#811 — the CI flake vector).
// Commit dates are pinned to now — a fixed past date would age out of the
// 90-day window and silently zero the churn stats.
func gitTestEnv() []string {
	now := time.Now().UTC().Format(time.RFC3339)
	return append(os.Environ(),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_AUTHOR_DATE="+now,
		"GIT_COMMITTER_DATE="+now,
	)
}

// itoa is a one-line wrapper around strconv.Itoa used by helper test
// fixtures that build deterministic numeric file content.
func itoa(i int) string { return strconv.Itoa(i) }
