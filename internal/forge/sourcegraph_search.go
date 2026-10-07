package forge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	kitcache "github.com/anatolykoptev/go-kit/cache"
	"github.com/anatolykoptev/vaelor/internal/cache"
)

const (
	sgDefaultBase    = "https://sourcegraph.com"
	sgDefaultTimeout = 20 * time.Second
	sgSearchCacheTTL = 15 * time.Minute
	// sgDefaultCount caps streamed matches when the caller did not set a
	// max_results budget.
	sgDefaultCount = 100
)

// SourcegraphClient queries a Sourcegraph instance's streaming search API
// (zoekt backend). The public sourcegraph.com endpoint works without
// credentials; a token raises rate limits.
type SourcegraphClient struct {
	base  string
	token string
	http  *http.Client
	cache *kitcache.Cache
}

// SourcegraphOption configures a SourcegraphClient.
type SourcegraphOption func(*SourcegraphClient)

// WithSourcegraphCache sets the cache used by SourcegraphClient.
func WithSourcegraphCache(c *kitcache.Cache) SourcegraphOption {
	return func(s *SourcegraphClient) { s.cache = c }
}

// NewSourcegraphClient creates a client for a Sourcegraph instance.
// Empty base defaults to the public sourcegraph.com.
func NewSourcegraphClient(base, token string, opts ...SourcegraphOption) *SourcegraphClient {
	if base == "" {
		base = sgDefaultBase
	}
	s := &SourcegraphClient{
		base:  strings.TrimSuffix(base, "/"),
		token: token,
		http:  &http.Client{Timeout: sgDefaultTimeout},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithSourcegraph attaches a Sourcegraph client used for engine=sourcegraph
// searches and engine=auto fallback coverage.
func WithSourcegraph(s *SourcegraphClient) GitHubForgeOption {
	return func(g *GitHubForge) { g.sg = s }
}

// sgPosition is a line/column/offset position inside a match.
type sgPosition struct {
	Offset int `json:"offset"`
	Line   int `json:"line"`
	Column int `json:"column"`
}

// sgLineMatch is a matching line in a Sourcegraph content result.
type sgLineMatch struct {
	Line             string  `json:"line"`
	LineNumber       int     `json:"lineNumber"` // 0-based
	OffsetAndLengths [][]int `json:"offsetAndLengths"`
}

// sgChunkMatch is a contiguous content chunk with match ranges.
type sgChunkMatch struct {
	Content      string     `json:"content"`
	ContentStart sgPosition `json:"contentStart"`
	Ranges       []struct {
		Start sgPosition `json:"start"`
		End   sgPosition `json:"end"`
	} `json:"ranges"`
}

// sgContentMatch is one file result of a Sourcegraph stream search.
type sgContentMatch struct {
	Type         string         `json:"type"`
	Path         string         `json:"path"`
	Repository   string         `json:"repository"` // e.g. "github.com/o/r"
	RepoStars    int            `json:"repoStars"`
	Commit       string         `json:"commit"`
	Language     string         `json:"language"`
	LineMatches  []sgLineMatch  `json:"lineMatches"`
	ChunkMatches []sgChunkMatch `json:"chunkMatches"`
}

// sgProgress is the final progress event of a stream.
type sgProgress struct {
	Done       bool `json:"done"`
	MatchCount int  `json:"matchCount"`
}

// searchCodeSourcegraph runs a code search against the attached Sourcegraph
// client. maxKeep bounds how many results are returned; expansion of context
// around matches is applied for github.com repos via the GitHub contents API.
func (g *GitHubForge) searchCodeSourcegraph(ctx context.Context, query string, repos []string, opt SearchCodeOptions, maxKeep int) (CodeSearchResult, error) {
	if g.sg == nil {
		return CodeSearchResult{}, errors.New("sourcegraph client not configured")
	}

	count := maxKeep
	if count <= 0 {
		count = opt.MaxResults
	}
	if count <= 0 {
		count = sgDefaultCount
	}

	q, err := buildSourcegraphQuery(query, repos, opt, count)
	if err != nil {
		return CodeSearchResult{}, err
	}

	key := cache.Key("sg:code:search", q, strconv.Itoa(count), strconv.Itoa(opt.MinStars), strconv.Itoa(opt.ContextLines), strconv.Itoa(opt.ContextResults), strconv.Itoa(maxKeep))
	result, err := cacheGetOrLoadJSONWithTTL(g.sg.cache, ctx, key, sgSearchCacheTTL, func(ctx context.Context) (CodeSearchResult, error) {
		res, err := g.sg.collectSourcegraphResults(ctx, q, count, opt.MinStars)
		if err != nil {
			return CodeSearchResult{}, err
		}
		g.expandCodeSearchContext(ctx, res.Results, searchQueryTerms(q), opt.ContextLines, opt.ContextResults)
		return res, nil
	})
	if err != nil {
		return CodeSearchResult{}, err
	}
	result.Query = q
	return result, nil
}

// collectSourcegraphResults streams the search and converts matches.
func (s *SourcegraphClient) collectSourcegraphResults(ctx context.Context, q string, count, minStars int) (CodeSearchResult, error) {
	matches, err := s.streamSearch(ctx, q)
	if err != nil {
		return CodeSearchResult{}, err
	}

	result := CodeSearchResult{Query: q}
	seen := make(map[uint64]struct{})
	for _, m := range matches {
		if m.Type != "content" {
			continue
		}
		if minStars > 0 && m.RepoStars < minStars {
			continue
		}
		r, ok := convertSGMatch(m)
		if !ok {
			continue
		}
		h := contentFingerprint(r.Content)
		if _, dup := seen[h]; dup {
			continue
		}
		seen[h] = struct{}{}
		result.Results = append(result.Results, r)
		if count > 0 && len(result.Results) >= count {
			break
		}
	}
	result.Total = len(result.Results)
	return result, nil
}

// streamSearch issues the streaming search request and collects all
// "matches" events until "done" or an error event.
func (s *SourcegraphClient) streamSearch(ctx context.Context, q string) ([]sgContentMatch, error) {
	u := s.base + "/.api/search/stream?q=" + url.QueryEscape(q) + "&v=V3"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build sourcegraph search request: %w", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", ghUserAgent)
	if s.token != "" {
		req.Header.Set("Authorization", "token "+s.token)
	}

	resp, err := s.http.Do(req) //nolint:gosec // URL built from configured base + escaped query
	if err != nil {
		return nil, fmt.Errorf("sourcegraph search: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sourcegraph search returned %d", resp.StatusCode)
	}

	return parseSourcegraphStream(resp.Body)
}

// parseSourcegraphStream decodes the SSE stream: event/data line pairs,
// collecting "matches" payloads until "done" or an "error" event.
func parseSourcegraphStream(body io.Reader) ([]sgContentMatch, error) {
	var out []sgContentMatch
	var data bytes.Buffer
	eventName := ""

	sc := bufio.NewScanner(body)
	sc.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)
	flush := func() error {
		defer data.Reset()
		switch eventName {
		case "matches":
			var batch []sgContentMatch
			if err := json.Unmarshal(data.Bytes(), &batch); err != nil {
				return fmt.Errorf("decode sourcegraph matches: %w", err)
			}
			out = append(out, batch...)
		case "error":
			return fmt.Errorf("sourcegraph stream error: %s", strings.TrimSpace(data.String()))
		}
		eventName = ""
		return nil
	}

	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			eventName = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data.WriteString(strings.TrimPrefix(line, "data: "))
		case line == "":
			if err := flush(); err != nil {
				return nil, err
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read sourcegraph stream: %w", err)
	}
	return out, flush()
}

