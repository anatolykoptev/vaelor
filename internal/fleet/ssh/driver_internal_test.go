// internal/fleet/ssh/driver_internal_test.go
//
// White-box tests for realExecer internal helpers.
// Package ssh (internal) — directly accesses unexported symbols.
package ssh

import (
	"path/filepath"
	"testing"
)

// TestBuildSSHArgv_NoFFlag verifies that buildSSHArgv without a homeDst
// produces the classic shape: [(-p N)? -- host remote-cmd...].
func TestBuildSSHArgv_NoFFlag(t *testing.T) {
	t.Parallel()
	host := "host-a"
	args := []string{"docker", "ps", "--no-trunc", "--format={{json .}}"}
	got := buildSSHArgv(host, args, "")

	// Without homeDst: no -F flag.
	if len(got) < 2 {
		t.Fatalf("argv too short: %v", got)
	}
	if got[0] == "-F" {
		t.Errorf("unexpected -F flag when homeDst is empty: %v", got)
	}

	// -- host must be present.
	dashDashIdx := -1
	for i, a := range got {
		if a == "--" {
			dashDashIdx = i
			break
		}
	}
	if dashDashIdx < 0 {
		t.Fatalf("-- separator missing from argv: %v", got)
	}
	if got[dashDashIdx+1] != host {
		t.Errorf("host after --: got %q, want %q", got[dashDashIdx+1], host)
	}
}

// TestBuildSSHArgv_NoFFlag_WithPort verifies that -p N is included before --
// and no -F is inserted when homeDst is empty.
func TestBuildSSHArgv_NoFFlag_WithPort(t *testing.T) {
	t.Parallel()
	host := "host-a"
	args := []string{"-p", "1987", "docker", "ps", "--no-trunc", "--format={{json .}}"}
	got := buildSSHArgv(host, args, "")

	if got[0] != "-p" || got[1] != "1987" {
		t.Errorf("port flags not at head: %v", got)
	}
	if got[0] == "-F" {
		t.Errorf("unexpected -F flag: %v", got)
	}
}

// TestBuildSSHArgv_FFlag verifies that buildSSHArgv with a non-empty homeDst
// prepends -F <homeDst>/.ssh/config BEFORE any -p and -- flags.
func TestBuildSSHArgv_FFlag(t *testing.T) {
	t.Parallel()
	host := "host-a"
	args := []string{"docker", "ps", "--no-trunc", "--format={{json .}}"}
	homeDst := "/tmp/fleet-ssh-home"
	got := buildSSHArgv(host, args, homeDst)

	// Must start with -F.
	if len(got) < 2 {
		t.Fatalf("argv too short: %v", got)
	}
	if got[0] != "-F" {
		t.Errorf("argv[0]: want -F, got %q (full argv: %v)", got[0], got)
	}
	wantCfg := filepath.Join(homeDst, ".ssh", "config")
	if got[1] != wantCfg {
		t.Errorf("argv[1]: want %q, got %q", wantCfg, got[1])
	}

	// -- separator must still be present somewhere after the -F block.
	dashDashIdx := -1
	for i, a := range got {
		if a == "--" {
			dashDashIdx = i
			break
		}
	}
	if dashDashIdx < 0 {
		t.Fatalf("-- separator missing from argv: %v", got)
	}
	if got[dashDashIdx+1] != host {
		t.Errorf("host after --: got %q, want %q", got[dashDashIdx+1], host)
	}
}

// TestBuildSSHArgv_FFlag_WithPort verifies that when both a port and a homeDst
// are provided, argv shape is: -F <cfg> -p N -- host remote-cmd...
func TestBuildSSHArgv_FFlag_WithPort(t *testing.T) {
	t.Parallel()
	host := "host-a"
	args := []string{"-p", "1987", "docker", "ps", "--no-trunc", "--format={{json .}}"}
	homeDst := "/tmp/fleet-ssh-home"
	got := buildSSHArgv(host, args, homeDst)

	// Shape: [-F cfg -p 1987 -- host ...]
	if len(got) < 6 {
		t.Fatalf("argv too short: %v", got)
	}
	if got[0] != "-F" {
		t.Errorf("argv[0]: want -F, got %q", got[0])
	}
	wantCfg := filepath.Join(homeDst, ".ssh", "config")
	if got[1] != wantCfg {
		t.Errorf("argv[1]: want %q, got %q", wantCfg, got[1])
	}
	if got[2] != "-p" {
		t.Errorf("argv[2]: want -p, got %q (full argv: %v)", got[2], got)
	}
	if got[3] != "1987" {
		t.Errorf("argv[3]: want 1987, got %q", got[3])
	}
	if got[4] != "--" {
		t.Errorf("argv[4]: want --, got %q", got[4])
	}
	if got[5] != host {
		t.Errorf("argv[5]: want %q, got %q", host, got[5])
	}
}
