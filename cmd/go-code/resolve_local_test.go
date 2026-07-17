package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitInitWithOrigin creates a git repo at dir with origin set to originURL.
func gitInitWithOrigin(t *testing.T, dir, originURL string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"remote", "add", "origin", originURL},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// TestLocalCheckoutFor verifies resolveRoot's local-first helper: a slug whose
// repo lives under one of the configured dirs (with a matching origin remote)
// resolves to the local checkout instead of triggering a clone. The remote-slug
// check guards against same-name-different-owner collisions.
func TestLocalCheckoutFor(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()

	peDir := filepath.Join(base, "acme-edge")
	gitInitWithOrigin(t, peDir, "git@github.com:anatolykoptev/acme-edge.git")
	dirs := []string{base}

	t.Run("matching slug returns local path", func(t *testing.T) {
		if got := localCheckoutFor(ctx, "anatolykoptev/acme-edge", dirs); got != peDir {
			t.Fatalf("want %q, got %q", peDir, got)
		}
	})

	t.Run("no local checkout returns empty (clone fallback)", func(t *testing.T) {
		if got := localCheckoutFor(ctx, "anatolykoptev/does-not-exist", dirs); got != "" {
			t.Fatalf("want empty, got %q", got)
		}
	})

	t.Run("same name different owner returns empty (collision guard)", func(t *testing.T) {
		if got := localCheckoutFor(ctx, "someoneelse/acme-edge", dirs); got != "" {
			t.Fatalf("want empty (remote slug mismatch), got %q", got)
		}
	})

	t.Run("empty dirs returns empty", func(t *testing.T) {
		if got := localCheckoutFor(ctx, "anatolykoptev/acme-edge", nil); got != "" {
			t.Fatalf("want empty, got %q", got)
		}
	})
}
