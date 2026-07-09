package ssh_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/fleet"
	flssh "github.com/anatolykoptev/go-code/internal/fleet/ssh"
)

// fakeExecer is a testing seam for Driver.
type fakeExecer struct {
	stdout    []byte
	stderr    []byte
	err       error
	gotBinary string
	gotHost   string
	gotArgs   []string
	called    bool
}

func (f *fakeExecer) Run(_ context.Context, binary, host string, args []string) ([]byte, []byte, error) {
	f.called = true
	f.gotBinary = binary
	f.gotHost = host
	f.gotArgs = args
	return f.stdout, f.stderr, f.err
}

// twoContainersJSON returns two JSON-per-line docker ps lines.
func twoContainersJSON() []byte {
	line1 := `{"ID":"abc123def456","Names":"web","Image":"nginx:1.27-alpine","State":"running","Labels":"","CreatedAt":"2024-08-12 14:00:00 +0000 UTC"}`
	line2 := `{"ID":"def456abc789","Names":"cache","Image":"redis:7","State":"running","Labels":"","CreatedAt":"2024-08-12 15:00:00 +0000 UTC"}`
	return []byte(line1 + "\n" + line2 + "\n")
}

func TestDriver_DisabledByDefault(t *testing.T) {
	t.Parallel()
	d := flssh.New()
	_, err := d.List(context.Background(), fleet.Target{Scheme: "ssh", Host: "krolik"}, fleet.Filter{})
	if err == nil {
		t.Fatal("want error from disabled driver, got nil")
	}
	if !errors.Is(err, flssh.ErrSSHDisabled) {
		t.Errorf("want ErrSSHDisabled, got: %v", err)
	}
}

