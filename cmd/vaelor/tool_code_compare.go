package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"time"

	"github.com/anatolykoptev/go-kit/embed"
	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/anatolykoptev/vaelor/internal/compare"
	"github.com/anatolykoptev/vaelor/internal/mcpmeta"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// CodeCompareInput is the input schema for the code_compare tool.
type CodeCompareInput struct {
	RepoA    string `json:"repo_a" jsonschema_description:"First repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`
	RepoB    string `json:"repo_b" jsonschema_description:"Second repository: GitHub slug (owner/repo), full GitHub URL, or absolute local host path (e.g. /home/user/src/project)"`
	Query    string `json:"query,omitempty" jsonschema_description:"What to compare — quality aspects, architectural patterns, specific concerns (default: general comparison)"`
	Focus    string `json:"focus,omitempty" jsonschema_description:"Subdirectory path to limit comparison scope (e.g. internal/auth, pkg/api), or space-separated keywords (e.g. 'auth handler'). Use query for topic focus."`
	Language string `json:"language,omitempty" jsonschema_description:"Limit comparison to files of this language (e.g. go, python, rust)"`
	MaxBytes int    `json:"max_bytes,omitempty" jsonschema_description:"Response budget in bytes (default 8192). When the response exceeds this, the ranked head is returned with a continuation footer."`
}

// registerCodeCompare registers the code_compare MCP tool.
func registerCodeCompare(server *mcp.Server, cfg Config, deps analyze.Deps, semDeps *SemanticDeps, graphStore *codegraph.Store) {
	outputDir := cfg.OutputDir

	addTool(server, &mcp.Tool{
		Name: "code_compare",
		Description: "Compare two code repositories to find the better implementation. " +
			"Analyzes architecture, code quality, patterns, and identifies missing features. " +
			"Returns XML with quality verdicts, coverage gaps, architecture insights, " +
			"metrics, and actionable recommendations. Works cross-language.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodeCompareInput) (*mcp.CallToolResult, error) {
		t0 := time.Now()
		if input.RepoA == "" || input.RepoB == "" {
			return errResult("repo_a and repo_b are required"), nil
		}
		if input.Query == "" {
			input.Query = "Compare architecture, code quality, patterns, and identify missing features"
		}

		rootA, cleanupA, err := resolveRoot(ctx, input.RepoA, "", deps)
		if err != nil {
			return errResult(fmt.Sprintf("resolve repo_a: %s", err)), nil
		}
		defer cleanupA()

		rootB, cleanupB, err := resolveRoot(ctx, input.RepoB, "", deps)
		if err != nil {
			return errResult(fmt.Sprintf("resolve repo_b: %s", err)), nil
		}
		defer cleanupB()

		var embedClient *embed.Client
		if semDeps != nil {
			embedClient = semDeps.Client
		}

		// Soft deadline: 25s default, below the 95s client timeout. On
		// expiry, compare.CompareRepos returns a partial result (the
		// compare package checks ctx.Err() at natural boundaries).
		softCtx, softCancel := mcpmeta.SoftDeadline(ctx)
		defer softCancel()

		result, err := compare.CompareRepos(softCtx, compare.CompareInput{
			RootA:       rootA,
			RootB:       rootB,
			Query:       input.Query,
			OxCodes:     deps.OxCodes,
			EmbedClient: embedClient,
			GraphStore:  graphStore,
			ParseCache:  deps.ParseCache,
			Opts: compare.SnapshotOpts{
				Focus:    input.Focus,
				Language: input.Language,
			},
		}, deps.LLM)

		elapsed := time.Since(t0)
		// If the soft deadline fired, compare returns either a partial
		// result (err == nil but incomplete) or a context-canceled error.
		// In the error case, we have no result to return — surface a
		// clear partial message instead of nothing.
		if softCtx.Err() != nil {
			logSoftDeadlineHit("code_compare", elapsed)
			if err != nil {
				return softDeadlineResult(
					fmt.Sprintf("code_compare: timed out after %s — snapshots/metrics computed partially, LLM analysis skipped. Retry with a narrower focus or language filter.", elapsed.Round(time.Second)),
					"LLM analysis, route diff, enrichment (soft deadline)",
					elapsed,
				), nil
			}
			// Partial result available — render it with a partial footer.
			xmlOut := buildCompareXML(result)
			data, mErr := xml.Marshal(xmlOut)
			if mErr != nil {
				return softDeadlineResult(
					fmt.Sprintf("code_compare: timed out after %s — partial result marshal failed: %s", elapsed.Round(time.Second), mErr),
					"XML marshal of partial result",
					elapsed,
				), nil
			}
			text := xml.Header + string(data)
			if input.MaxBytes > 0 {
				text = mcpmeta.Shape(text, budgetOverride(input.MaxBytes), "narrow with focus= or language=")
			}
			text += mcpmeta.PartialFooter("some enrichment/LLM stages skipped (soft deadline)")
			text = mcpmeta.AppendTook(text, elapsed)
			return textResult(text), nil
		}

		if err != nil {
			return errResult(fmt.Sprintf("compare: %s", err)), nil
		}

		return xmlMarshalFileResult(buildCompareXML(result), "code_compare", outputDir), nil
	})
}
