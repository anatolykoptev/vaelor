// Package ssh provides a fleet.Probe implementation that discovers running
// containers on a remote host via the system `ssh` binary and `docker ps`.
//
// Security model:
//   - Disabled by default; caller must pass WithEnabled(true).
//   - Only one docker invocation is ever issued: `docker ps --no-trunc --format={{json .}}`.
//   - Args are passed as slice elements to exec.CommandContext — no shell-string
//     composition, no `sh -c`, no `bash -c`.
//   - All args are validated against the allowlist before exec.
//   - Filter.Service is validated BEFORE any remote call.
//   - Stderr from ssh is discarded (never surfaces to callers).
//   - Stdout is capped at 1 MiB (enforced streaming via cappedWriter).
package ssh

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"time"

	"github.com/anatolykoptev/go-code/internal/fleet"
)

// Sentinel errors. Callers use errors.Is to distinguish error classes.
var (
	// ErrSSHDisabled is returned when the driver has not been enabled.
	// Production wiring (P5) enables the driver only when
	// GOCODE_FLEET_SSH_ENABLE=true in env.
	ErrSSHDisabled = errors.New("fleet/ssh: driver disabled; set GOCODE_FLEET_SSH_ENABLE=true")

	// ErrAllowlistViolation is returned when the computed argv does not match
	// the fixed allowlist. This should never happen in normal usage because the
	// driver constructs the args slice itself; it protects against code-path bugs.
	ErrAllowlistViolation = errors.New("fleet/ssh: command not in allowlist")

	// ErrInvalidFilter is returned when Filter.Service contains characters
	// outside [a-zA-Z0-9._-]. Validated before any remote call.
	ErrInvalidFilter = errors.New("fleet/ssh: invalid filter")

	// ErrInvalidTarget is returned when Target.Scheme != "ssh" or Target.Host == "".
	ErrInvalidTarget = errors.New("fleet/ssh: invalid target")

	// ErrSSHError is returned when the exec.CommandContext call fails or
	// stdout exceeds the 1 MiB cap.
	ErrSSHError = errors.New("fleet/ssh: ssh execution error")

	// ErrParseError is returned when a JSON line cannot be decoded.
	// The driver uses best-effort parsing: a single bad line does not abort
	// the entire result; it is skipped silently.
	ErrParseError = errors.New("fleet/ssh: parse error")
)

// maxStdoutBytes is the cap on ssh stdout. Responses larger than this are
// rejected to prevent memory exhaustion from a misbehaving host.
// Enforced streaming via cappedWriter inside realExecer.Run, and
// additionally as a post-fetch check in List (so the post-fetch check
// also protects callers that inject a fakeExecer returning oversized data).
const maxStdoutBytes = 1 * 1024 * 1024 // 1 MiB

// maxStderrBytes is the cap on ssh stderr.
// Stderr is diagnostic text only; 64 KiB is ample for any error message.
const maxStderrBytes = 64 * 1024 // 64 KiB

// cappedWriter limits writes to a fixed byte budget.
// Once the budget is exhausted, Write returns io.ErrShortWrite and invokes
// the cancel function (if set) to terminate the underlying subprocess.
// cancel is called at most once; it is nilled after the first invocation.
type cappedWriter struct {
	inner   io.Writer
	max     int
	written int
	cancel  context.CancelFunc
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	// Already exhausted: reject immediately.
	if w.written >= w.max {
		return 0, io.ErrShortWrite
	}
	remaining := w.max - w.written
	if len(p) > remaining {
		// Write only up to budget, then cancel.
		n, _ := w.inner.Write(p[:remaining])
		w.written += n
		if w.cancel != nil {
			w.cancel()
			w.cancel = nil
		}
		return n, io.ErrShortWrite
	}
	n, err := w.inner.Write(p)
	w.written += n
	return n, err
}

// Driver is the ssh-shell-out fleet.Probe implementation.
// It delegates to the user's system `ssh` binary so that ~/.ssh/config
// (ProxyJump, agent, key passphrases, known_hosts) is the single source
// of truth — no parallel SSH stack to maintain.
type Driver struct {
	enabled bool
	binary  string
	execer  Execer
	timeout time.Duration
}

