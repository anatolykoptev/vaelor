package jaegerclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// ErrTraceNotFound is returned by GetTrace when Jaeger returns an empty
// data array for the given trace ID. Callers can check via errors.Is.
var ErrTraceNotFound = errors.New("trace not found")

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

// Process is one Jaeger process metadata (per-trace).
type Process struct {
	ServiceName string    `json:"serviceName"`
	Tags        []SpanTag `json:"tags,omitempty"`
}

// Trace is a Jaeger distributed trace containing one or more spans.
type Trace struct {
	TraceID   string             `json:"traceID"`
	Spans     []Span             `json:"spans"`
	Processes map[string]Process `json:"processes,omitempty"`
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
	// Limit caps the number of traces returned (0 = server default applies).
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
		tagsJSON, err := json.Marshal(p.Tags)
		if err != nil {
			// Cannot happen for map[string]string but check defensively.
			return nil, fmt.Errorf("encode tags: %w", err)
		}
		v.Set("tags", string(tagsJSON))
	}

	var resp tracesResponse
	if err := c.getJSON(ctx, "/api/traces?"+v.Encode(), &resp); err != nil {
		return nil, fmt.Errorf("find traces: %w", err)
	}
	return resp.Data, nil
}

// GetTrace fetches a single trace by its ID from /api/traces/{id}.
// Returns ErrTraceNotFound (wrapped) when the trace is not found.
func (c *Client) GetTrace(ctx context.Context, traceID string) (*Trace, error) {
	if traceID == "" {
		return nil, fmt.Errorf("traceID is required")
	}
	var resp tracesResponse
	if err := c.getJSON(ctx, "/api/traces/"+url.PathEscape(traceID), &resp); err != nil {
		return nil, fmt.Errorf("get trace: %w", err)
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("trace %q: %w", traceID, ErrTraceNotFound)
	}
	return &resp.Data[0], nil
}
