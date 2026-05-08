package jaegerclient

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// SpanTag is a key-value pair attached to a span.
type SpanTag struct {
	Key   string `json:"key"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

// Span represents a single Jaeger span within a trace.
type Span struct {
	SpanID        string    `json:"spanID"`
	OperationName string    `json:"operationName"`
	Duration      int64     `json:"duration"`
	Tags          []SpanTag `json:"tags"`
}

// Trace is a Jaeger distributed trace containing one or more spans.
type Trace struct {
	TraceID   string                    `json:"traceID"`
	Spans     []Span                    `json:"spans"`
	Processes map[string]map[string]any `json:"processes,omitempty"`
}

type tracesResponse struct {
	Data  []Trace `json:"data"`
	Total int     `json:"total"`
}

// FindTracesParams holds query parameters for FindTraces.
type FindTracesParams struct {
	// Service is required — the Jaeger service name to search.
	Service string
	// Operation filters to a specific RPC/endpoint name (optional).
	Operation string
	// Tags is an optional key→value map for span tag filtering.
	Tags map[string]string
	// StartTime and EndTime define the time window (zero = omit).
	StartTime time.Time
	EndTime   time.Time
	// Limit caps the number of traces returned (zero = omit, server default applies).
	Limit int
}

// FindTraces queries the Jaeger /api/traces endpoint and returns matching traces.
func (c *Client) FindTraces(ctx context.Context, p FindTracesParams) ([]Trace, error) {
	if p.Service == "" {
		return nil, fmt.Errorf("FindTracesParams.Service is required")
	}

	v := url.Values{}
	v.Set("service", p.Service)
	if p.Operation != "" {
		v.Set("operation", p.Operation)
	}
	if p.Limit > 0 {
		v.Set("limit", strconv.Itoa(p.Limit))
	}
	if !p.StartTime.IsZero() {
		v.Set("start", strconv.FormatInt(p.StartTime.UnixMicro(), 10))
	}
	if !p.EndTime.IsZero() {
		v.Set("end", strconv.FormatInt(p.EndTime.UnixMicro(), 10))
	}
	if len(p.Tags) > 0 {
		v.Set("tags", buildTagsJSON(p.Tags))
	}

	var resp tracesResponse
	if err := c.getJSON(ctx, "/api/traces?"+v.Encode(), &resp); err != nil {
		return nil, fmt.Errorf("find traces: %w", err)
	}
	return resp.Data, nil
}

// GetTrace fetches a single trace by its ID from /api/traces/{id}.
// Returns an error when the trace is not found.
func (c *Client) GetTrace(ctx context.Context, traceID string) (*Trace, error) {
	if traceID == "" {
		return nil, fmt.Errorf("traceID is required")
	}
	var resp tracesResponse
	if err := c.getJSON(ctx, "/api/traces/"+url.PathEscape(traceID), &resp); err != nil {
		return nil, fmt.Errorf("get trace: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("trace %q not found", traceID)
	}
	return &resp.Data[0], nil
}

// buildTagsJSON serialises a string map into the compact JSON object shape
// Jaeger expects for its ?tags= query parameter, e.g. {"error":"true"}.
// It avoids importing encoding/json to keep the dependency minimal.
func buildTagsJSON(tags map[string]string) string {
	var sb []byte
	sb = append(sb, '{')
	first := true
	for k, val := range tags {
		if !first {
			sb = append(sb, ',')
		}
		first = false
		sb = append(sb, '"')
		sb = append(sb, k...)
		sb = append(sb, '"', ':', '"')
		sb = append(sb, val...)
		sb = append(sb, '"')
	}
	sb = append(sb, '}')
	return string(sb)
}
