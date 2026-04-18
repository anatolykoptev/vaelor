package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/learnings"
	"github.com/anatolykoptev/go-code/internal/review"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ReviewPRInput struct {
	Repo     string `json:"repo" jsonschema_description:"Repository: GitHub slug (owner/repo) or full URL"`
	PR       int    `json:"pr" jsonschema_description:"Pull request number"`
	Depth    int    `json:"depth,omitempty" jsonschema_description:"Impact traversal depth (default 2, max 5)"`
	Language string `json:"language,omitempty" jsonschema_description:"Limit to files of this language"`
}

func registerReviewPR(server *mcp.Server, _ Config, deps analyze.Deps) {
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "review_pr",
		Description: "Review a pull request: fetches PR metadata and diff, " +
			"then runs differential impact analysis on all changes. " +
			"Returns changed symbols, blast radius, untested code, and risk guidance.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ReviewPRInput) (*mcp.CallToolResult, error) {
		return handleReviewPR(ctx, input, deps)
	})
}

func handleReviewPR(ctx context.Context, input ReviewPRInput, deps analyze.Deps) (*mcp.CallToolResult, error) {
	if input.Repo == "" {
		return errResult("repo is required"), nil
	}
	if input.PR <= 0 {
		return errResult("pr number is required"), nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve repo: %s", err)), nil
	}
	defer cleanup()

	base, err := fetchPRBase(ctx, root, input.PR)
	if err != nil {
		base = "origin/main"
	}

	depth := input.Depth
	if depth <= 0 {
		depth = defaultReviewDepth
	}
	if depth > maxReviewDepth {
		depth = maxReviewDepth
	}

	result, err := review.DeltaReview(ctx, review.DeltaInput{
		Root:     root,
		Base:     base,
		Depth:    depth,
		Language: input.Language,
		OxCodes:  deps.OxCodes,
	})
	if err != nil {
		return errResult(fmt.Sprintf("review: %s", err)), nil
	}

	findings := applyPolicy(ctx, root, result)
	for _, f := range findings {
		result.Risk.Flags = append(result.Risk.Flags, fmt.Sprintf("policy:%s %s:%d %s", f.Rule, f.Path, f.Line, f.Message))
	}

	// Persist learnings and look up prior findings
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		store, err := learnings.New(ctx, dsn, nil)
		if err == nil {
			defer store.Close()
			slug := input.Repo
			for _, cs := range result.ChangedSymbols {
				// Lookup hints
				prior, _ := store.Nearest(ctx, slug, cs.Symbol.Name, 3)
				for _, p := range prior {
					result.Risk.Suggestions = append(result.Risk.Suggestions,
						fmt.Sprintf("prior review on %s: %s (%s)", p.Symbol, p.Flag, p.PRURL))
				}
				// Record current risk level from impact analysis.
				_ = store.Upsert(ctx, learnings.Record{
					Repo: slug, Symbol: cs.Symbol.Name,
					RiskLevel: result.Risk.RiskLevel,
					Flag:      strings.Join(result.Risk.Flags, ";"),
					PRURL:     fmt.Sprintf("https://github.com/%s/pull/%d", slug, input.PR),
				})
			}
		}
	}

	resp := buildDeltaXML(result)
	resp.Tool = "review_pr"
	resp.Verdict = deriveVerdict(result)
	data, err := xml.MarshalIndent(resp, "", "  ")
	if err != nil {
		return errResult(fmt.Sprintf("marshal: %s", err)), nil
	}

	return textResult(string(data)), nil
}

// deriveVerdict maps a delta review's risk guidance into a structured merge
// verdict. It recognises the three levels produced by review.classifyRisk
// ("low", "medium", "high"); any future level falls through to nil so the
// verdict is simply omitted rather than emitting a misleading default.
func deriveVerdict(r *review.DeltaResult) *xmlVerdict {
	if r == nil {
		return nil
	}
	level := r.Risk.RiskLevel
	switch level {
	case "low":
		return &xmlVerdict{
			CanReplace: "yes",
			Reason:     "low risk",
		}
	case "medium":
		reason := "medium risk"
		if len(r.Risk.Flags) > 0 {
			reason = r.Risk.Flags[0]
		}
		return &xmlVerdict{
			CanReplace: "partial",
			Reason:     reason,
		}
	case "high":
		return &xmlVerdict{
			CanReplace: "no",
			Reason:     "high risk",
			Blockers:   r.Risk.Flags,
		}
	default:
		return nil
	}
}

func fetchPRBase(ctx context.Context, root string, prNumber int) (string, error) {
	ref := fmt.Sprintf("pull/%d/head", prNumber)
	_, err := review.GitExec(ctx, root, "fetch", "origin", ref)
	if err != nil {
		return "", fmt.Errorf("fetch PR ref: %w", err)
	}
	out, err := review.GitExec(ctx, root, "merge-base", "FETCH_HEAD", "origin/main")
	if err != nil {
		out, err = review.GitExec(ctx, root, "merge-base", "FETCH_HEAD", "origin/master")
		if err != nil {
			return "", fmt.Errorf("merge-base: %w", err)
		}
	}
	return strings.TrimSpace(out), nil
}