func TestDriver_BasicList(t *testing.T) {
	t.Parallel()
	fake := &fakeExecer{stdout: twoContainersJSON()}
	d := flssh.New(flssh.WithEnabled(true), flssh.WithExecer(fake))
	imgs, err := d.List(context.Background(), fleet.Target{Scheme: "ssh", Host: "krolik"}, fleet.Filter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(imgs) != 2 {
		t.Fatalf("want 2 images, got %d", len(imgs))
	}
	if imgs[0].Container != "web" {
		t.Errorf("imgs[0].Container: want %q, got %q", "web", imgs[0].Container)
	}
	if imgs[1].Container != "cache" {
		t.Errorf("imgs[1].Container: want %q, got %q", "cache", imgs[1].Container)
	}
}

func TestDriver_ComposeLabelPopulatesService(t *testing.T) {
	t.Parallel()
	line := `{"ID":"abc","Names":"web","Image":"nginx","State":"running","Labels":"com.docker.compose.service=web,com.docker.compose.project=myapp","CreatedAt":""}` + "\n"
	fake := &fakeExecer{stdout: []byte(line)}
	d := flssh.New(flssh.WithEnabled(true), flssh.WithExecer(fake))
	imgs, err := d.List(context.Background(), fleet.Target{Scheme: "ssh", Host: "krolik"}, fleet.Filter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("want 1 image, got %d", len(imgs))
	}
	if imgs[0].Service != "web" {
		t.Errorf("Service: want %q, got %q", "web", imgs[0].Service)
	}
}

func TestDriver_CreatedAtParse(t *testing.T) {
	t.Parallel()
	line := `{"ID":"abc","Names":"web","Image":"nginx","State":"running","Labels":"","CreatedAt":"2024-08-12 14:00:00 +0000 UTC"}` + "\n"
	fake := &fakeExecer{stdout: []byte(line)}
	d := flssh.New(flssh.WithEnabled(true), flssh.WithExecer(fake))
	imgs, err := d.List(context.Background(), fleet.Target{Scheme: "ssh", Host: "krolik"}, fleet.Filter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("want 1, got %d", len(imgs))
	}
	if imgs[0].StartedAt.IsZero() {
		t.Error("StartedAt: want non-zero from parsed CreatedAt, got zero")
	}
}

func TestDriver_CreatedAtZeroString(t *testing.T) {
	t.Parallel()
	line := `{"ID":"abc","Names":"web","Image":"nginx","State":"running","Labels":"","CreatedAt":""}` + "\n"
	fake := &fakeExecer{stdout: []byte(line)}
	d := flssh.New(flssh.WithEnabled(true), flssh.WithExecer(fake))
	imgs, err := d.List(context.Background(), fleet.Target{Scheme: "ssh", Host: "krolik"}, fleet.Filter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("want 1, got %d", len(imgs))
	}
	if !imgs[0].StartedAt.IsZero() {
		t.Errorf("StartedAt: want zero, got %v", imgs[0].StartedAt)
	}
}

func TestDriver_DigestPin(t *testing.T) {
	t.Parallel()
	line := `{"ID":"abc","Names":"db","Image":"postgres:16@sha256:abc","State":"running","Labels":"","CreatedAt":""}` + "\n"
	fake := &fakeExecer{stdout: []byte(line)}
	d := flssh.New(flssh.WithEnabled(true), flssh.WithExecer(fake))
	imgs, err := d.List(context.Background(), fleet.Target{Scheme: "ssh", Host: "krolik"}, fleet.Filter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("want 1, got %d", len(imgs))
	}
	if imgs[0].Tag != "16" {
		t.Errorf("Tag: want %q, got %q", "16", imgs[0].Tag)
	}
	if imgs[0].Digest != "sha256:abc" {
		t.Errorf("Digest: want %q, got %q", "sha256:abc", imgs[0].Digest)
	}
}

func TestDriver_FilterServiceExactMatch(t *testing.T) {
	t.Parallel()
	line1 := `{"ID":"a","Names":"web","Image":"nginx","State":"running","Labels":"","CreatedAt":""}` + "\n"
	line2 := `{"ID":"b","Names":"cache","Image":"redis","State":"running","Labels":"","CreatedAt":""}` + "\n"
	line3 := `{"ID":"c","Names":"db","Image":"postgres","State":"running","Labels":"","CreatedAt":""}` + "\n"
	fake := &fakeExecer{stdout: []byte(line1 + line2 + line3)}
	d := flssh.New(flssh.WithEnabled(true), flssh.WithExecer(fake))
	imgs, err := d.List(context.Background(), fleet.Target{Scheme: "ssh", Host: "krolik"}, fleet.Filter{Service: "web"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("want 1, got %d", len(imgs))
	}
	if imgs[0].Container != "web" {
		t.Errorf("Container: want %q, got %q", "web", imgs[0].Container)
	}
}

func TestDriver_FilterServiceMatchesLabel(t *testing.T) {
	t.Parallel()
	line1 := `{"ID":"a","Names":"app_api_1","Image":"myapp","State":"running","Labels":"com.docker.compose.service=api","CreatedAt":""}` + "\n"
	line2 := `{"ID":"b","Names":"app_db_1","Image":"postgres","State":"running","Labels":"com.docker.compose.service=db","CreatedAt":""}` + "\n"
	fake := &fakeExecer{stdout: []byte(line1 + line2)}
	d := flssh.New(flssh.WithEnabled(true), flssh.WithExecer(fake))
	imgs, err := d.List(context.Background(), fleet.Target{Scheme: "ssh", Host: "krolik"}, fleet.Filter{Service: "api"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("want 1, got %d", len(imgs))
	}
	if imgs[0].Service != "api" {
		t.Errorf("Service: want %q, got %q", "api", imgs[0].Service)
	}
}

func TestDriver_FilterInvalid_NoExec(t *testing.T) {
	t.Parallel()
	fake := &fakeExecer{}
	d := flssh.New(flssh.WithEnabled(true), flssh.WithExecer(fake))
	_, err := d.List(context.Background(), fleet.Target{Scheme: "ssh", Host: "krolik"}, fleet.Filter{Service: "web;rm"})
	if err == nil {
		t.Fatal("want error for invalid filter, got nil")
	}
	if !errors.Is(err, flssh.ErrInvalidFilter) {
		t.Errorf("want ErrInvalidFilter, got: %v", err)
	}
	if fake.called {
		t.Error("execer must NOT be called for invalid filter")
	}
}

func TestDriver_EmptyTargetHost(t *testing.T) {
	t.Parallel()
	fake := &fakeExecer{}
	d := flssh.New(flssh.WithEnabled(true), flssh.WithExecer(fake))
	_, err := d.List(context.Background(), fleet.Target{Scheme: "ssh", Host: ""}, fleet.Filter{})
	if err == nil {
		t.Fatal("want error for empty Host, got nil")
	}
	if !errors.Is(err, flssh.ErrInvalidTarget) {
		t.Errorf("want ErrInvalidTarget, got: %v", err)
	}
	if fake.called {
		t.Error("execer must NOT be called for empty host")
	}
}

func TestDriver_NonSSHScheme(t *testing.T) {
	t.Parallel()
	fake := &fakeExecer{}
	d := flssh.New(flssh.WithEnabled(true), flssh.WithExecer(fake))
	_, err := d.List(context.Background(), fleet.Target{Scheme: "local", Host: "krolik"}, fleet.Filter{})
	if err == nil {
		t.Fatal("want error for non-ssh scheme, got nil")
	}
	if !errors.Is(err, flssh.ErrInvalidTarget) {
		t.Errorf("want ErrInvalidTarget, got: %v", err)
	}
}

func TestDriver_PortFlagPassed(t *testing.T) {
	t.Parallel()
	fake := &fakeExecer{stdout: []byte("")}
	d := flssh.New(flssh.WithEnabled(true), flssh.WithExecer(fake))
	_, err := d.List(context.Background(), fleet.Target{Scheme: "ssh", Host: "krolik", Port: 2222}, fleet.Filter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fake.called {
		t.Fatal("execer was not called")
	}
	// args must contain -p and 2222
	foundP := false
	found2222 := false
	for _, a := range fake.gotArgs {
		if a == "-p" {
			foundP = true
		}
		if a == "2222" {
			found2222 = true
		}
	}
	if !foundP {
		t.Errorf("args should contain -p flag, got: %v", fake.gotArgs)
	}
	if !found2222 {
		t.Errorf("args should contain 2222, got: %v", fake.gotArgs)
	}
}

func TestDriver_UserFlagInHostArg(t *testing.T) {
	t.Parallel()
	fake := &fakeExecer{stdout: []byte("")}
	d := flssh.New(flssh.WithEnabled(true), flssh.WithExecer(fake))
	_, err := d.List(context.Background(), fleet.Target{Scheme: "ssh", Host: "hully", User: "ubuntu"}, fleet.Filter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.gotHost != "ubuntu@hully" {
		t.Errorf("host arg: want %q, got %q", "ubuntu@hully", fake.gotHost)
	}
}

func TestDriver_EmptyStdout(t *testing.T) {
	t.Parallel()
	fake := &fakeExecer{stdout: []byte("")}
	d := flssh.New(flssh.WithEnabled(true), flssh.WithExecer(fake))
	imgs, err := d.List(context.Background(), fleet.Target{Scheme: "ssh", Host: "krolik"}, fleet.Filter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(imgs) != 0 {
		t.Errorf("want empty slice, got %d", len(imgs))
	}
}

func TestDriver_SingleBlankLine(t *testing.T) {
	t.Parallel()
	fake := &fakeExecer{stdout: []byte("\n")}
	d := flssh.New(flssh.WithEnabled(true), flssh.WithExecer(fake))
	imgs, err := d.List(context.Background(), fleet.Target{Scheme: "ssh", Host: "krolik"}, fleet.Filter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(imgs) != 0 {
		t.Errorf("want empty slice for blank line, got %d", len(imgs))
	}
}

func TestDriver_StdoutOver1MiB(t *testing.T) {
	t.Parallel()
	bigOutput := make([]byte, 1024*1024+1)
	fake := &fakeExecer{stdout: bigOutput}
	d := flssh.New(flssh.WithEnabled(true), flssh.WithExecer(fake))
	_, err := d.List(context.Background(), fleet.Target{Scheme: "ssh", Host: "krolik"}, fleet.Filter{})
	if err == nil {
		t.Fatal("want error for stdout > 1 MiB, got nil")
	}
	if !errors.Is(err, flssh.ErrSSHError) {
		t.Errorf("want ErrSSHError, got: %v", err)
	}
}

func TestDriver_ExecErrorPropagates(t *testing.T) {
	t.Parallel()
	fake := &fakeExecer{err: errors.New("connection refused")}
	d := flssh.New(flssh.WithEnabled(true), flssh.WithExecer(fake))
	_, err := d.List(context.Background(), fleet.Target{Scheme: "ssh", Host: "krolik"}, fleet.Filter{})
	if err == nil {
		t.Fatal("want error from exec, got nil")
	}
	if !errors.Is(err, flssh.ErrSSHError) {
		t.Errorf("want ErrSSHError, got: %v", err)
	}
}

func TestDriver_StderrDiscarded(t *testing.T) {
	t.Parallel()
	// stderr contains sensitive text; it must NOT appear in any returned field.
	line := `{"ID":"abc","Names":"web","Image":"nginx","State":"running","Labels":"","CreatedAt":""}` + "\n"
	fake := &fakeExecer{
		stdout: []byte(line),
		stderr: []byte("Warning: Permanently added 'krolik' key fingerprint leak"),
	}
	d := flssh.New(flssh.WithEnabled(true), flssh.WithExecer(fake))
	imgs, err := d.List(context.Background(), fleet.Target{Scheme: "ssh", Host: "krolik"}, fleet.Filter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(imgs) != 1 {
		t.Fatalf("want 1 image, got %d", len(imgs))
	}
	// None of the image fields should contain stderr content
	img := imgs[0]
	for _, field := range []string{img.Container, img.Image, img.Tag, img.Digest, img.Service, img.State} {
		if strings.Contains(field, "fingerprint") || strings.Contains(field, "krolik") {
			t.Errorf("field %q contains leaked stderr content", field)
		}
	}
}

func TestDriver_ParseErrorPerLine_BestEffort(t *testing.T) {
	t.Parallel()
	// One valid line + one garbage line → best-effort: return 1 valid result, no error.
	// This follows the partial-info tolerance pattern established in P3.
	validLine := `{"ID":"abc","Names":"web","Image":"nginx","State":"running","Labels":"","CreatedAt":""}` + "\n"
	garbageLine := "not json at all\n"
	fake := &fakeExecer{stdout: []byte(validLine + garbageLine)}
	d := flssh.New(flssh.WithEnabled(true), flssh.WithExecer(fake))
	imgs, err := d.List(context.Background(), fleet.Target{Scheme: "ssh", Host: "krolik"}, fleet.Filter{})
	if err != nil {
		t.Fatalf("unexpected error (best-effort: garbage lines are skipped): %v", err)
	}
	if len(imgs) != 1 {
		t.Errorf("want 1 (valid) result, got %d", len(imgs))
	}
	if imgs[0].Container != "web" {
		t.Errorf("Container: want %q, got %q", "web", imgs[0].Container)
	}
}

// TestDriver_LeadingDashHost_Rejected ensures that a host starting with "-"
// is rejected before any exec.Command construction.
// url.Parse("ssh://-G") returns Hostname()=="-G"; without this guard,
// ssh would interpret "-G" as its "--print-config" flag, not a destination.
func TestDriver_LeadingDashHost_Rejected(t *testing.T) {
	t.Parallel()
	fake := &fakeExecer{stdout: []byte("")}
	d := flssh.New(flssh.WithEnabled(true), flssh.WithExecer(fake))
	_, err := d.List(context.Background(),
		fleet.Target{Scheme: "ssh", Host: "-G"}, fleet.Filter{})
	if err == nil {
		t.Fatal("expected error for leading-dash host, got nil")
	}
	if fake.called {
		t.Error("execer must NOT be called when host starts with -")
	}
	// Either ErrAllowlistViolation or ErrInvalidTarget closes the class.
	if !errors.Is(err, flssh.ErrAllowlistViolation) && !errors.Is(err, flssh.ErrInvalidTarget) {
		t.Errorf("want ErrAllowlistViolation or ErrInvalidTarget, got: %v", err)
	}
}
