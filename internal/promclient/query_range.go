package promclient

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// QueryRangeResponse is the top-level response from the Prometheus /api/v1/query_range endpoint.
type QueryRangeResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string         `json:"resultType"`
		Result     []SeriesResult `json:"result"`
	} `json:"data"`
}

// SeriesResult holds one time series from a matrix query result.
type SeriesResult struct {
	Metric map[string]string `json:"metric"`
	// Values contains [timestamp, value] pairs where timestamp is a float64
	// and value is a string-encoded float.
	Values [][2]any `json:"values"`
}

// QueryRange executes a range query against the Prometheus HTTP API v1.
// step must be >= 1µs and end must be after start.
func (c *Client) QueryRange(ctx context.Context, query string, start, end time.Time, step time.Duration) (*QueryRangeResponse, error) {
	if step <= 0 {
		return nil, fmt.Errorf("step must be > 0")
	}
	// Reject sub-microsecond steps that would round to 0 in seconds-float encoding.
	if step < time.Microsecond {
		return nil, fmt.Errorf("step too small: %v", step)
	}
	if !end.After(start) {
		return nil, fmt.Errorf("end (%v) must be after start (%v)", end, start)
	}

	v := url.Values{}
	v.Set("query", query)
	v.Set("start", strconv.FormatInt(start.Unix(), 10))
	v.Set("end", strconv.FormatInt(end.Unix(), 10))
	v.Set("step", strconv.FormatFloat(step.Seconds(), 'f', -1, 64))

	path := "/api/v1/query_range?" + v.Encode()
	var resp QueryRangeResponse
	if err := c.GetJSON(ctx, path, &resp); err != nil {
		return nil, fmt.Errorf("query_range: %w", err)
	}
	if resp.Status != "success" {
		return nil, fmt.Errorf("prometheus returned status %q", resp.Status)
	}
	return &resp, nil
}
