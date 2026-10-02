package workspace

import (
	"context"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const (
	sentinelCloneToken = "ghs_SENTINELCLONE789"
	sentinelGitLab     = "glpat-SENTINELGITLAB1"
)

// TestMain points git at the httptest TLS certificate: credentials are only
// ever sent over https, so every auth test talks TLS.
func TestMain(m *testing.M) {
	s := httptest.NewTLSServer(http.NotFoundHandler())
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.Certificate().Raw})
	s.Close()
	f, err := os.CreateTemp("", "vaelor-ca-*.pem")
	if err != nil {
		panic(err)
	}
	if _, err := f.Write(pemBytes); err != nil {
		panic(err)
	}
	_ = f.Close()
	_ = os.Setenv("GIT_SSL_CAINFO", f.Name())
	code := m.Run()
	_ = os.Remove(f.Name())
	os.Exit(code)
}

func basicFor(user, tok string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+tok))
}

func run(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), args[0], args[1:]...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out)
	}
}

func hostOf(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Host
}

// recorder logs the Authorization header of every request a server receives.
type recorder struct {
	mu    sync.Mutex
	auths []string
}

func (r *recorder) add(a string) {
	r.mu.Lock()
	r.auths = append(r.auths, a)
	r.mu.Unlock()
}

func (r *recorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.auths...)
}

func (r *recorder) count(want string) int {
	n := 0
	for _, a := range r.all() {
		if a == want {
			n++
		}
	}
	return n
}

func (r *recorder) anyContains(sub string) bool {
	for _, a := range r.all() {
		if strings.Contains(a, sub) {
			return true
		}
	}
	return false
}

// rejecting returns a TLS server that always answers 401.
func rejecting(rec *recorder) *httptest.Server {
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.add(r.Header.Get("Authorization"))
		w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
}

// TestRemoteSource_401_DoesNotLeakTokenAndSendsCredential clones from a server
// that always answers 401. The credential must reach the issuer host (via the
// credential helper, after the 401 challenge) and the token must be absent from
// the returned error. A token in the URL cannot satisfy the challenge, so no
// request would ever carry the Basic credential.
func TestRemoteSource_401_DoesNotLeakTokenAndSendsCredential(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	srv := rejecting(rec)
	defer srv.Close()

	src := RemoteSource{
		Slug: "o/r", DestDir: t.TempDir(), StaticToken: sentinelCloneToken,
		Host: srv.URL, TokenHost: hostOf(t, srv.URL),
	}
	_, cleanup, err := src.Root(context.Background())
	cleanup()
	if err == nil {
		t.Fatal("expected clone error against a 401 server")
	}
	if strings.Contains(err.Error(), sentinelCloneToken) {
		t.Errorf("token leaked in error: %v", err)
	}
	if rec.count(basicFor("x-access-token", sentinelCloneToken)) == 0 {
		t.Errorf("no request carried the Basic credential; saw %q", rec.all())
	}
}

// TestRemoteSource_NonCanonicalHostGetsNoCredential: a Host override without a
// matching TokenHost must never receive the GitHub token (SEC-CR-001).
func TestRemoteSource_NonCanonicalHostGetsNoCredential(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	srv := rejecting(rec)
	defer srv.Close()

	src := RemoteSource{Slug: "o/r", DestDir: t.TempDir(), StaticToken: sentinelCloneToken, Host: srv.URL}
	_, cleanup, err := src.Root(context.Background())
	cleanup()
	if err == nil {
		t.Fatal("expected clone error")
	}
	if len(rec.all()) == 0 {
		t.Fatal("server saw no requests")
	}
	for _, a := range rec.all() {
		if a != "" {
			t.Errorf("non-canonical host received a credential: %q", a)
		}
	}
}

// TestRemoteSource_GitLabNeverGetsGitHubToken: a GitLab clone uses only
// GitLabToken; the GitHub token and TokenFunc are never sent.
func TestRemoteSource_GitLabNeverGetsGitHubToken(t *testing.T) {
	t.Parallel()
	rec := &recorder{}
	srv := rejecting(rec)
	defer srv.Close()

	src := RemoteSource{
		Slug: "o/r", RepoInput: "https://gitlab.com/o/r", DestDir: t.TempDir(),
		StaticToken: sentinelCloneToken, GitLabToken: sentinelGitLab,
		Host: srv.URL, TokenHost: hostOf(t, srv.URL),
	}
	_, cleanup, _ := src.Root(context.Background())
	cleanup()
	if rec.count(basicFor("oauth2", sentinelGitLab)) == 0 {
		t.Errorf("GitLab token not sent as oauth2 credential; saw %q", rec.all())
	}
	if rec.anyContains(base64.StdEncoding.EncodeToString([]byte("x-access-token:"+sentinelCloneToken))) ||
		rec.anyContains(base64.StdEncoding.EncodeToString([]byte("oauth2:"+sentinelCloneToken))) {
		t.Errorf("GitHub token reached a GitLab clone: %q", rec.all())
	}

	// Without a GitLab token nothing is sent, even with a GitHub token set.
	rec2 := &recorder{}
	srv2 := rejecting(rec2)
	defer srv2.Close()
	src2 := RemoteSource{
		Slug: "o/r", RepoInput: "https://gitlab.com/o/r", DestDir: t.TempDir(),
		StaticToken: sentinelCloneToken, Host: srv2.URL, TokenHost: hostOf(t, srv2.URL),
	}
	_, cleanup2, _ := src2.Root(context.Background())
	cleanup2()
	for _, a := range rec2.all() {
		if a != "" {
			t.Errorf("credential sent to GitLab without a GitLab token: %q", a)
		}
	}
}

