package coupling

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSymbolVerifier_CrossLanguage(t *testing.T) {
	dir := t.TempDir()
	rust := filepath.Join(dir, "edgeA")
	svelte := filepath.Join(dir, "chatB")
	mustWrite(t, rust, "fanout.rs", `
let secret = std::env::var("RELAY_JWT_SECRET").unwrap();
match m { "peer_joined" => fanout(), _ => {} }`)
	mustWrite(t, svelte, "Call.svelte", `
const secret = import.meta.env.RELAY_JWT_SECRET;
socket.on("peer_joined", () => {});`)

	v := NewSymbolVerifier()
	ev, err := v.Verify(context.Background(),
		FilePair{Repo: "acme-edge", Root: rust, Rel: "fanout.rs"},
		FilePair{Repo: "acme-web", Root: svelte, Rel: "Call.svelte"})
	if err != nil {
		t.Fatal(err)
	}
	got := evidenceDetails(ev)
	assertContains(t, got, "RELAY_JWT_SECRET")
	assertContains(t, got, "peer_joined")
	for _, e := range ev {
		if e.Kind != "symbol" || e.Tier != "offline" {
			t.Errorf("evidence kind/tier = %q/%q, want symbol/offline", e.Kind, e.Tier)
		}
	}
}

func TestSymbolVerifier_NoSharedTokens(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	mustWrite(t, a, "x.rs", `let s = std::env::var("ALPHA_TOKEN_ONE");`)
	mustWrite(t, b, "y.ts", `const s = process.env.BETA_TOKEN_TWO;`)
	ev, err := NewSymbolVerifier().Verify(context.Background(),
		FilePair{Root: a, Rel: "x.rs"}, FilePair{Root: b, Rel: "y.ts"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 0 {
		t.Errorf("expected no evidence, got %v", evidenceDetails(ev))
	}
}

func TestSymbolVerifier_SkipsNoiseFiles(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	b := filepath.Join(dir, "b")
	// Both "share" RELAY_JWT_SECRET but one side is a README (markdown → empty
	// lang → skipped). Release-noise must NEVER verify.
	mustWrite(t, a, "code.rs", `let s = std::env::var("RELAY_JWT_SECRET");`)
	mustWrite(t, b, "README.md", "Set `RELAY_JWT_SECRET` before running.")
	ev, _ := NewSymbolVerifier().Verify(context.Background(),
		FilePair{Root: a, Rel: "code.rs"}, FilePair{Root: b, Rel: "README.md"})
	if len(ev) != 0 {
		t.Errorf("README must be skipped, got evidence %v", evidenceDetails(ev))
	}
}

// --- test helpers ---

func mustWrite(t *testing.T, dir, rel, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func evidenceDetails(ev []Evidence) []string {
	out := make([]string, len(ev))
	for i, e := range ev {
		out[i] = e.Detail
	}
	return out
}

func assertContains(t *testing.T, got []string, want string) {
	t.Helper()
	for _, g := range got {
		if g == want {
			return
		}
	}
	t.Errorf("evidence %v does not contain %q", got, want)
}