// Option configures a Driver.
type Option func(*Driver)

// WithEnabled gates the driver. Default false → List() returns ErrSSHDisabled.
func WithEnabled(b bool) Option {
	return func(d *Driver) {
		d.enabled = b
	}
}

// WithBinary overrides the ssh binary path. Default "ssh" (resolved via PATH).
func WithBinary(path string) Option {
	return func(d *Driver) {
		d.binary = path
	}
}

// WithExecer overrides the underlying exec mechanism. Tests inject fakes.
func WithExecer(e Execer) Option {
	return func(d *Driver) {
		d.execer = e
	}
}

// WithTimeout sets the per-call ssh timeout. Default 10s.
func WithTimeout(d time.Duration) Option {
	return func(dr *Driver) {
		dr.timeout = d
	}
}

// New constructs a Driver with the given options.
// Default: disabled, binary="ssh", timeout=10s.
func New(opts ...Option) *Driver {
	d := &Driver{
		enabled: false,
		binary:  "ssh",
		timeout: 10 * time.Second,
	}
	for _, o := range opts {
		o(d)
	}
	if d.execer == nil {
		d.execer = &realExecer{}
	}
	return d
}

// Scheme returns "ssh".
func (d *Driver) Scheme() string {
	return "ssh"
}

// List queries the remote host for running containers.
//
// Flow:
//  1. Gate on enabled.
//  2. Validate target scheme and host.
//  3. Validate Filter.Service before any remote call.
//  4. Build argv (host string, optional -p port, fixed docker invocation).
//  5. Run through allowlist.
//  6. Call execer.Run.
//  7. Check stdout size cap (belt-and-braces: also enforced streaming in realExecer).
//  8. Parse JSON-per-line output.
//  9. Post-fetch filter by Service.
func (d *Driver) List(ctx context.Context, t fleet.Target, f fleet.Filter) ([]fleet.RuntimeImage, error) {
	// Step 1: gate on enabled.
	if !d.enabled {
		return nil, ErrSSHDisabled
	}

	// Step 2: validate target.
	if t.Scheme != "ssh" {
		return nil, fmt.Errorf("%w: scheme %q is not \"ssh\"", ErrInvalidTarget, t.Scheme)
	}
	if t.Host == "" {
		return nil, fmt.Errorf("%w: host is empty", ErrInvalidTarget)
	}

	// Step 3: validate filter before any I/O.
	if err := validateFilter(f); err != nil {
		return nil, err
	}

	// Step 4: build the ssh host string and args slice.
	// host string: "user@host" if User is set, else just "host".
	hostArg := t.Host
	if t.User != "" {
		hostArg = t.User + "@" + t.Host
	}

	// Build the full argv for allowlist validation:
	//   [(-p port)? host docker ps --no-trunc --format={{json .}}]
	var fullArgv []string
	if t.Port > 0 {
		fullArgv = append(fullArgv, "-p", strconv.Itoa(t.Port))
	}
	fullArgv = append(fullArgv, hostArg, "docker", "ps", "--no-trunc", "--format={{json .}}")

	// Step 5: allowlist validation.
	if err := Validate(fullArgv); err != nil {
		return nil, err
	}

	// Build args for Execer.Run (everything except the host — host is a
	// separate parameter). The Execer's production implementation will pass
	// host as its own positional arg to exec.Command.
	//
	// args = [(-p port)? docker ps --no-trunc --format={{json .}}]
	var execArgs []string
	if t.Port > 0 {
		execArgs = append(execArgs, "-p", strconv.Itoa(t.Port))
	}
	execArgs = append(execArgs, "docker", "ps", "--no-trunc", "--format={{json .}}")

	// Step 6: call execer.
	callCtx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	stdout, _, err := d.execer.Run(callCtx, d.binary, hostArg, execArgs)
	// Stderr is intentionally discarded (prevents host fingerprints / key paths
	// from leaking into the LLM prompt or error messages).

	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSSHError, err)
	}

	// Step 7: stdout size cap (belt-and-braces post-fetch check).
	// realExecer enforces this streaming via cappedWriter; this check also
	// catches oversized data injected by fakeExecer in tests.
	if len(stdout) > maxStdoutBytes {
		return nil, fmt.Errorf("%w: stdout exceeds %d bytes cap", ErrSSHError, maxStdoutBytes)
	}

	// Step 8: parse JSON-per-line output.
	// Best-effort: garbage lines are skipped rather than aborting the result.
	imgs := make([]fleet.RuntimeImage, 0)
	for _, line := range bytes.Split(stdout, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		img, parseErr := ParseDockerPSLine(line)
		if parseErr != nil {
			// Skip malformed lines; preserve partial results.
			continue
		}
		imgs = append(imgs, img)
	}

	// Step 9: post-fetch filter.
	if f.Service == "" {
		return imgs, nil
	}
	filtered := imgs[:0]
	for _, img := range imgs {
		if matchesFilter(img, f.Service) {
			filtered = append(filtered, img)
		}
	}
	return filtered, nil
}