// TestRemoteSource_RedirectToOtherHostGetsNoCredential: a 302 from the issuer
// host to another host that serves the repo (so git follows with further
// requests) must not hand that host the credential (SEC-CR-002).
func TestRemoteSource_RedirectToOtherHostGetsNoCredential(t *testing.T) {
	t.Parallel()
	other := newGitServer(t, "") // open server: serves the clone, records Authorization

	issuer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.RequestURI(), http.StatusFound)
	}))
	defer issuer.Close()

	src := RemoteSource{
		Slug: "o/r", DestDir: t.TempDir(), StaticToken: sentinelCloneToken,
		Host: issuer.URL, TokenHost: hostOf(t, issuer.URL),
	}
	_, cleanup, err := src.Root(context.Background())
	if err != nil {
		t.Fatalf("clone through redirect failed (the open target rejects any credential it is sent; it saw %q): %v", other.rec.all(), err)
	}
	cleanup()
	if len(other.rec.all()) < 2 {
		t.Fatalf("redirect target saw %d requests; test did not exercise follow-up requests", len(other.rec.all()))
	}
	for _, a := range other.rec.all() {
		if a != "" {
			t.Errorf("redirect target received a credential: %q", a)
		}
	}
}

// gitServer is a TLS git-http-backend that requires the Basic credential.
type gitServer struct {
	*httptest.Server
	rec  *recorder
	work string // non-bare working repo (commit here, then push)
	bare string
}

func newGitServer(t *testing.T, wantAuth string) *gitServer {
	t.Helper()
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
	if err := os.MkdirAll(filepath.Join(srvRoot, "o"), 0o755); err != nil {
		t.Fatal(err)
	}
	bare := filepath.Join(srvRoot, "o", "r.git")
	run(t, root, "git", "clone", "-q", "--bare", work, bare)

	cgiH := &cgi.Handler{Path: backend, Env: []string{"GIT_PROJECT_ROOT=" + srvRoot, "GIT_HTTP_EXPORT_ALL=1"}}
	rec := &recorder{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := r.Header.Get("Authorization")
		rec.add(a)
		if a != wantAuth {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		cgiH.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return &gitServer{Server: srv, rec: rec, work: work, bare: bare}
}

// TestRemoteSource_ClonesWithHelperAuth clones from a server that demands the
// Basic credential, and checks the token was not persisted in the clone config.
func TestRemoteSource_ClonesWithHelperAuth(t *testing.T) {
	t.Parallel()
	gs := newGitServer(t, basicFor("x-access-token", sentinelCloneToken))
	src := RemoteSource{Slug: "o/r", DestDir: t.TempDir(), StaticToken: sentinelCloneToken, Host: gs.URL, TokenHost: hostOf(t, gs.URL)}
	dir, cleanup, err := src.Root(context.Background())
	if err != nil {
		t.Fatalf("clone with helper auth failed: %v", err)
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

// TestRemoteSource_RefreshIsAuthenticated: a second Root on a cached clone must
// refresh in place through the authenticated gate. A refresh that loses its
// credential fails and falls back to an atomic re-clone, which replaces the
// directory and wipes the untracked marker.
func TestRemoteSource_RefreshIsAuthenticated(t *testing.T) {
	t.Parallel()
	gs := newGitServer(t, basicFor("x-access-token", sentinelCloneToken))
	dest := t.TempDir()
	src := RemoteSource{Slug: "o/r", DestDir: dest, StaticToken: sentinelCloneToken, Host: gs.URL, TokenHost: hostOf(t, gs.URL)}

	dir, cleanup, err := src.Root(context.Background())
	if err != nil {
		t.Fatalf("first clone: %v", err)
	}
	defer cleanup()
	marker := filepath.Join(dir, "UNTRACKED_MARKER")
	if err := os.WriteFile(marker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(gs.work, "NEW.md"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, gs.work, "git", "add", ".")
	run(t, gs.work, "git", "commit", "-q", "-m", "second")
	run(t, gs.work, "git", "push", "-q", gs.bare, "main")

	dir2, cleanup2, err := src.Root(context.Background())
	if err != nil {
		t.Fatalf("second Root (refresh): %v", err)
	}
	defer cleanup2()
	if dir2 != dir {
		t.Fatalf("unexpected dir change %q -> %q", dir, dir2)
	}
	if _, err := os.Stat(filepath.Join(dir2, "NEW.md")); err != nil {
		t.Errorf("refresh did not fetch the new commit: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("clone was replaced by a re-clone instead of refreshed in place (marker gone): %v", err)
	}
	if ents, _ := filepath.Glob(filepath.Join(dest, "*.tmp.*")); len(ents) != 0 {
		t.Errorf("leftover re-clone siblings: %v", ents)
	}
}
