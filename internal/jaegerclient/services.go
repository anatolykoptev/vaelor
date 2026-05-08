package jaegerclient

import (
	"context"
	"fmt"
)

type servicesResponse struct {
	Data  []string `json:"data"`
	Total int      `json:"total"`
}

// ListServices returns the list of service names Jaeger has observed.
func (c *Client) ListServices(ctx context.Context) ([]string, error) {
	var resp servicesResponse
	if err := c.getJSON(ctx, "/api/services", &resp); err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	if resp.Data == nil {
		return []string{}, nil
	}
	return resp.Data, nil
}
