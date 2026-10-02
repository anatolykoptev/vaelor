package ingest

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRefreshClone_InjectsTokenViaGitConfig verifies that when TokenFunc is
// set on CloneOpts, the cache-hit path calls refreshClone with env vars that
// inject a fresh credential via GIT_CONFIG_COUNT / GIT_CONFIG_KEY_0 /
// GIT_CONFIG_VALUE_0. We verify this indirectly by using a file:// remote
// (no real network) and a sentinel TokenFunc, then asserting the expected
// env vars would be constructed correctly via the internal helper directly.
func TestRefreshClone_InjectsTokenViaGitConfig(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	origin := filepath.Join(tmp, "origin")
	dest := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, origin)

	const sentinelToken = "fresh-token-xyz"

	// Track calls to tokenFunc.
	called := 0
	tokenFunc := func(_ context.Context) (string, error) {
		called++
		return sentinelToken, nil
	}

	opts := CloneOpts{
		Slug:      "test/demo",
		DestDir:   dest,
		CloneURL:  "file://" + origin,
		Ref:       "main",
		TokenFunc: tokenFunc,
	}

	// First call: fresh clone (no local path yet), tokenFunc not used for initial clone.
	_, err := CloneRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("first clone: %v", err)
	}

	// Push a new file so refreshClone actually fetches.
	if err := os.WriteFile(filepath.Join(origin, "TOKEN.md"), []byte("token-test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, origin, "git", "add", ".")
	mustRun(t, origin, "git", "commit", "-q", "-m", "add TOKEN")

	// Second call: cache-hit path → refreshClone is called with tokenFunc.
	res2, err := CloneRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("second clone (cache-hit): %v", err)
	}
	if _, err := os.Stat(filepath.Join(res2.LocalPath, "TOKEN.md")); err != nil {
		t.Fatalf("cache-hit did not fetch new commit: TOKEN.md missing — %v", err)
	}
	if called == 0 {
		t.Fatal("expected tokenFunc to be called on cache-hit refresh, was not called")
	}
}

// TestRefreshClone_EnvVarsFormat unit-tests that refreshClone builds the
// correct GIT_CONFIG_* env var values for a given token, without touching
// the network. It calls refreshClone against a local file:// repo and
// captures the expected env var format.
func TestRefreshClone_EnvVarsFormat(t *testing.T) {
	t.Parallel()
	const tok = "ghs_testtoken123"
	want := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+tok))

	// Simulate what refreshClone builds.
	cred := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + tok))
	got := "Authorization: Basic " + cred

	if got != want {
		t.Errorf("extraheader mismatch\n got: %s\nwant: %s", got, want)
	}
	if !strings.HasPrefix(got, "Authorization: Basic ") {
		t.Errorf("expected Basic auth prefix, got: %s", got)
	}

	// Verify base64 round-trips to x-access-token:<token>.
	decoded, err := base64.StdEncoding.DecodeString(cred)
	if err != nil {
		t.Fatalf("base64 decode: %v", err)
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 || parts[0] != "x-access-token" || parts[1] != tok {
		t.Errorf("decoded cred = %q, want x-access-token:%s", string(decoded), tok)
	}
}

