// Package oxcodes — dataflow analysis types and client methods.
package oxcodes

import "context"

// DataflowInput is the request for POST /dataflow/analyze.
type DataflowInput struct {
	Root        string `json:"root"`
	Language    string `json:"language"`
	MaxResults  int    `json:"max_results"`
	FileGlob    string `json:"file_glob,omitempty"`
	ExcludeGlob string `json:"exclude_glob,omitempty"`
}

// DataflowResponse is the response from POST /dataflow/analyze.
type DataflowResponse struct {
	Findings      []DataflowFinding `json:"findings"`
	TotalFindings int               `json:"total_findings"`
	FilesAnalyzed int               `json:"files_analyzed"`
	Truncated     bool              `json:"truncated"`
	DurationMS    int64             `json:"duration_ms"`
}

// DataflowFinding represents a single quality finding (dead store, unused var, etc.).
type DataflowFinding struct {
	Kind     string       `json:"kind"`
	Severity string       `json:"severity"`
	Message  string       `json:"message"`
	File     string       `json:"file"`
	Span     DataflowSpan `json:"span"`
	Variable string       `json:"variable"`
}

// DataflowSpan represents a byte/line range in source code.
type DataflowSpan struct {
	StartByte int `json:"start_byte"`
	EndByte   int `json:"end_byte"`
	StartLine int `json:"start_line"`
	EndLine   int `json:"end_line"`
}

// TaintInput is the request for POST /dataflow/taint.
type TaintInput struct {
	Root        string      `json:"root"`
	Language    string      `json:"language"`
	Rules       []TaintRule `json:"rules,omitempty"`
	MaxResults  int         `json:"max_results"`
	FileGlob    string      `json:"file_glob,omitempty"`
	ExcludeGlob string      `json:"exclude_glob,omitempty"`
}

// TaintRule defines a custom taint-tracking rule.
type TaintRule struct {
	ID         string        `json:"id"`
	Sources    []TaintSource `json:"sources"`
	Sinks      []TaintSink   `json:"sinks"`
	Sanitizers []Sanitizer   `json:"sanitizers,omitempty"`
	Severity   string        `json:"severity"`
}

// TaintSource defines where tainted data originates.
type TaintSource struct {
	Pattern string `json:"pattern"`
	Tag     string `json:"tag"`
}

// TaintSink defines where tainted data must not flow.
type TaintSink struct {
	Pattern     string `json:"pattern"`
	ArgIndex    int    `json:"arg_index"`
	CWE         string `json:"cwe"`
	Description string `json:"description"`
}

// Sanitizer defines a function that neutralizes tainted data.
type Sanitizer struct {
	Pattern string `json:"pattern"`
}

// TaintResponse is the response from POST /dataflow/taint.
type TaintResponse struct {
	Findings      []TaintFinding `json:"findings"`
	TotalFindings int            `json:"total_findings"`
	FilesAnalyzed int            `json:"files_analyzed"`
	Truncated     bool           `json:"truncated"`
	DurationMS    int64          `json:"duration_ms"`
}

// TaintFinding represents a single taint-tracking finding.
type TaintFinding struct {
	RuleID   string          `json:"rule_id"`
	Source   TaintSourceInfo `json:"source"`
	Sink     TaintSinkInfo   `json:"sink"`
	Severity string          `json:"severity"`
	Message  string          `json:"message"`
	File     string          `json:"file"`
}

// TaintSourceInfo describes the source location in a taint finding.
type TaintSourceInfo struct {
	Function string       `json:"function"`
	Span     DataflowSpan `json:"span"`
}

// TaintSinkInfo describes the sink location in a taint finding.
type TaintSinkInfo struct {
	Function string       `json:"function"`
	Span     DataflowSpan `json:"span"`
	ArgIndex int          `json:"arg_index"`
	CWE      string       `json:"cwe"`
}

// DataflowAnalyze calls POST /dataflow/analyze.
func (c *Client) DataflowAnalyze(ctx context.Context, input DataflowInput) (*DataflowResponse, error) {
	var result DataflowResponse
	if err := c.doPost(ctx, "/dataflow/analyze", input, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// DataflowTaint calls POST /dataflow/taint.
func (c *Client) DataflowTaint(ctx context.Context, input TaintInput) (*TaintResponse, error) {
	var result TaintResponse
	if err := c.doPost(ctx, "/dataflow/taint", input, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
