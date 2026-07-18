package main

import (
	"fmt"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/forge"
)

// resolvePostForge resolves the Forge implementation that should receive a
// WRITE (PostReview / PostCommitComment / PostIssueComment) for repo,
// routed through the multi-forge registry (deps.Forges) instead of
// hardcoding GitHub. Mirrors the read-path resolution already used by
// handleQuickMode / handleIssuesMode / forgeAPIRepoHits: ForURL(repo),
// falling back to the registered GitHub forge for a bare/ambiguous slug.
// Shared by reviewPRPost (tool_review_pr_post_impl.go) and handlePushReview
// (push_review.go) — the two dry_run=false write entry points.
//
// GitLab is detected and rejected up front with a clear, repo-scoped error
// instead of being silently routed to GitHub (the bug this fixes) or routed
// to GitLabForge's three-stub Poster methods, which return an opaque
// "not implemented" with no repo context.
func resolvePostForge(deps analyze.Deps, repo string) (forge.Poster, error) {
	if forge.DetectForge(repo) == forge.GitLab {
		return nil, fmt.Errorf("posting to GitLab is not yet implemented for %s", repo)
	}
	if deps.Forges == nil {
		return nil, fmt.Errorf("no forge registry configured")
	}
	f := deps.Forges.ForURL(repo)
	if f == nil {
		f = deps.Forges.Get(forge.GitHub)
	}
	if f == nil {
		return nil, fmt.Errorf("no forge configured for %s", repo)
	}
	p, ok := f.(forge.Poster)
	if !ok {
		return nil, fmt.Errorf("forge %s does not support posting for %s", f.Kind(), repo)
	}
	return p, nil
}