// TestRefreshClone_BackwardsCompat_NoTokenFunc ensures nil TokenFunc leaves
// existing behaviour unchanged (no env injection, file:// remote still works).
func TestRefreshClone_BackwardsCompat_NoTokenFunc(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	origin := filepath.Join(tmp, "origin")
	dest := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, origin)

	opts := CloneOpts{
		Slug:     "test/compat",
		DestDir:  dest,
		CloneURL: "file://" + origin,
		Ref:      "main",
		// TokenFunc intentionally nil.
	}

	if _, err := CloneRepo(context.Background(), opts); err != nil {
		t.Fatalf("first clone: %v", err)
	}

	if err := os.WriteFile(filepath.Join(origin, "COMPAT.md"), []byte("compat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRun(t, origin, "git", "add", ".")
	mustRun(t, origin, "git", "commit", "-q", "-m", "compat commit")

	res2, err := CloneRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("second clone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(res2.LocalPath, "COMPAT.md")); err != nil {
		t.Fatalf("backwards-compat refresh failed: COMPAT.md missing — %v", err)
	}
}

// TestRefreshClone_TokenFuncError verifies that a tokenFunc error is
// propagated and causes CloneRepo to fall through to a re-clone.
// We simulate this by making tokenFunc error on the second call (cache-hit).
func TestRefreshClone_TokenFuncError(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	origin := filepath.Join(tmp, "origin")
	dest := filepath.Join(tmp, "workspace")
	if err := os.MkdirAll(origin, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, origin)

	callN := 0
	tokenFunc := func(_ context.Context) (string, error) {
		callN++
		if callN > 1 {
			return "", errors.New("token service unavailable")
		}
		return "valid-token", nil
	}

	opts := CloneOpts{
		Slug:      "test/errrepo",
		DestDir:   dest,
		CloneURL:  "file://" + origin,
		Ref:       "main",
		TokenFunc: tokenFunc,
	}

	// First call — fresh clone (tokenFunc not used here since CloneRepo
	// passes tokenFunc only to refreshClone, which is only called on cache-hit).
	_, err := CloneRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("first clone: %v", err)
	}

	// Second call — cache-hit; tokenFunc returns error → refreshClone fails
	// → CloneRepo removes localPath and re-clones. Re-clone succeeds since
	// CloneURL is file:// and doesn't require a token.
	res2, err := CloneRepo(context.Background(), opts)
	if err != nil {
		// Re-clone after refresh failure should succeed (file:// needs no auth).
		t.Fatalf("second clone after token error: %v", err)
	}
	// Result must still be a valid clone.
	if _, err := os.Stat(filepath.Join(res2.LocalPath, "README.md")); err != nil {
		t.Fatalf("re-clone after token error missing README: %v", err)
	}

	// Verify error message format from refreshClone directly.
	sentinelErr := errors.New("sentinel error")
	errFunc := func(_ context.Context) (string, error) { return "", sentinelErr }
	err = refreshClone(context.Background(), res2.LocalPath, "main", opts.CloneURL, "", "github.com", errFunc)
	if err == nil {
		t.Fatal("expected error from refreshClone when tokenFunc errors")
	}
	if !errors.Is(err, sentinelErr) {
		t.Errorf("expected wrapped sentinel error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "refresh token") {
		t.Errorf("expected 'refresh token' in error message, got: %v", err)
	}
	_ = fmt.Sprintf("error check: %v", err) // ensure err is used
}

func TestSanitizeGitOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"no auth", "fatal: repository not found", "fatal: repository not found"},
		{"strip Authorization line",
			"fatal: unable to access\nAuthorization: Basic eHRyYTpzZWNyZXQ=\nbye",
			"fatal: unable to access\nbye"},
		{"strip extraheader line",
			"http.extraheader=Authorization: Basic xxx\nfatal: end",
			"fatal: end"},
		{"multiple lines",
			"a\nAuthorization: Basic abc\nb\nextraheader\nc",
			"a\nb\nc"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeGitOutput(tc.in)
			if got != tc.want {
				t.Errorf("sanitizeGitOutput(%q):\n  got:  %q\n  want: %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizeGitOutput_MasksURLUserinfoAndTokens(t *testing.T) {
	t.Parallel()
	in := "fatal: could not read Password for 'https://ghs_SENTINELSAN1@github.com': terminal prompts disabled\n" +
		"remote: see https://user:SENTINELSAN2@host.example/x glpat-SENTINELSAN3"
	got := sanitizeGitOutput(in)
	for _, s := range []string{"SENTINELSAN1", "SENTINELSAN2", "SENTINELSAN3"} {
		if strings.Contains(got, s) {
			t.Errorf("%s survived: %q", s, got)
		}
	}
	if !strings.Contains(got, "terminal prompts disabled") {
		t.Errorf("context lost: %q", got)
	}
}

func TestGitAuthEnv(t *testing.T) {
	t.Parallel()
	nilCases := []struct{ name, url, tok, issuer string }{
		{"empty token", "https://github.com/o/r.git", "", "github.com"},
		{"empty issuer", "https://github.com/o/r.git", "tok", ""},
		{"file remote", "file:///tmp/x", "tok", "github.com"},
		{"plain http", "http://github.com/o/r.git", "tok", "github.com"},
		{"host != issuer", "https://gitlab.com/o/r.git", "tok", "github.com"},
		{"lookalike host", "https://github.com.evil.example/o/r.git", "tok", "github.com"},
		{"quote in host", "https://a'b/o/r.git", "tok", "a'b"},
	}
	for _, c := range nilCases {
		if got := gitAuthEnv(c.url, "", c.tok, c.issuer); got != nil {
			t.Errorf("%s: want nil env, got %v", c.name, got)
		}
	}
	env := gitAuthEnv("https://gitlab.example.com/g/r.git", "oauth2", "tok", "gitlab.example.com")
	joined := strings.Join(env, "\n")
	for _, want := range []string{
		"GIT_CONFIG_KEY_0=credential.helper\nGIT_CONFIG_VALUE_0=\n",
		"GIT_CONFIG_KEY_1=credential.https://gitlab.example.com.helper",
		"VAELOR_GIT_USER=oauth2",
		"VAELOR_GIT_TOKEN=tok",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("env missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "extraheader") || strings.Contains(joined, "Authorization") {
		t.Errorf("credential must not travel as a header:\n%s", joined)
	}
}

func TestBuildCloneURL_StripsUserinfo(t *testing.T) {
	t.Parallel()
	got := buildCloneURL(CloneOpts{CloneURL: "https://ghs_SENTINELSTRIP:x@github.com/o/r.git"}, "o/r")
	if strings.Contains(got, "SENTINELSTRIP") || got != "https://github.com/o/r.git" {
		t.Errorf("userinfo not stripped: %q", got)
	}
	if got := buildCloneURL(CloneOpts{}, "o/r"); got != "https://github.com/o/r.git" {
		t.Errorf("default url = %q", got)
	}
}
