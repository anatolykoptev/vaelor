package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// PostReview posts a pull request review (body + inline comments).
func (g *GitHubForge) PostReview(ctx context.Context, slug string, pr int, p ReviewPayload) (string, error) {
	type ghInline struct {
		Path string `json:"path"`
		Line int    `json:"line"`
		Body string `json:"body"`
		Side string `json:"side"`
	}
	type ghBody struct {
		Body     string     `json:"body"`
		Event    string     `json:"event"`
		Comments []ghInline `json:"comments,omitempty"`
	}
	body := ghBody{Body: p.Body, Event: p.Event}
	for _, c := range p.Comments {
		body.Comments = append(body.Comments, ghInline{Path: c.Path, Line: c.Line, Body: c.Body, Side: "RIGHT"})
	}
	return g.postJSON(ctx, fmt.Sprintf("%s/repos/%s/pulls/%d/reviews", g.apiBase, slug, pr), body, "post review")
}

// PostCommitComment posts a comment on a specific commit SHA.
func (g *GitHubForge) PostCommitComment(ctx context.Context, slug, sha, body string) (string, error) {
	return g.postJSON(ctx, fmt.Sprintf("%s/repos/%s/commits/%s/comments", g.apiBase, slug, sha), map[string]string{"body": body}, "post commit comment")
}

// PostIssueComment posts a comment on a PR or issue thread.
func (g *GitHubForge) PostIssueComment(ctx context.Context, slug string, number int, body string) (string, error) {
	return g.postJSON(ctx, fmt.Sprintf("%s/repos/%s/issues/%d/comments", g.apiBase, slug, number), map[string]string{"body": body}, "post comment")
}

// postJSON is the shared POST + decode html_url helper for all write methods.
func (g *GitHubForge) postJSON(ctx context.Context, url string, payload any, opName string) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("%s marshal: %w", opName, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("%s build: %w", opName, err)
	}
	g.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s: %w", opName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("%s: %s: %s", opName, resp.Status, string(b))
	}
	var out struct {
		HTMLURL string `json:"html_url"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out.HTMLURL, nil
}
