package docker_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/fleet"
	"github.com/anatolykoptev/go-code/internal/fleet/docker"
)

// fakeDockerServer runs a minimal fake docker API over the provided server-side net.Conn.
// It reads one HTTP request, writes the provided response, then closes.
// Returns a channel that is closed when the goroutine exits (for leak checking).
func fakeDockerServer(t *testing.T, serverConn net.Conn, response string) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer serverConn.Close()
		buf := make([]byte, 4096)
		n, _ := serverConn.Read(buf)
		_ = n // we have the request bytes; ignore content for basic tests
		_, _ = io.WriteString(serverConn, response)
	}()
	return done
}

// fakeDockerServerAssert is like fakeDockerServer but captures the request and
// passes it to assertFn before responding. Used for asserting request content.
func fakeDockerServerAssert(t *testing.T, serverConn net.Conn, assertFn func(req string), response string) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer serverConn.Close()
		buf := make([]byte, 4096)
		n, _ := serverConn.Read(buf)
		req := string(buf[:n])
		assertFn(req)
		_, _ = io.WriteString(serverConn, response)
	}()
	return done
}

// httpOK wraps a JSON body in a minimal HTTP/1.1 200 response.
func httpOK(body string) string {
	return fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
		len(body), body)
}

// httpError returns a non-200 HTTP response.
func httpError(code int, phrase string) string {
	return fmt.Sprintf("HTTP/1.1 %d %s\r\nContent-Length: 0\r\nConnection: close\r\n\r\n", code, phrase)
}

// newPipeDriver creates a Driver wired to a net.Pipe-backed fake.
// The caller is responsible for driving serverConn (via fakeDockerServer or similar).
func newPipeDriver(t *testing.T) (*docker.Driver, net.Conn) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	var mu sync.Mutex
	used := false
	d := docker.New(
		docker.WithDialer(func(_ context.Context, _, _ string) (net.Conn, error) {
			mu.Lock()
			defer mu.Unlock()
			if used {
				// For multi-call tests create a new pair; single-call tests won't hit this.
				return nil, fmt.Errorf("dialer: only one call expected")
			}
			used = true
			return clientConn, nil
		}),
	)
	t.Cleanup(func() {
		clientConn.Close()
		serverConn.Close()
	})
	return d, serverConn
}

// waitDone asserts the server goroutine exited within 2 seconds.
func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("server goroutine did not exit within 2s (goroutine leak)")
	}
}

// ---- Tests -------------------------------------------------------------------

func TestDriver_Scheme(t *testing.T) {
	d := docker.New()
	if d.Scheme() != "docker" {
		t.Fatalf("want Scheme()=docker, got %q", d.Scheme())
	}
}

func TestList_EmptyList(t *testing.T) {
	d, serverConn := newPipeDriver(t)
	done := fakeDockerServer(t, serverConn, httpOK("[]"))
	imgs, err := d.List(context.Background(), fleet.Target{Scheme: "docker"}, fleet.Filter{})
	waitDone(t, done)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(imgs) != 0 {
		t.Fatalf("expected empty slice, got %d items", len(imgs))
	}
}

func TestList_RequestLine(t *testing.T) {
	d, serverConn := newPipeDriver(t)
	done := fakeDockerServerAssert(t, serverConn,
		func(req string) {
			// t.Errorf is goroutine-safe; use it so the test actually fails on regression.
			firstLine := strings.SplitN(req, "\r\n", 2)[0]
			if !strings.HasPrefix(firstLine, "GET /containers/json?all=0") {
				t.Errorf("request line: want prefix %q, got %q", "GET /containers/json?all=0", firstLine)
			}
			if !strings.Contains(req, "Host: docker") {
				t.Errorf("request missing Host: docker header")
			}
			if !strings.Contains(req, "Connection: close") {
				t.Errorf("request missing Connection: close header")
			}
		},
		httpOK("[]"),
	)
	_, _ = d.List(context.Background(), fleet.Target{Scheme: "docker"}, fleet.Filter{})
	waitDone(t, done)
}