// matchesFilter reports whether img satisfies the Service filter.
// Matches container name OR compose-service label (case-sensitive).
func matchesFilter(img fleet.RuntimeImage, service string) bool {
	return img.Container == service || img.Service == service
}

// validateFilter checks Filter.Service for allowed characters [a-zA-Z0-9._-].
func validateFilter(f fleet.Filter) error {
	if f.Service == "" {
		return nil
	}
	for _, r := range f.Service {
		if !isAllowedFilterChar(r) {
			return fmt.Errorf("%w: service name %q contains invalid character %q",
				ErrInvalidFilter, f.Service, r)
		}
	}
	return nil
}

// isAllowedFilterChar reports whether r is in [a-zA-Z0-9._-].
// No regexp — hand-rolled per constraint.
func isAllowedFilterChar(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '.' || r == '_' || r == '-'
}

// Execer is the testing seam.
//
// Production: realExecer shells out to the system `ssh` binary.
// Tests: inject a fakeExecer to run in-process.
type Execer interface {
	// Run executes `<binary> <host> <args...>`.
	// Returns stdout, stderr, error.
	// ctx deadline must be honoured.
	// Output is returned as-is without trailing newline trim — caller handles it.
	Run(ctx context.Context, binary, host string, args []string) (stdout []byte, stderr []byte, err error)
}

// realExecer is the production Execer that shells out via exec.CommandContext.
// stdout is capped at maxStdoutBytes and stderr at maxStderrBytes via
// cappedWriter, which cancels the subprocess on overflow.
type realExecer struct{}

func (e *realExecer) Run(ctx context.Context, binary, host string, args []string) ([]byte, []byte, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Separate any leading option flags (-p N) from the positional args
	// so that "--" can be inserted immediately before the host.
	// This prevents a leading-dash host (e.g. "-v") from being interpreted
	// as an ssh flag — defense-in-depth alongside the allowlist check.
	//
	// Resulting argv shape:
	//   [(-p N)?  --  host  docker ps ...]
	var opts []string
	rest := args
	if len(rest) >= 2 && rest[0] == "-p" {
		opts = []string{rest[0], rest[1]}
		rest = rest[2:]
	}
	argv := append(opts, "--", host)
	argv = append(argv, rest...)

	cmd := exec.CommandContext(ctx, binary, argv...)

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &cappedWriter{inner: &outBuf, max: maxStdoutBytes, cancel: cancel}
	cmd.Stderr = &cappedWriter{inner: &errBuf, max: maxStderrBytes, cancel: cancel}

	err := cmd.Run()
	if err != nil && errors.Is(ctx.Err(), context.Canceled) {
		// Overflow triggered the cancel — produce a clear diagnostic error.
		err = fmt.Errorf("%w: output exceeded cap (stdout=%d stderr=%d)",
			ErrSSHError, outBuf.Len(), errBuf.Len())
	}
	return outBuf.Bytes(), errBuf.Bytes(), err
}

// Ensure Driver satisfies fleet.Probe at compile time.
var _ fleet.Probe = (*Driver)(nil)
