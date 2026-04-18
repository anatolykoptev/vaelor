package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/forge"
	"github.com/anatolykoptev/go-code/internal/langutil"
	"github.com/anatolykoptev/go-code/internal/learnings"
	"github.com/anatolykoptev/go-code/internal/policy"
	"github.com/anatolykoptev/go-code/internal/review"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ReviewPRPostInput struct {
	Repo  string `json:"repo" jsonschema_description:"Repository: owner/repo slug"`
	PR    int    `json:"pr" jsonschema_description:"Pull request number"`
	Depth int    `json:"depth,omitempty" jsonschema_description:"Impact depth (default 2, max 5)"`
	Event string `json:"event,omitempty" jsonschema_description:"GitHub review event: COMMENT (default), REQUEST_CHANGES, APPROVE"`
	Dry   bool   `json:"dry,omitempty" jsonschema_description:"Preview only — do not post"`
}

// learningsPersister is the narrow write-side interface handleReviewPRPost
// uses to record review outcomes. *learnings.Store satisfies it; tests supply a spy.
// Kept local to cmd/go-code so the production path keeps using the Store
// directly via deps.Learnings (same pattern as compound.LearningsLookup).
type learningsPersister interface {
	Upsert(ctx context.Context, r learnings.Record) error
}

func registerReviewPRPost(server *mcp.Server, _ Config, deps analyze.Deps) {
	mcpserver.AddTool(server, &mcp.Tool{
		Name:        "review_pr_post",
		Description: "Run review_pr and post the result as a PR review on GitHub. Requires GITHUB_TOKEN.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input ReviewPRPostInput) (*mcp.CallToolResult, error) {
		return handleReviewPRPost(ctx, input, deps)
	})
}

func handleReviewPRPost(ctx context.Context, input ReviewPRPostInput, deps analyze.Deps) (*mcp.CallToolResult, error) {
	if input.Repo == "" || input.PR <= 0 {
		return errResult("repo and pr are required"), nil
	}

	root, cleanup, err := resolveRoot(ctx, input.Repo, "", deps)
	if err != nil {
		return errResult(fmt.Sprintf("resolve: %s", err)), nil
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
		Root: root, Base: base, Depth: depth, OxCodes: deps.OxCodes,
	})
	if err != nil {
		return errResult(fmt.Sprintf("review: %s", err)), nil
	}
	findings := applyPolicy(ctx, root, result)
	body, comments := renderReview(result, findings)

	if input.Dry {
		// Dry-run: preview only, no side effects (no post, no persist).
		return textResult(body + "\n\n--- inline comments ---\n" + fmt.Sprintf("%+v", comments)), nil
	}

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return errResult("GITHUB_TOKEN not set"), nil
	}
	g := forge.NewGitHubForge(token)
	event := input.Event
	if event == "" {
		event = "COMMENT"
	}
	url, err := g.PostReview(ctx, input.Repo, input.PR, forge.ReviewPayload{
		Body: body, Event: event, Comments: comments,
	})
	if err != nil {
		return errResult(fmt.Sprintf("post: %s", err)), nil
	}

	// Persist a review outcome per changed symbol so future reviews on the same
	// repo/symbol can surface prior findings. Best-effort: Upsert failures
	// are logged but never fail the tool. Use typed-nil-interface guard
	// (deps.Learnings is a concrete *Store, assign only when non-nil).
	if deps.Learnings != nil {
		persistChangedSymbols(
			ctx, deps.Learnings,
			input.Repo, url,
			outcomeFromEvent(event),
			root, result.ChangedSymbols, findings,
		)
	}
	return textResult("posted: " + url), nil
}

// outcomeFromEvent maps a GitHub review event to a canonical review-outcome
// string persisted in the learnings store. Unknown events fall back to
// "neutral" so we never emit an invalid review_outcome column value.
func outcomeFromEvent(event string) string {
	switch strings.ToUpper(event) {
	case "APPROVE":
		return "good"
	case "REQUEST_CHANGES":
		return "bad"
	default:
		return "neutral"
	}
}

// persistChangedSymbols writes one learnings.Record per changed symbol.
//
// Flag/Note derivation: for each symbol, the first policy.Finding whose path
// matches the symbol's file AND whose line falls within the symbol's range
// wins; otherwise Flag defaults to the ChangeType (added/modified/removed)
// and Note is empty. This keeps the record informative even when no policy
// rule fired.
//
// Upsert errors are swallowed and logged via slog.Warn — persistence is
// best-effort and MUST NOT fail the posting workflow.
func persistChangedSymbols(
	ctx context.Context,
	p learningsPersister,
	repo, prURL, outcome, root string,
	symbols []review.ChangedSymbol,
	findings []policy.Finding,
) {
	if p == nil {
		return
	}
	for _, cs := range symbols {
		if cs.Symbol == nil {
			continue
		}
		flag, note := deriveFlagNote(cs, findings, root)
		rec := learnings.Record{
			Repo:          repo,
			Symbol:        cs.Symbol.Name,
			ReviewOutcome: outcome,
			Flag:          flag,
			Note:          note,
			PRURL:         prURL,
		}
		if err := p.Upsert(ctx, rec); err != nil {
			slog.Warn("learnings upsert failed",
				slog.String("repo", repo),
				slog.String("symbol", cs.Symbol.Name),
				slog.Any("error", err),
			)
		}
	}
}

// deriveFlagNote picks the first policy finding that lands inside the
// symbol's line range (in the symbol's own file), returning its rule+message.
// Falls back to (ChangeType, "") when no finding matches.
func deriveFlagNote(cs review.ChangedSymbol, findings []policy.Finding, root string) (string, string) {
	sym := cs.Symbol
	if sym != nil {
		symRel := langutil.RelPath(sym.File, root)
		for _, f := range findings {
			if f.Path == "" || f.Line == 0 {
				continue
			}
			if f.Path != symRel {
				continue
			}
			if f.Line >= int(sym.StartLine) && f.Line <= int(sym.EndLine) {
				return f.Rule, f.Message
			}
		}
	}
	return string(cs.ChangeType), ""
}

// renderReview converts a DeltaResult + policy findings into a markdown body
// and a slice of inline comments.
func renderReview(r *review.DeltaResult, findings []policy.Finding) (body string, comments []forge.InlineComment) {
	var b strings.Builder
	fmt.Fprintf(&b, "## go-code review\n\n**Risk:** %s (score %.2f)\n\n", r.Risk.RiskLevel, r.Risk.RiskScore)
	if len(r.Risk.Flags) > 0 {
		b.WriteString("### Flags\n")
		for _, f := range r.Risk.Flags {
			fmt.Fprintf(&b, "- %s\n", f)
		}
	}
	if len(r.UntestedSymbols) > 0 {
		b.WriteString("\n### Untested changed symbols\n")
		for _, s := range r.UntestedSymbols {
			fmt.Fprintf(&b, "- `%s`\n", s)
		}
	}
	if len(r.Risk.Suggestions) > 0 {
		b.WriteString("\n### Suggestions\n")
		for _, s := range r.Risk.Suggestions {
			fmt.Fprintf(&b, "- %s\n", s)
		}
	}
	for _, f := range findings {
		if f.Path == "" || f.Line == 0 {
			continue
		}
		comments = append(comments, forge.InlineComment{
			Path: f.Path, Line: f.Line,
			Body: fmt.Sprintf("**[%s]** %s", f.Rule, f.Message),
		})
	}
	return b.String(), comments
}
