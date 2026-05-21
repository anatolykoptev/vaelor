// internal/fleet/upstream/client.go
package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/anatolykoptev/go-code/internal/fleet"
)

const (
	// maxCommits is the per-Changelog commit cap.
	maxCommits = 20

	// maxBodyBytes is the per-response body cap (16 KiB).
	maxBodyBytes = 16 * 1024

	// defaultTimeout is the default per-request timeout.
	defaultTimeout = 8 * time.Second

	// githubAPIBase is the GitHub REST API base URL (overridable in tests).
	githubAPIBase = "https://api.github.com"
)

// Client makes GitHub Compare API calls to produce Changelog values.
type Client struct {
	httpClient *http.Client
	token      string
	timeout    time.Duration
	baseURL    string // overridable for tests; defaults to githubAPIBase
}

// Option configures a Client.
type Option func(*Client)

// WithToken sets the GitHub PAT or App-installation token.
func WithToken(t string) Option {
	return func(c *Client) { c.token = t }
}

// WithHTTPClient overrides the underlying http.Client.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

// WithTimeout sets per-request timeout. Default 8s.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) { c.timeout = d }
}

// WithBaseURL overrides the GitHub API base URL. Used in tests to point at
// an httptest.Server. Production code should never call this.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = u }
}

// New creates a new Client with the given options applied.
func New(opts ...Option) *Client {
	c := &Client{
		httpClient: &http.Client{},
		timeout:    defaultTimeout,
		baseURL:    githubAPIBase,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// githubCompareResp is the minimal shape parsed from GitHub's /compare API.
type githubCompareResp struct {
	Status  string              `json:"status"`
	HTMLURL string              `json:"html_url"`
	Commits []githubCommitEntry `json:"commits"`
}

type githubCommitEntry struct {
	SHA    string            `json:"sha"`
	Commit githubCommitInner `json:"commit"`
}

type githubCommitInner struct {
	Message string       `json:"message"`
	Author  githubAuthor `json:"author"`
}

type githubAuthor struct {
	Name string `json:"name"`
	Date string `json:"date"`
}

// tagForms returns the tag forms to attempt in order for a given raw version string.
func tagForms(raw string) []string {
	forms := make([]string, 0, 3)
	forms = append(forms, raw)
	if !strings.HasPrefix(raw, "v") {
		forms = append(forms, "v"+raw)
	}
	if !strings.HasPrefix(raw, "release-") {
		forms = append(forms, "release-"+raw)
	}
	return forms
}

// Compare returns the GitHub /compare/{base}...{head} payload, normalised
// into our Changelog shape. Attempts tag forms in order: <tag>, v<tag>,
// release-<tag> for both base and head simultaneously.
//
// Returns Resolved=false (never a non-nil error) for soft failures:
//   - HTTP 404 (one of the tags doesn't exist; all forms tried)
//   - HTTP 422 (refs don't share an ancestor)
//   - HTTP 403 with X-RateLimit-Remaining: 0 (rate limit)
//   - Context timeout or cancellation
func (c *Client) Compare(ctx context.Context, slug, base, head string) (*fleet.Changelog, error) {
	baseForms := tagForms(base)
	headForms := tagForms(head)

	// Try each combination: (base[0],head[0]), (base[1],head[1]), (base[2],head[2]).
	// Spec says "try <tag>, v<tag>, release-<tag>" for both base and head together.
	// We iterate form index, using min(len(baseForms), len(headForms)) attempts.
	maxAttempts := len(baseForms)
	if len(headForms) < maxAttempts {
		maxAttempts = len(headForms)
	}

	for i := 0; i < maxAttempts; i++ {
		bForm := baseForms[i]
		hForm := headForms[i]

		cl, done, err := c.compareOnce(ctx, slug, bForm, hForm, base, head)
		if err != nil {
			// Context-level error: return soft-fail immediately.
			return &fleet.Changelog{
				Repo:     slug,
				Base:     base,
				Head:     head,
				Status:   "unresolved",
				Resolved: false,
				Reason:   err.Error(),
			}, nil
		}
		if done {
			return cl, nil
		}
		// Not done yet: 404 on this form, try next.
	}

	// All forms exhausted.
	return &fleet.Changelog{
		Repo:     slug,
		Base:     base,
		Head:     head,
		Status:   "unresolved",
		Resolved: false,
		Reason:   "no matching tags upstream",
	}, nil
}

// compareOnce performs one HTTP request for the given (slug, bForm, hForm) combo.
// Returns:
//   - cl, true, nil   → success or permanent failure (resolved or unresolved but final)
//   - nil, false, nil → 404 on this tag form, caller should try next
//   - nil, false, err → context/network error, caller should abort
func (c *Client) compareOnce(ctx context.Context, slug, bForm, hForm, rawBase, rawHead string) (*fleet.Changelog, bool, error) {
	url := fmt.Sprintf("%s/repos/%s/compare/%s...%s", c.baseURL, slug, bForm, hForm)

	reqCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Check if this was a context cancellation/timeout.
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		if reqCtx.Err() != nil {
			// Per-request timeout.
			return nil, false, fmt.Errorf("request timeout: %w", reqCtx.Err())
		}
		return nil, false, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotFound:
		// Tag form doesn't exist: try next form.
		return nil, false, nil

	case http.StatusUnprocessableEntity:
		// Refs exist but don't share a common ancestor.
		return &fleet.Changelog{
			Repo:     slug,
			Base:     rawBase,
			Head:     rawHead,
			Status:   "unresolved",
			Resolved: false,
			Reason:   "refs don't share a common ancestor",
		}, true, nil

	case http.StatusForbidden:
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return &fleet.Changelog{
				Repo:     slug,
				Base:     rawBase,
				Head:     rawHead,
				Status:   "unresolved",
				Resolved: false,
				Reason:   "GitHub API rate limit exceeded",
			}, true, nil
		}
		// Other 403 (auth failure, org restriction) — treat as permanent failure.
		return &fleet.Changelog{
			Repo:     slug,
			Base:     rawBase,
			Head:     rawHead,
			Status:   "unresolved",
			Resolved: false,
			Reason:   fmt.Sprintf("HTTP 403 Forbidden"),
		}, true, nil

	case http.StatusOK:
		// Fall through to parse.

	default:
		// Any other non-200 → soft fail.
		return &fleet.Changelog{
			Repo:     slug,
			Base:     rawBase,
			Head:     rawHead,
			Status:   "unresolved",
			Resolved: false,
			Reason:   fmt.Sprintf("unexpected HTTP %d", resp.StatusCode),
		}, true, nil
	}

	// Parse response body with size cap.
	limited := io.LimitReader(resp.Body, maxBodyBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		return &fleet.Changelog{
			Repo:     slug,
			Base:     rawBase,
			Head:     rawHead,
			Status:   "unresolved",
			Resolved: false,
			Reason:   fmt.Sprintf("read body: %v", err),
		}, true, nil
	}

	var ghResp githubCompareResp
	if err := json.Unmarshal(body, &ghResp); err != nil {
		return &fleet.Changelog{
			Repo:     slug,
			Base:     rawBase,
			Head:     rawHead,
			Status:   "unresolved",
			Resolved: false,
			Reason:   fmt.Sprintf("parse response: %v", err),
		}, true, nil
	}

	// Build Changelog from response.
	cl := &fleet.Changelog{
		Repo:     slug,
		Base:     bForm, // the actually-resolved tag form
		Head:     hForm,
		Status:   ghResp.Status,
		Resolved: true,
		URL:      ghResp.HTMLURL,
	}

	// Cap commits at maxCommits.
	commits := ghResp.Commits
	if len(commits) > maxCommits {
		cl.Truncated = true
		commits = commits[:maxCommits]
	}

	cl.Commits = make([]fleet.ChangelogCommit, 0, len(commits))
	for _, entry := range commits {
		subject := entry.Commit.Message
		if idx := strings.IndexByte(subject, '\n'); idx >= 0 {
			subject = subject[:idx]
		}
		cl.Commits = append(cl.Commits, fleet.ChangelogCommit{
			SHA:     entry.SHA,
			Author:  entry.Commit.Author.Name,
			Date:    entry.Commit.Author.Date,
			Subject: subject,
		})
	}

	return cl, true, nil
}
