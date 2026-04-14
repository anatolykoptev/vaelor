package forge

import (
	"context"
	"fmt"
)

// GitLab write methods — stubs; implement when GitLab targets are registered.

func (g *GitLabForge) PostReview(_ context.Context, _ string, _ int, _ ReviewPayload) (string, error) {
	return "", fmt.Errorf("gitlab PostReview: not implemented")
}

func (g *GitLabForge) PostIssueComment(_ context.Context, _ string, _ int, _ string) (string, error) {
	return "", fmt.Errorf("gitlab PostIssueComment: not implemented")
}

func (g *GitLabForge) PostCommitComment(_ context.Context, _, _, _ string) (string, error) {
	return "", fmt.Errorf("gitlab PostCommitComment: not implemented")
}
