package mcpmeta

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func mkRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "t@t.t"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-m", "x"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func TestLiveHead_DirectRef(t *testing.T) {
	dir := mkRepo(t)
	sha, err := LiveHead(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(sha) != 40 {
		t.Fatalf("sha: got %q (len=%d), want 40-hex", sha, len(sha))
	}
}

func TestLiveHead_PackedRefs(t *testing.T) {
	dir := mkRepo(t)
	refPath := filepath.Join(dir, ".git", "refs", "heads", "main")
	loose, err := os.ReadFile(refPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, ".git", "packed-refs"),
		[]byte("# pack-refs with: peeled fully-peeled sorted\n"+string(loose)[:40]+" refs/heads/main\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(refPath); err != nil {
		t.Fatal(err)
	}
	sha, err := LiveHead(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(sha) != 40 {
		t.Fatalf("packed-refs path: got %q", sha)
	}
}

func TestWithFreshness_MatchSilent(t *testing.T) {
	dir := mkRepo(t)
	sha, _ := LiveHead(dir)
	env := WithFreshness(Wrap(1, ""), dir, sha)
	if env.StaleWarning != "" {
		t.Fatalf("match must be silent, got: %q", env.StaleWarning)
	}
	if env.IndexedSHA != "" || env.LiveSHA != "" {
		t.Fatalf("match must not surface SHAs")
	}
}

func TestWithFreshness_MismatchSpeaks(t *testing.T) {
	dir := mkRepo(t)
	env := WithFreshness(Wrap(1, ""), dir, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	if env.StaleWarning == "" {
		t.Fatalf("mismatch must populate stale_warning")
	}
	if env.IndexedSHA == "" || env.LiveSHA == "" {
		t.Fatalf("mismatch must surface both SHAs")
	}
}