func TestList_SingleContainer(t *testing.T) {
	body := `[{"Id":"abc","Names":["/web"],"Image":"nginx:1.27-alpine","ImageID":"sha256:def","Created":1700000000,"State":"running","Labels":{}}]`
	d, serverConn := newPipeDriver(t)
	done := fakeDockerServer(t, serverConn, httpOK(body))
	imgs, err := d.List(context.Background(), fleet.Target{Scheme: "docker"}, fleet.Filter{})
	waitDone(t, done)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("expected 1 image, got %d", len(imgs))
	}
	img := imgs[0]
	if img.Container != "web" {
		t.Errorf("Container: want %q, got %q", "web", img.Container)
	}
	if img.Image != "nginx" {
		t.Errorf("Image: want %q, got %q", "nginx", img.Image)
	}
	if img.Tag != "1.27-alpine" {
		t.Errorf("Tag: want %q, got %q", "1.27-alpine", img.Tag)
	}
	if img.State != "running" {
		t.Errorf("State: want %q, got %q", "running", img.State)
	}
	wantTime := time.Unix(1700000000, 0).UTC()
	if !img.StartedAt.Equal(wantTime) {
		t.Errorf("StartedAt: want %v, got %v", wantTime, img.StartedAt)
	}
	if img.Digest != "" {
		t.Errorf("Digest: want empty (ImageID not used), got %q", img.Digest)
	}
}

func TestList_ComposeLabel(t *testing.T) {
	body := `[{"Id":"def","Names":["/myapp_redis_1"],"Image":"redis:7.4","ImageID":"sha256:abc","Created":1700000001,"State":"running","Labels":{"com.docker.compose.service":"redis"}}]`
	d, serverConn := newPipeDriver(t)
	done := fakeDockerServer(t, serverConn, httpOK(body))
	imgs, err := d.List(context.Background(), fleet.Target{Scheme: "docker"}, fleet.Filter{})
	waitDone(t, done)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("expected 1, got %d", len(imgs))
	}
	if imgs[0].Service != "redis" {
		t.Errorf("Service: want %q, got %q", "redis", imgs[0].Service)
	}
	if imgs[0].Container != "myapp_redis_1" {
		t.Errorf("Container: want %q, got %q", "myapp_redis_1", imgs[0].Container)
	}
}

func TestList_DigestPin(t *testing.T) {
	// Image with @sha256 digest in the user-facing Image field.
	body := `[{"Id":"ghi","Names":["/db"],"Image":"postgres:16@sha256:abc123","ImageID":"sha256:localonly","Created":1700000002,"State":"running","Labels":{}}]`
	d, serverConn := newPipeDriver(t)
	done := fakeDockerServer(t, serverConn, httpOK(body))
	imgs, err := d.List(context.Background(), fleet.Target{Scheme: "docker"}, fleet.Filter{})
	waitDone(t, done)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("expected 1, got %d", len(imgs))
	}
	img := imgs[0]
	if img.Image != "postgres" {
		t.Errorf("Image: want %q, got %q", "postgres", img.Image)
	}
	if img.Tag != "16" {
		t.Errorf("Tag: want %q, got %q", "16", img.Tag)
	}
	if img.Digest != "sha256:abc123" {
		t.Errorf("Digest: want %q, got %q", "sha256:abc123", img.Digest)
	}
}

func TestList_PortRegistry(t *testing.T) {
	// localhost:5000/foo:1.0 — last colon+no-slash-in-suffix = tag, but first
	// colon is part of registry host:port.
	body := `[{"Id":"jkl","Names":["/custom"],"Image":"localhost:5000/foo:1.0","ImageID":"sha256:x","Created":0,"State":"exited","Labels":{}}]`
	d, serverConn := newPipeDriver(t)
	done := fakeDockerServer(t, serverConn, httpOK(body))
	imgs, err := d.List(context.Background(), fleet.Target{Scheme: "docker"}, fleet.Filter{})
	waitDone(t, done)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("expected 1, got %d", len(imgs))
	}
	img := imgs[0]
	if img.Image != "localhost:5000/foo" {
		t.Errorf("Image: want %q, got %q", "localhost:5000/foo", img.Image)
	}
	if img.Tag != "1.0" {
		t.Errorf("Tag: want %q, got %q", "1.0", img.Tag)
	}
	if !img.StartedAt.IsZero() {
		t.Errorf("StartedAt: want zero, got %v", img.StartedAt)
	}
}

func TestList_NoNamesShortID(t *testing.T) {
	body := `[{"Id":"123456789012abcdefgh","Names":[],"Image":"alpine","ImageID":"sha256:z","Created":0,"State":"created","Labels":{}}]`
	d, serverConn := newPipeDriver(t)
	done := fakeDockerServer(t, serverConn, httpOK(body))
	imgs, err := d.List(context.Background(), fleet.Target{Scheme: "docker"}, fleet.Filter{})
	waitDone(t, done)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("expected 1, got %d", len(imgs))
	}
	img := imgs[0]
	if img.Container != "123456789012" {
		t.Errorf("Container: want 12-char short id %q, got %q", "123456789012", img.Container)
	}
	if img.Image != "alpine" {
		t.Errorf("Image: want %q, got %q", "alpine", img.Image)
	}
	if img.Tag != "latest" {
		t.Errorf("Tag: want %q (no tag → latest), got %q", "latest", img.Tag)
	}
}

