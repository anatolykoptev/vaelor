package main

import (
	"context"
	"fmt"

	"github.com/anatolykoptev/go-code/internal/oxcodes"
)

type securityResult struct {
	*xmlDfSecurity
	filesAnalyzed int
	durationMS    int64
}

func runSecurityAnalysis(ctx context.Context, client *oxcodes.Client, root string, input DataflowInput) (*securityResult, error) {
	result, err := client.DataflowTaint(ctx, oxcodes.TaintInput{
		Root:        root,
		Language:    input.Language,
		MaxResults:  dataflowMaxResults,
		FileGlob:    input.FileGlob,
		ExcludeGlob: input.ExcludeGlob,
	})
	if err != nil {
		return nil, fmt.Errorf("security: %w", err)
	}

	findings := make([]xmlSecurityFinding, len(result.Findings))
	for i, f := range result.Findings {
		findings[i] = xmlSecurityFinding{
			RuleID:   f.RuleID,
			Severity: f.Severity,
			File:     f.File,
			Line:     f.Sink.Span.StartLine,
			CWE:      f.Sink.CWE,
			Message:  f.Message,
		}
	}

	return &securityResult{
		xmlDfSecurity: &xmlDfSecurity{Count: result.TotalFindings, Findings: findings},
		filesAnalyzed: result.FilesAnalyzed,
		durationMS:    result.DurationMS,
	}, nil
}
