package main

import (
	"context"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/analyze"
)

func TestInferRepoFromPath_AbsoluteUnderRoot(t *testing.T) {
	dirs := []string{"/host/src"}
	got, ok := inferRepoFromPath("/host/src/go-code/internal/query/ranking.go", dirs)
	if !ok {
		t.Fatalf("expected inference, got ok=false")
	}
	if got != "/host/src/go-code" {
		t.Errorf("inferred repo = %q, want /host/src/go-code", got)
	}
}

func TestInferRepoFromPath_RelativeNotInferred(t *testing.T) {
	dirs := []string{"/host/src"}
	if _, ok := inferRepoFromPath("internal/query", dirs); ok {
		t.Errorf("relative path must not be inferred")
	}
}

func TestInferRepoFromPath_EmptyNotInferred(t *testing.T) {
	if _, ok := inferRepoFromPath("", []string{"/host/src"}); ok {
		t.Errorf("empty path must not be inferred")
	}
}

func TestInferRepoFromPath_OutsideRootsNotInferred(t *testing.T) {
	dirs := []string{"/host/src"}
	if _, ok := inferRepoFromPath("/etc/passwd", dirs); ok {
		t.Errorf("path outside roots must not be inferred")
	}
}

func TestInferRepoFromPath_ParentTraversalRejected(t *testing.T) {
	dirs := []string{"/host/src"}
	if _, ok := inferRepoFromPath("/host/src/../etc/passwd", dirs); ok {
		t.Errorf("parent-traversal path must not be inferred")
	}
}

func TestInferRepoFromPath_MultipleRoots(t *testing.T) {
	dirs := []string{"/host/src", "/opt/repos"}
	got, ok := inferRepoFromPath("/opt/repos/vaelor/cmd/main.go", dirs)
	if !ok || got != "/opt/repos/vaelor" {
		t.Errorf("multi-root: expected /opt/repos/vaelor, got %q (ok=%v)", got, ok)
	}
}

func TestShortMissingRepoMsg_FallsBackToDirBasename(t *testing.T) {
	// No store → fall back to LocalRepoDirs basenames.
	dirs := []string{"/host/src/go-nerv", "/host/src/vaelor", "/host/src/go-wp"}
	msg := shortMissingRepoMsg(context.Background(), nil, dirs)
	if !strings.HasPrefix(msg, `missing "repo" — e.g.`) {
		t.Errorf("message must start with missing-repo prefix: %q", msg)
	}
	if !strings.Contains(msg, "go-nerv") || !strings.Contains(msg, "vaelor") || !strings.Contains(msg, "go-wp") {
		t.Errorf("message should name the 3 dir basenames: %q", msg)
	}
}

func TestShortMissingRepoMsg_NoDirsNoStore(t *testing.T) {
	msg := shortMissingRepoMsg(context.Background(), nil, nil)
	if !strings.Contains(msg, `missing "repo"`) {
		t.Errorf("message must mention missing repo: %q", msg)
	}
	if strings.Contains(msg, "e.g.") {
		t.Errorf("with no candidates the message should not fake examples: %q", msg)
	}
}

func TestResolveOrInferRepo_RepoPresentPassesThrough(t *testing.T) {
	deps := analyze.Deps{LocalRepoDirs: []string{"/host/src"}}
	repo, note, ok := resolveOrInferRepo("owner/repo", "/host/src/x", "", deps)
	if !ok || repo != "owner/repo" || note != "" {
		t.Errorf("repo present should pass through unchanged, got repo=%q note=%q ok=%v", repo, note, ok)
	}
}

func TestResolveOrInferRepo_InfersAndNotes(t *testing.T) {
	deps := analyze.Deps{LocalRepoDirs: []string{"/host/src"}}
	repo, note, ok := resolveOrInferRepo("", "/host/src/go-code/internal/query", "", deps)
	if !ok || repo != "/host/src/go-code" {
		t.Errorf("expected inference to /host/src/go-code, got repo=%q ok=%v", repo, ok)
	}
	if !strings.Contains(note, "inferred repo") {
		t.Errorf("note should mention inference, got %q", note)
	}
}

func TestResolveOrInferRepo_MissingNoPathReturnsFalse(t *testing.T) {
	deps := analyze.Deps{LocalRepoDirs: []string{"/host/src"}}
	_, _, ok := resolveOrInferRepo("", "", "", deps)
	if ok {
		t.Errorf("missing repo + no path should return ok=false (caller emits short error)")
	}
}

// TestShortMissingRepoMsg_TableDriven guards #563(b): the missing-repo error
// must be a SHORT, first-line-actionable message naming the field and the
// accepted forms (owner/repo or /host/src/<name>) — never a JSON-schema dump.
// Each row falsifies by reverting shortMissingRepoMsg to a schema-dump style.
func TestShortMissingRepoMsg_TableDriven(t *testing.T) {
	dirs := []string{"/host/src/go-nerv", "/host/src/vaelor", "/host/src/go-wp"}
	cases := []struct {
		name  string
		dirs  []string
		store bool
	}{
		{"with_dir_basenames", dirs, false},
		{"no_dirs_no_store", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := shortMissingRepoMsg(context.Background(), nil, tc.dirs)
			if len(msg) > 200 {
				t.Errorf("missing-repo message must be SHORT (<200 chars), got %d: %q", len(msg), msg)
			}
			if !strings.Contains(msg, `"repo"`) {
				t.Errorf("message must name the \"repo\" field: %q", msg)
			}
			// Must NOT look like a JSON-schema validation dump.
			if strings.Contains(msg, "validating arguments") || strings.Contains(msg, `"required"`) || strings.Contains(msg, `"properties"`) {
				t.Errorf("message must not be a schema dump: %q", msg)
			}
		})
	}
}