func TestList_FilterByContainerName(t *testing.T) {
	body := `[
		{"Id":"a","Names":["/web"],"Image":"nginx:1.27","Created":1700000000,"State":"running","Labels":{}},
		{"Id":"b","Names":["/cache"],"Image":"redis:7","Created":1700000001,"State":"running","Labels":{}},
		{"Id":"c","Names":["/db"],"Image":"postgres:16","Created":1700000002,"State":"running","Labels":{}}
	]`
	d, serverConn := newPipeDriver(t)
	done := fakeDockerServer(t, serverConn, httpOK(body))
	imgs, err := d.List(context.Background(), fleet.Target{Scheme: "docker"}, fleet.Filter{Service: "web"})
	waitDone(t, done)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("expected 1, got %d", len(imgs))
	}
	if imgs[0].Container != "web" {
		t.Errorf("Container: want %q, got %q", "web", imgs[0].Container)
	}
}

func TestList_FilterByComposeLabel(t *testing.T) {
	body := `[
		{"Id":"aa","Names":["/app_api_1"],"Image":"myapp:latest","Created":1700000000,"State":"running","Labels":{"com.docker.compose.service":"api"}},
		{"Id":"bb","Names":["/app_db_1"],"Image":"postgres:16","Created":1700000001,"State":"running","Labels":{"com.docker.compose.service":"db"}}
	]`
	d, serverConn := newPipeDriver(t)
	done := fakeDockerServer(t, serverConn, httpOK(body))
	imgs, err := d.List(context.Background(), fleet.Target{Scheme: "docker"}, fleet.Filter{Service: "api"})
	waitDone(t, done)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("expected 1, got %d", len(imgs))
	}
	if imgs[0].Service != "api" {
		t.Errorf("Service: want %q, got %q", "api", imgs[0].Service)
	}
}

func TestList_FilterInvalidService(t *testing.T) {
	// Invalid filter char — must return ErrInvalidFilter WITHOUT dialing.
	dialCalled := false
	d := docker.New(
		docker.WithDialer(func(_ context.Context, _, _ string) (net.Conn, error) {
			dialCalled = true
			return nil, fmt.Errorf("should not be called")
		}),
	)
	_, err := d.List(context.Background(), fleet.Target{Scheme: "docker"}, fleet.Filter{Service: "web;rm"})
	if err == nil {
		t.Fatal("expected error for invalid filter, got nil")
	}
	if !errors.Is(err, docker.ErrInvalidFilter) {
		t.Errorf("want errors.Is(err, ErrInvalidFilter), got: %v", err)
	}
	if dialCalled {
		t.Error("dialer must not be called for invalid filter")
	}
}

func TestList_HTTP500(t *testing.T) {
	d, serverConn := newPipeDriver(t)
	done := fakeDockerServer(t, serverConn, httpError(500, "Internal Server Error"))
	_, err := d.List(context.Background(), fleet.Target{Scheme: "docker"}, fleet.Filter{})
	waitDone(t, done)
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
	if !errors.Is(err, docker.ErrEngineError) {
		t.Errorf("want errors.Is(err, ErrEngineError), got: %v", err)
	}
}

func TestList_NonJSONBody(t *testing.T) {
	d, serverConn := newPipeDriver(t)
	done := fakeDockerServer(t, serverConn, httpOK("not json"))
	_, err := d.List(context.Background(), fleet.Target{Scheme: "docker"}, fleet.Filter{})
	waitDone(t, done)
	if err == nil {
		t.Fatal("expected error for non-JSON body, got nil")
	}
	if !errors.Is(err, docker.ErrEngineError) {
		t.Errorf("want errors.Is(err, ErrEngineError), got: %v", err)
	}
}

