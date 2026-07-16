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
	taintInput := oxcodes.TaintInput{
		Root:        root,
		Language:    input.Language,
		MaxResults:  dataflowMaxResults,
		FileGlob:    input.FileGlob,
		ExcludeGlob: input.ExcludeGlob,
	}
	// Convert MCP taint rules to oxcodes types. When nil, ox-codes uses
	// its built-in SQL/command injection rules.
	if len(input.Rules) > 0 {
		taintInput.Rules = make([]oxcodes.TaintRule, len(input.Rules))
		for i, r := range input.Rules {
			taintInput.Rules[i] = convertTaintRule(r)
		}
	}
	result, err := client.DataflowTaint(ctx, taintInput)
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

// convertTaintRule converts MCP TaintRuleInput to oxcodes.TaintRule.
func convertTaintRule(r TaintRuleInput) oxcodes.TaintRule {
	sources := make([]oxcodes.TaintSource, len(r.Sources))
	for i, s := range r.Sources {
		sources[i] = oxcodes.TaintSource{Pattern: s.Pattern, Tag: s.Tag}
	}
	sinks := make([]oxcodes.TaintSink, len(r.Sinks))
	for i, s := range r.Sinks {
		sinks[i] = oxcodes.TaintSink{
			Pattern:     s.Pattern,
			ArgIndex:    s.ArgIndex,
			CWE:         s.CWE,
			Description: s.Description,
		}
	}
	sanitizers := make([]oxcodes.Sanitizer, len(r.Sanitizers))
	for i, s := range r.Sanitizers {
		sanitizers[i] = oxcodes.Sanitizer{Pattern: s.Pattern}
	}
	return oxcodes.TaintRule{
		ID:         r.ID,
		Sources:    sources,
		Sinks:      sinks,
		Sanitizers: sanitizers,
		Severity:   r.Severity,
	}
}
