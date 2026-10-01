package workspace

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const sentinelCloneToken = "ghs_SENTINELCLONE789"

func basicFor(tok string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+tok))
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), args[0], args[1:]...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
}

// TestRemoteSource_401_DoesNotLeakTokenAndSendsHeader clones from a server that
// always answers 401. The token must (a) arrive as an Authorization header on
// the FIRST request — i.e. it is not in the URL, where git would only retry
// with it after a 401 — and (b) be absent from the returned error.
func TestRemoteSource_401_DoesNotLeakTokenAndSendsHeader(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auths = append(auths, r.Header.Get("Authorization"))
		mu.Unlock()
		w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	src := RemoteSource{
		Slug:        "o/r",
		DestDir:     t.TempDir(),
		StaticToken: sentinelCloneToken,
		Host:        srv.URL,
	}
	_, cleanup, err := src.Root(context.Background())
	cleanup()
	if err == nil {
		t.Fatal("expected clone error against a 401 server")
	}
	if strings.Contains(err.Error(), sentinelCloneToken) {
		t.Errorf("token leaked in error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(auths) == 0 {
		t.Fatal("server saw no requests")
	}
	if auths[0] != basicFor(sentinelCloneToken) {
		t.Errorf("first request Authorization = %q, want the extraheader Basic credential (token must not travel in the URL)", auths[0])
	}
}

// TestRemoteSource_ClonesWithHeaderAuth clones a repo served by git-http-backend
// behind a gate that demands the Basic credential, and checks the token was not
// persisted in the clone's config.
func TestRemoteSource_ClonesWithHeaderAuth(t *testing.T) {
	t.Parallel()
	out, err := exec.CommandContext(context.Background(), "git", "--exec-path").Output()
	if err != nil {
		t.Skip("git not available")
	}
	backend := filepath.Join(strings.TrimSpace(string(out)), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skipf("git-http-backend not available: %v", err)
	}

	root := t.TempDir()
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, work, "git", "init", "-q", "-b", "main")
	run(t, work, "git", "config", "user.email", "t@example.com")
	run(t, work, "git", "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, work, "git", "add", ".")
	run(t, work, "git", "commit", "-q", "-m", "init")
	srvRoot := filepath.Join(root, "srv")
	if err := os.MkdirAll(srvRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, root, "git", "clone", "-q", "--bare", work, filepath.Join(srvRoot, "r.git"))
	// The slug is "o/r"; serve the repo at /o/r.git.
	if err := os.MkdirAll(filepath.Join(srvRoot, "o"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(srvRoot, "r.git"), filepath.Join(srvRoot, "o", "r.git")); err != nil {
		t.Fatal(err)
	}

	cgiH := &cgi.Handler{Path: backend, Env: []string{"GIT_PROJECT_ROOT=" + srvRoot, "GIT_HTTP_EXPORT_ALL=1"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != basicFor(sentinelCloneToken) {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		cgiH.ServeHTTP(w, r)
	}))
	defer srv.Close()

	src := RemoteSource{Slug: "o/r", DestDir: t.TempDir(), StaticToken: sentinelCloneToken, Host: srv.URL}
	dir, cleanup, err := src.Root(context.Background())
	if err != nil {
		t.Fatalf("clone with header auth failed: %v", err)
	}
	defer cleanup()
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Fatalf("README.md missing from clone: %v", err)
	}
	cfg, err := os.ReadFile(filepath.Join(dir, ".git", "config"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg), sentinelCloneToken) {
		t.Errorf("token persisted in .git/config:\n%s", cfg)
	}
}
