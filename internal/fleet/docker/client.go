package docker

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const (
	// defaultSocketPath is the standard Docker Engine unix socket location.
	defaultSocketPath = "/var/run/docker.sock"

	// defaultTimeout is the per-request deadline applied via context and conn.SetDeadline.
	defaultTimeout = 10 * time.Second

	// maxResponseBytes is the cap on the response body. Responses exceeding this
	// limit are rejected with ErrEngineError to avoid unbounded memory growth.
	maxResponseBytes = 8 * 1024 * 1024 // 8 MiB
)

// client is a minimal HTTP/1.1 client for the Docker Engine unix socket.
// The dial function is a seam that tests replace with net.Pipe-backed fakes.
type client struct {
	socketPath string
	dial       func(ctx context.Context, network, addr string) (net.Conn, error)
	timeout    time.Duration
}

// newClient constructs a client from the driver's configuration.
func newClient(socketPath string, dial func(ctx context.Context, network, addr string) (net.Conn, error), timeout time.Duration) *client {
	return &client{
		socketPath: socketPath,
		dial:       dial,
		timeout:    timeout,
	}
}

// containerJSON is the relevant subset of the Docker Engine containers/json response.
type containerJSON struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	ImageID string            `json:"ImageID"`
	Created int64             `json:"Created"`
	State   string            `json:"State"`
	Labels  map[string]string `json:"Labels"`
}

// listContainers calls GET /containers/json?all=0 and returns the raw JSON objects.
func (c *client) listContainers(ctx context.Context) ([]containerJSON, error) {
	// Per-request timeout: apply to both context and connection deadline.
	timeout := c.timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	dialCtx, dialCancel := context.WithTimeout(ctx, timeout)
	defer dialCancel()

	conn, err := c.dial(dialCtx, "unix", c.socketPath)
	if err != nil {
		return nil, fmt.Errorf("%w: dial %s: %s", ErrSocketUnavailable, c.socketPath, err)
	}
	defer conn.Close()

	// Set conn deadline: use the earlier of context deadline (if set) and driver timeout.
	// This ensures context cancellation propagates promptly even on blocking reads.
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("%w: set deadline: %s", ErrEngineError, err)
	}

	// Close the conn when the context is canceled. This unblocks any blocking
	// read/write in http.ReadResponse that would otherwise wait for the conn deadline.
	connClosedCh := make(chan struct{})
	defer close(connClosedCh)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-connClosedCh:
		}
	}()

	// Write HTTP/1.1 request. Connection: close tells the server (and the client-side
	// http.ReadResponse) to treat the connection as one-shot — no keep-alive.
	req := "GET /containers/json?all=0 HTTP/1.1\r\nHost: docker\r\nConnection: close\r\nAccept: application/json\r\n\r\n"
	if _, err := conn.Write([]byte(req)); err != nil {
		return nil, fmt.Errorf("%w: write request: %s", ErrSocketUnavailable, err)
	}

	// Parse the HTTP response.
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		// Distinguish context cancellation from other read errors.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: read response: %s", ErrEngineError, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: HTTP %d", ErrEngineError, resp.StatusCode)
	}

	// Cap response body to avoid unbounded memory usage.
	// Read up to maxResponseBytes+1: if we get more than max, reject.
	limited := io.LimitReader(resp.Body, int64(maxResponseBytes)+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: read body: %s", ErrEngineError, err)
	}
	if len(raw) > maxResponseBytes {
		return nil, fmt.Errorf("%w: response body exceeds %d bytes limit", ErrEngineError, maxResponseBytes)
	}

	var containers []containerJSON
	if err := json.Unmarshal(raw, &containers); err != nil {
		return nil, fmt.Errorf("%w: JSON decode: %s", ErrEngineError, err)
	}

	return containers, nil
}
