package main

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/fsutil/fsutiltest"
)

func TestReadREADME_RefusesSymlinkedReadme(t *testing.T) {
	outside := fsutiltest.WriteOutside(t, "Secret value is "+fsutiltest.Token)
	for name, target := range map[string]string{
		"canary outside the repo": outside,
		"endless device":          fsutiltest.EndlessDevice,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			fsutiltest.Symlink(t, root, "README.md", target)
			var got string
			fsutiltest.Within(t, func() { got = readREADME(root) })
			if got != "" || strings.Contains(got, fsutiltest.Token) {
				t.Fatalf("symlinked README must yield nothing, got %q", got)
			}
		})
	}
}

func TestReadREADME_TruncatesRegularReadme(t *testing.T) {
	root := t.TempDir()
	fsutiltest.WriteFile(t, root, "README.md", strings.Repeat("a", 9000))
	got := readREADME(root)
	if !strings.HasSuffix(got, "...(truncated)") || len(got) > 8100 {
		t.Fatalf("unexpected README result: len=%d", len(got))
	}
}
