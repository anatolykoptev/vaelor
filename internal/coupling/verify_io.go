package coupling

import (
	"os"
	"path/filepath"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

// maxVerifyFileBytes bounds a file read during stage-2 verification. Routes and
// shared symbols never live in huge files; skip anything larger to keep
// verification cheap on the ARM box. (Moved here from route_verify.go so both
// the route and symbol verifiers share one definition.)
const maxVerifyFileBytes = 512 * 1024

// readVerifyFile reads a verification candidate file and detects its language.
// Returns (nil, "") when the file is missing, a directory, larger than
// maxVerifyFileBytes, or has no registered language handler (markdown,
// lockfiles, VERSION — i.e. the release-noise that must never verify). The
// empty-lang return is the canonical "skip this file" signal both verifiers use.
func readVerifyFile(root, rel string) (src []byte, lang string) {
	full := filepath.Join(root, rel)
	info, err := os.Stat(full)
	if err != nil || info.IsDir() || info.Size() > maxVerifyFileBytes {
		return nil, ""
	}
	b, err := os.ReadFile(full) //nolint:gosec // root+rel are trusted local paths from ResolveRepos
	if err != nil {
		return nil, ""
	}
	lang = parser.DetectLanguageFromPath(rel)
	if lang == "" {
		return nil, "" // no handler (markdown, lockfiles, etc.)
	}
	return b, lang
}
