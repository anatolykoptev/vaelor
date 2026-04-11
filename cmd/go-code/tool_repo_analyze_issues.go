package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/forge"
	"github.com/anatolykoptev/go-code/internal/prompts"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// handleIssuesMode searches GitHub issues or pull requests via the Issues Search API.
func handleIssuesMode(ctx context.Context, input RepoAnalyzeInput, deps analyze.Deps) (*mcp.CallToolResult, error) {
	kind := input.Type
	if isLocalPath(input.Repo) {
		return errResult(kind + " search requires a GitHub repo (owner/repo), not a local path"), nil
	}
	repos := resolveQuickRepos(input)
	if len(repos) == 0 {
		return errResult(fmt.Sprintf("repo is required for %s search", kind)), nil
	}

	repoSlug := strings.Join(repos, ", ")

	var qb strings.Builder
	qb.WriteString("is:")
	qb.WriteString(kind)
	for _, r := range repos {
		qb.WriteString(" repo:")
		qb.WriteString(r)
	}
	if input.Query != "" {
		qb.WriteString(" ")
		qb.WriteString(input.Query)
	}

	fi := deps.Forges.ForURL(input.Repo)
	if fi == nil {
		fi = deps.Forges.Get(forge.GitHub)
	}
	if fi == nil {
		return errResult("no forge configured for issues search"), nil
	}

	issues, err := fi.SearchIssues(ctx, qb.String())
	if err != nil {
		return errResult(fmt.Sprintf("issues search: %s", err)), nil
	}

	if len(issues) == 0 {
		return textResult(fmt.Sprintf("No %ss found for query: %s", kind, input.Query)), nil
	}

	var sb strings.Builder
	for i, item := range issues {
		state := item.State
		if item.MergedAt != "" {
			state = "merged"
		}
		fmt.Fprintf(&sb, "[%d] #%d %s\nURL: %s | State: %s | Author: %s | Comments: %d\n",
			i+1, item.Number, item.Title, item.URL, state, item.Author, item.Comments)
		if len(item.Labels) > 0 {
			fmt.Fprintf(&sb, "Labels: %s\n", strings.Join(item.Labels, ", "))
		}
		if item.Body != "" {
			body := item.Body
			const maxBodyLen = 500
			if len(body) > maxBodyLen {
				body = body[:maxBodyLen] + "..."
			}
			fmt.Fprintf(&sb, "Body: %s\n", body)
		}
		sb.WriteString("\n")
	}

	if input.Mode == modeRaw {
		return textResult(fmt.Sprintf("Found %d %ss:\n\n%s", len(issues), kind, sb.String())), nil
	}

	summary, llmErr := deps.LLM.Complete(ctx, prompts.SystemPromptIssuesAnalysis,
		fmt.Sprintf("Query: %s\n\n%s results:\n%s", input.Query, kind, sb.String()))
	if llmErr != nil {
		return textResult(fmt.Sprintf("Found %d %ss (LLM unavailable):\n\n%s", len(issues), kind, sb.String())), nil
	}

	return textResult(fmt.Sprintf("# %s Search: %s\nRepo: %s | Found: %d\n\n%s",
		capitalizeFirst(kind), input.Query, repoSlug, len(issues), summary)), nil
}