func TestList_DialError(t *testing.T) {
	dialErr := fmt.Errorf("no such file: %w", &net.OpError{Op: "dial", Net: "unix", Err: fmt.Errorf("no such file or directory")})
	d := docker.New(
		docker.WithDialer(func(_ context.Context, _, _ string) (net.Conn, error) {
			return nil, dialErr
		}),
	)
	_, err := d.List(context.Background(), fleet.Target{Scheme: "docker"}, fleet.Filter{})
	if err == nil {
		t.Fatal("expected error for dial failure, got nil")
	}
	if !errors.Is(err, docker.ErrSocketUnavailable) {
		t.Errorf("want errors.Is(err, ErrSocketUnavailable), got: %v", err)
	}
}

func TestList_BodyCapExceeded(t *testing.T) {
	// Write a response body that exceeds the 8MiB cap.
	// Build ~9MiB body that starts with '[' so it's not caught by JSON parse first.
	// We just fill with spaces inside a JSON array that's never closed so JSON decode fails,
	// but we test that the cap wrapping causes an ErrEngineError.
	// Actually we need to send > 8MiB to trigger the cap. We'll use a goroutine
	// that streams large data.
	clientConn, serverConn := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer serverConn.Close()
		// Read the request.
		buf := make([]byte, 4096)
		serverConn.Read(buf)
		// Write a response header, then 9MiB of garbage.
		header := "HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\nContent-Type: application/json\r\nConnection: close\r\n\r\n"
		io.WriteString(serverConn, header)
		// Write 9 * 1024 * 1024 bytes: chunk format.
		chunk := make([]byte, 64*1024)
		for i := range chunk {
			chunk[i] = ' '
		}
		chunk[0] = '['
		totalBytes := 0
		for totalBytes < 9*1024*1024 {
			n := len(chunk)
			if totalBytes+n > 9*1024*1024 {
				n = 9*1024*1024 - totalBytes
			}
			fmt.Fprintf(serverConn, "%x\r\n", n)
			serverConn.Write(chunk[:n])
			io.WriteString(serverConn, "\r\n")
			totalBytes += n
		}
	}()

	d := docker.New(
		docker.WithDialer(func(_ context.Context, _, _ string) (net.Conn, error) {
			return clientConn, nil
		}),
	)
	_, err := d.List(context.Background(), fleet.Target{Scheme: "docker"}, fleet.Filter{})
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	clientConn.Close()
	serverConn.Close()

	if err == nil {
		t.Fatal("expected error for oversized body, got nil")
	}
	if !errors.Is(err, docker.ErrEngineError) {
		t.Errorf("want errors.Is(err, ErrEngineError) for body cap exceeded, got: %v", err)
	}
}

func TestList_ContextCanceled(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()
	// Never respond — let context cancel trigger.
	d := docker.New(
		docker.WithDialer(func(_ context.Context, _, _ string) (net.Conn, error) {
			return clientConn, nil
		}),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := d.List(ctx, fleet.Target{Scheme: "docker"}, fleet.Filter{})
	if err == nil {
		t.Fatal("expected error for canceled context, got nil")
	}
}

func TestList_ImageNoTagNoDigest_DefaultsToLatest(t *testing.T) {
	// "alpine" with no tag and no digest → tag should be "latest"
	body := `[{"Id":"xyz","Names":["/mycontainer"],"Image":"alpine","ImageID":"sha256:z","Created":1700000000,"State":"running","Labels":{}}]`
	d, serverConn := newPipeDriver(t)
	done := fakeDockerServer(t, serverConn, httpOK(body))
	imgs, err := d.List(context.Background(), fleet.Target{Scheme: "docker"}, fleet.Filter{})
	waitDone(t, done)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("expected 1, got %d", len(imgs))
	}
	if imgs[0].Tag != "latest" {
		t.Errorf("Tag: want %q, got %q", "latest", imgs[0].Tag)
	}
}

func TestList_InvalidDigestFormatDropped(t *testing.T) {
	// @<non-sha256> in user-facing Image field → Digest should be "" (silent drop, not error).
	body := `[{"Id":"bad","Names":["/badsig"],"Image":"registry.io/img:1.0@md5:notvalid","ImageID":"sha256:z","Created":1700000000,"State":"running","Labels":{}}]`
	d, serverConn := newPipeDriver(t)
	done := fakeDockerServer(t, serverConn, httpOK(body))
	imgs, err := d.List(context.Background(), fleet.Target{Scheme: "docker"}, fleet.Filter{})
	waitDone(t, done)
	if err != nil {
		t.Fatalf("unexpected error (invalid digest is silently dropped in runtime probe): %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("expected 1, got %d", len(imgs))
	}
	if imgs[0].Digest != "" {
		t.Errorf("Digest: want empty for invalid digest format, got %q", imgs[0].Digest)
	}
}
