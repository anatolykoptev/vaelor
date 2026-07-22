package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/embeddings"
)

// repoInferNoteMaxRecent is the number of recently-indexed repos named in the
// short missing-repo error (issue #569). Three keeps the first line actionable
// without dumping a catalog (the client already appends its own ~10KB tool-list
// noise on failure).
const repoInferNoteMaxRecent = 3

// inferRepoFromPath resolves a missing `repo` argument from an absolute `path`
// or `file` argument that lies inside a known indexed-repo checkout root
// (issue #569). Returns the inferred repo input (an absolute checkout-root
// path that resolveRoot accepts as a LocalSource) and whether inference
// happened. Returns ("", false) when path is empty, not absolute, or not under
// any LocalRepoDir.
//
// The inferred value is the checkout root: the first path component under the
// matching LocalRepoDir. E.g. path=/host/src/go-code/internal/query with
// LocalRepoDirs=[/host/src] → "/host/src/go-code". resolveRoot then treats it
// as a local path and skips a redundant clone.
func inferRepoFromPath(path string, dirs []string) (string, bool) {
	if path == "" || !filepath.IsAbs(path) {
		return "", false
	}
	clean := filepath.Clean(path)
	for _, dir := range dirs {
		base := filepath.Clean(dir)
		if base == "" {
			continue
		}
		rel, err := filepath.Rel(base, clean)
		if err != nil || rel == "" || strings.HasPrefix(rel, "..") {
			continue
		}
		// First component under the root = the checkout name.
		first := rel
		if i := strings.IndexByte(rel, filepath.Separator); i >= 0 {
			first = rel[:i]
		}
		if first == "" || first == "." {
			continue
		}
		return filepath.Join(base, first), true
	}
	return "", false
}

// shortMissingRepoMsg builds the first-line-actionable missing-repo error
// (issue #569): names the field and up to 3 recently-indexed repos as
// candidates. When the embeddings store is available it uses the live
// recently-indexed set; otherwise it falls back to the basenames of the
// configured LocalRepoDirs. Format:
//
//	missing "repo" — e.g. "go-nerv", "vaelor", "go-wp"
//
// No catalog dump — the client adds its own noise; we keep our line short.
func shortMissingRepoMsg(ctx context.Context, store *embeddings.Store, dirs []string) string {
	var examples []string
	if store != nil {
		examples = store.RecentRepoKeys(ctx, repoInferNoteMaxRecent)
	}
	if len(examples) == 0 {
		for _, d := range dirs {
			name := filepath.Base(d)
			if name == "" || name == "." || name == "/" {
				continue
			}
			examples = append(examples, name)
			if len(examples) >= repoInferNoteMaxRecent {
				break
			}
		}
	}
	if len(examples) == 0 {
		return `missing "repo" — pass a GitHub slug (owner/repo) or an absolute local path`
	}
	quoted := make([]string, len(examples))
	for i, e := range examples {
		quoted[i] = `"` + e + `"`
	}
	return fmt.Sprintf(`missing "repo" — e.g. %s`, strings.Join(quoted, ", "))
}

// resolveOrInferRepo is the shared helper for tools that require `repo` but
// whose agents frequently omit it (code_search, code_research, semantic_search
// — issue #569). When repo is present it is returned unchanged with a nil note.
// When repo is missing but an absolute path/file infers a checkout, the
// inferred repo is returned with a short note to append to the response. When
// repo is missing and no inference is possible, it returns ("", "", false) and
// the caller should emit shortMissingRepoMsg as an error.
//
// deps is analyze.Deps (for LocalRepoDirs); path/file are the optional
// scoping arguments from the tool input.
func resolveOrInferRepo(repo, path, file string, deps analyze.Deps) (inferredRepo, note string, ok bool) {
	if repo != "" {
		return repo, "", true
	}
	if r, found := inferRepoFromPath(firstNonEmpty(path, file), deps.LocalRepoDirs); found {
		return r, fmt.Sprintf(`note: inferred repo %q from path — pass "repo" explicitly to pin it`, r), true
	}
	return "", "", false
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// semStore safely extracts the embeddings.Store from SemanticDeps (nil-safe
// for the no-DATABASE_URL path where sem or sem.Store may be nil).
func semStore(sem *SemanticDeps) *embeddings.Store {
	if sem == nil {
		return nil
	}
	return sem.Store
}
