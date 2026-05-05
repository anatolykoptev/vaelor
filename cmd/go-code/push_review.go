package main

import (
	"context"
	"fmt"
	"os"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/forge"
	"github.com/anatolykoptev/go-code/internal/review"
)

// handlePushReview runs a delta review on before..after and posts a commit
// comment on the after SHA. Intended for direct pushes to main (no PR to
// attach a review to).
func handlePushReview(slug, before, after string, deps analyze.Deps) error {
	ctx := context.Background()
	root, cleanup, err := resolveRoot(ctx, slug, "", deps)
	if err != nil {
		return fmt.Errorf("resolve: %w", err)
	}
	defer cleanup()

	// Delta between the pushed range.
	result, err := review.DeltaReview(ctx, review.DeltaInput{
		Root:    root,
		Base:    before,
		Depth:   defaultReviewDepth,
		OxCodes: deps.OxCodes,
	})
	if err != nil {
		return fmt.Errorf("review: %w", err)
	}
	findings := applyPolicy(ctx, root, result)
	body, inline := renderReview(result, findings)

	// Inline comments on a commit comment are not supported by the GitHub
	// API in the same shape — append them as a simple list in the body.
	if len(inline) > 0 {
		body += "\n### Inline findings\n"
		for _, c := range inline {
			body += fmt.Sprintf("- `%s:%d` %s\n", c.Path, c.Line, c.Body)
		}
	}

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return fmt.Errorf("GITHUB_TOKEN not set")
	}
	g := forge.NewGitHubForge(token, forge.AppConfig{})
	_, err = g.PostCommitComment(ctx, slug, after, body)
	return err
}