// convertSGMatch maps a Sourcegraph content match into a CodeResult.
// The second return value is false for matches without a usable repository.
func convertSGMatch(m sgContentMatch) (CodeResult, bool) {
	repo, host := sgRepoSlug(m.Repository)
	if repo == "" {
		return CodeResult{}, false
	}

	r := CodeResult{
		Path:   m.Path,
		Repo:   repo,
		Commit: m.Commit,
		Stars:  m.RepoStars,
		Engine: "sourcegraph",
	}
	r.URL = sgResultURL(m.Repository, host, repo, m.Commit, m.Path, m.LineMatches)

	var matched []string
	if len(m.ChunkMatches) > 0 {
		for _, cm := range m.ChunkMatches {
			base := cm.ContentStart.Line
			for _, rg := range cm.Ranges {
				line1 := base + rg.Start.Line + 1
				r.Lines = appendLine(r.Lines, line1)
				if t := sliceSpan(cm.Content, rg.Start.Offset, rg.End.Offset); t != "" {
					matched = appendUnique(matched, t)
				}
			}
			if frag := strings.TrimSpace(cm.Content); frag != "" {
				if r.rawFrag == "" {
					r.rawFrag = frag
				}
				r.Content = joinFragment(r.Content, frag)
			}
		}
	} else {
		for _, lm := range m.LineMatches {
			r.Lines = appendLine(r.Lines, lm.LineNumber+1)
			if r.rawFrag == "" {
				r.rawFrag = lm.Line
			}
			r.Content = joinFragment(r.Content, lm.Line)
			for _, ol := range lm.OffsetAndLengths {
				if t := sliceSpan(lm.Line, ol[0], ol[0]+ol[1]); t != "" {
					matched = appendUnique(matched, t)
				}
			}
		}
	}
	if r.Content == "" {
		r.Content = "File: " + m.Path
	}
	r.Matched = matched
	return r, true
}

// sgRepoSlug splits a Sourcegraph repository name ("github.com/o/r") into the
// owner/repo slug and host. Returns "" for unparseable names.
func sgRepoSlug(repository string) (slug, host string) {
	parts := strings.SplitN(repository, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", ""
	}
	return parts[1], parts[0]
}

// sgResultURL builds a commit-pinned URL for a match: a github.com blob URL
// for GitHub-hosted repos, a Sourcegraph URL otherwise.
func sgResultURL(repository, host, repo, commit, path string, lms []sgLineMatch) string {
	frag := ""
	if len(lms) > 0 {
		frag = "#L" + strconv.Itoa(lms[0].LineNumber+1)
	}
	if host == "github.com" {
		ref := commit
		if ref == "" {
			ref = "HEAD"
		}
		return "https://github.com/" + repo + "/blob/" + ref + "/" + path + frag
	}
	at := ""
	if commit != "" {
		at = "@" + commit
	}
	return sgDefaultBase + "/" + repository + at + "/-/blob/" + path + frag
}

// sliceSpan returns content[start:end] clamped to bounds, or "" when the
// span is invalid.
func sliceSpan(content string, start, end int) string {
	if start < 0 || end > len(content) || start >= end {
		return ""
	}
	return content[start:end]
}

func appendLine(lines []int, n int) []int {
	for _, l := range lines {
		if l == n {
			return lines
		}
	}
	return append(lines, n)
}

func appendUnique(xs []string, s string) []string {
	for _, x := range xs {
		if x == s {
			return xs
		}
	}
	return append(xs, s)
}

// joinFragment appends a fragment to content with the fragment separator.
func joinFragment(content, frag string) string {
	if content == "" {
		return frag
	}
	return content + "\n---\n" + frag
}
