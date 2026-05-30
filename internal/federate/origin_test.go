package federate

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// setOrigin points dir's origin remote at url (dir must already be a git repo).
func setOrigin(t *testing.T, dir, url string) {
	t.Helper()
	if out, err := exec.Command("git", "-C", dir, "remote", "add", "origin", url).CombinedOutput(); err != nil {
		t.Fatalf("git remote add: %v\n%s", err, out)
	}
}

func TestResolveRepos_DedupsByOrigin(t *testing.T) {
	parent := t.TempDir()
	chat := filepath.Join(parent, "oxpulse-chat")
	chatDev := filepath.Join(parent, "oxpulse-chat-dev")
	admin := filepath.Join(parent, "oxpulse-admin")
	gitInit(t, chat)
	gitInit(t, chatDev)
	gitInit(t, admin)
	setOrigin(t, chat, "git@github.com:anatolykoptev/oxpulse-chat.git")
	setOrigin(t, chatDev, "git@github.com:anatolykoptev/oxpulse-chat.git")
	setOrigin(t, admin, "git@github.com:anatolykoptev/oxpulse-admin.git")

	got, err := ResolveRepos("oxpulse-*", []string{parent})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("dedup → 2 distinct origins (chat, admin), got %d: %v", len(got), got)
	}
	slugs := map[string]bool{}
	for _, r := range got {
		slugs[r.Slug] = true
	}
	if !slugs["oxpulse-chat"] || !slugs["oxpulse-admin"] {
		t.Fatalf("expected chat+admin, got %v", got)
	}
	if slugs["oxpulse-chat-dev"] {
		t.Fatalf("chat-dev (duplicate origin) must be dropped, got %v", got)
	}
}

func TestResolveRepos_NoOriginKeptDistinct(t *testing.T) {
	parent := t.TempDir()
	gitInit(t, filepath.Join(parent, "repo-a"))
	gitInit(t, filepath.Join(parent, "repo-b"))

	got, err := ResolveRepos("all", []string{parent})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("origin-less repos stay distinct → 2, got %d: %v", len(got), got)
	}
}

func TestRepoIdentity(t *testing.T) {
	cases := []struct{ in, want string }{
		{"git@github.com:anatolykoptev/oxpulse-chat.git", "anatolykoptev/oxpulse-chat"},
		{"https://github.com/anatolykoptev/oxpulse-chat.git", "anatolykoptev/oxpulse-chat"},
		{"https://github.com/anatolykoptev/oxpulse-chat", "anatolykoptev/oxpulse-chat"},
		{"git@self-hosted.example:team/svc.git", "git@self-hosted.example:team/svc.git"}, // unknown host → raw fallback
		{"", ""},
	}
	for _, c := range cases {
		if got := repoIdentity(c.in); got != c.want {
			t.Errorf("repoIdentity(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A repo reachable via SSH and another via HTTPS but pointing at the SAME
// GitHub repo must collapse (slugparse canonicalizes both to "owner/repo").
func TestResolveRepos_DedupsAcrossSSHAndHTTPS(t *testing.T) {
	parent := t.TempDir()
	a := filepath.Join(parent, "svc-ssh")
	b := filepath.Join(parent, "svc-https")
	gitInit(t, a)
	gitInit(t, b)
	setOrigin(t, a, "git@github.com:anatolykoptev/oxpulse-chat.git")
	setOrigin(t, b, "https://github.com/anatolykoptev/oxpulse-chat.git")

	got, err := ResolveRepos("all", []string{parent})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("SSH+HTTPS of one repo must collapse to 1, got %d: %v", len(got), got)
	}
}

// When two checkouts share an origin, the lexically-first directory wins
// (Discover returns lexical order; dedupeByOrigin keeps the first occurrence).
func TestResolveRepos_DedupKeepsLexicallyFirstCheckout(t *testing.T) {
	parent := t.TempDir()
	chat := filepath.Join(parent, "oxpulse-chat")
	chatDev := filepath.Join(parent, "oxpulse-chat-dev")
	gitInit(t, chat)
	gitInit(t, chatDev)
	setOrigin(t, chat, "git@github.com:anatolykoptev/oxpulse-chat.git")
	setOrigin(t, chatDev, "git@github.com:anatolykoptev/oxpulse-chat.git")

	got, err := ResolveRepos("oxpulse-*", []string{parent})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 after collapse, got %d: %v", len(got), got)
	}
	if got[0].Slug != "oxpulse-chat" {
		t.Fatalf("lexically-first checkout must win: want oxpulse-chat, got %q", got[0].Slug)
	}
}
