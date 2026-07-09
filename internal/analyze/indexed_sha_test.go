package analyze

import (
	"context"
	"testing"
)

// TestIndexedSHA_NilResolverReturnsEmpty verifies the cold-path guarantee:
// with no resolver configured, IndexedSHA returns "" (never panics, never
// errors) so WithFreshness stays silent.
func TestIndexedSHA_NilResolverReturnsEmpty(t *testing.T) {
	t.Parallel()
	var d Deps // IndexedSHAFunc nil
	if got := d.IndexedSHA(context.Background(), "any/repo"); got != "" {
		t.Fatalf("nil resolver must return empty, got %q", got)
	}
}

// TestIndexedSHA_ResolverWins verifies the accessor delegates to the func.
func TestIndexedSHA_ResolverWins(t *testing.T) {
	t.Parallel()
	d := Deps{
		IndexedSHAFunc: func(_ context.Context, repoKey string) string {
			if repoKey == "known/repo" {
				return "abc123"
			}
			return ""
		},
	}
	if got := d.IndexedSHA(context.Background(), "known/repo"); got != "abc123" {
		t.Fatalf("resolver hit: got %q, want abc123", got)
	}
	if got := d.IndexedSHA(context.Background(), "unknown/repo"); got != "" {
		t.Fatalf("resolver miss: got %q, want empty", got)
	}
}
