package forge

import (
	"context"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/sync/errgroup"
)

const (
	// defaultContextResults caps how many top results get their Context
	// populated when ContextResults is unset.
	defaultContextResults = 5
	// maxContextFileBytes bounds the file read for context expansion —
	// larger files are skipped rather than truncated mid-line.
	maxContextFileBytes = 1 << 20
)

// contentFingerprint hashes a fragment normalized by whitespace so copied
// files (vendored READMEs, generated stubs) dedupe across results.
func contentFingerprint(content string) uint64 {
	h := fnv.New64a()
	for _, line := range strings.Split(content, "\n") {
		h.Write([]byte(strings.Join(strings.Fields(line), " ")))
		h.Write([]byte{0})
	}
	return h.Sum64()
}

// expandCodeSearchContext fetches the full file for the top results and
// fills Context/ContextStart with a window of contextLines around the first
// match. Failures are tolerated per result — context is enrichment, the
// fragment already carries the match.
func (g *GitHubForge) expandCodeSearchContext(ctx context.Context, results []CodeResult, terms []string, contextLines, contextResults int) {
	if contextLines <= 0 {
		return
	}
	limit := contextResults
	if limit <= 0 {
		limit = defaultContextResults
	}
	if limit > len(results) {
		limit = len(results)
	}

	grp, ctx := errgroup.WithContext(ctx)
	grp.SetLimit(repoStarsWorkers)
	for i := 0; i < limit; i++ {
		r := &results[i]
		grp.Go(func() error {
			content, err := g.fetchRepoFileContent(ctx, r.Repo, r.Path)
			if err != nil || content == "" {
				return nil
			}
			win, start := contextWindow(content, r.rawFrag, r.Matched, contextLines)
			if win == "" {
				return nil
			}
			if isMarkdownSearchPath(r.Path) {
				win = sanitizeSearchFragment(win, nil, append(terms, r.Matched...))
			}
			r.Context, r.ContextStart = win, start
			return nil
		})
	}
	_ = grp.Wait()
}

// fetchRepoFileContent returns the raw file content of repo/path on the
// default branch. Returns "" (not an error) when the file is gone.
func (g *GitHubForge) fetchRepoFileContent(ctx context.Context, repo, filePath string) (string, error) {
	apiURL := fmt.Sprintf("%s/repos/%s/contents/%s", g.apiBase, repo, escapeContentPath(filePath))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("build contents request: %w", err)
	}
	req.Header.Set(ghHeaderAccept, ghMediaTypeRaw)
	req.Header.Set(ghHeaderAPIVersion, ghAPIVersion)

	resp, err := g.doGitHubRequest(ctx, req)
	if err != nil {
		return "", err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxContextFileBytes+1))
	if err != nil {
		return "", fmt.Errorf("read contents response: %w", err)
	}
	if len(body) > maxContextFileBytes {
		return "", nil
	}
	return string(body), nil
}

// escapeContentPath escapes a repo file path for use in a contents URL,
// keeping "/" separators intact.
func escapeContentPath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// contextWindow locates the match inside the full file and returns a window
// of n lines around it plus its 1-based start line. The probe is the raw
// fragment (verbatim file slice); on mismatch it falls back to the longest
// matched term. Returns ("", 0) when nothing can be located.
func contextWindow(content, rawFrag string, matched []string, n int) (string, int) {
	probe, off := locateMatch(content, rawFrag, matched)
	if off < 0 {
		return "", 0
	}

	lines := strings.Split(content, "\n")
	startLine := strings.Count(content[:off], "\n")
	endLine := strings.Count(content[:off+len(probe)], "\n")
	lo := max(0, startLine-n)
	hi := min(len(lines), endLine+n+1)
	return strings.Join(lines[lo:hi], "\n"), lo + 1
}

// locateMatch finds the fragment (or, failing that, the longest matched
// term) inside the file content. Returns the probe actually located and its
// byte offset, or ("", -1).
func locateMatch(content, rawFrag string, matched []string) (string, int) {
	if rawFrag != "" {
		if i := strings.Index(content, rawFrag); i >= 0 {
			return rawFrag, i
		}
	}
	longest := ""
	for _, m := range matched {
		if len(m) > len(longest) {
			longest = m
		}
	}
	if longest != "" {
		if i := strings.Index(content, longest); i >= 0 {
			return longest, i
		}
	}
	return "", -1
}
