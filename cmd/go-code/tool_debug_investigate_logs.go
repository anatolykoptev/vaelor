package main

import (
	"context"
	"fmt"
	"time"

	"github.com/anatolykoptev/go-code/internal/dozorclient"
	"github.com/anatolykoptev/go-code/internal/investigate"
)

// runLogsPhase queries the dozor sidecar for recent log lines (server applies
// default grep: panic|fatal|error when grep param is empty) in the investigation
// window. Top-N most recent lines are attached to the result as LogExcerpts.
//
// If dozor is nil (DOZOR_URL not configured or empty), the phase is a no-op and
// nil is returned. Errors are appended to diags.Warnings without failing the
// investigation — logs are supplemental evidence, not a hard dependency.
func runLogsPhase(ctx context.Context, dozor *dozorclient.Client, input DebugInvestigateInput, start, end time.Time, diags *investigate.Diagnostics) []investigate.LogExcerpt {
	if dozor == nil {
		return nil
	}
	resp, err := dozor.GetLogs(ctx, input.Service, start, end, "", 20)
	if err != nil {
		diags.Warnings = append(diags.Warnings, fmt.Sprintf("logs phase: %v", err))
		return nil
	}
	diags.LogsFetched = len(resp.Lines)
	excerpts := make([]investigate.LogExcerpt, 0, len(resp.Lines))
	for _, l := range resp.Lines {
		excerpts = append(excerpts, investigate.LogExcerpt{
			Ts:    l.Ts,
			Level: l.Level,
			Msg:   l.Msg,
			Raw:   l.Raw,
		})
	}
	return excerpts
}
