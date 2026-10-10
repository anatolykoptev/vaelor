package scip_test

import (
	"os"
	"path/filepath"
	"testing"

	gocodescip "github.com/anatolykoptev/vaelor/internal/scip"
)

func TestAllowIndexer_Decision(t *testing.T) {
	trusted := t.TempDir()
	inside := filepath.Join(trusted, "repo")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	// A symlink inside the trusted dir that points at an untrusted tree must
	// not launder it into the trusted set.
	link := filepath.Join(trusted, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(inside+"-evil", 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		roots []string
		lang  string
		root  string
		want  bool
	}{
		{"rust untrusted, nothing configured", nil, "rust", outside, false},
		{"python untrusted", []string{trusted}, "python", outside, false},
		{"java untrusted", []string{trusted}, "java", outside, false},
		{"unknown lang untrusted (deny by default)", []string{trusted}, "csharp", outside, false},
		{"typescript untrusted ok", nil, "typescript", outside, true},
		{"javascript untrusted ok", nil, "javascript", outside, true},
		{"rust trusted", []string{trusted}, "rust", inside, true},
		{"rust symlink escapes trusted dir", []string{trusted}, "rust", link, false},
		{"rust sibling prefix is not inside", []string{inside}, "rust", inside + "-evil", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gocodescip.SetTrustedRoots(tc.roots)
			t.Cleanup(func() { gocodescip.SetTrustedRoots(nil) })
			if got := gocodescip.AllowIndexer(tc.lang, tc.root); got != tc.want {
				t.Errorf("AllowIndexer(%q, %q) = %v, want %v", tc.lang, tc.root, got, tc.want)
			}
		})
	}
}
