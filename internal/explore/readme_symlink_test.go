package explore

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/fsutil/fsutiltest"
)

func TestReadmeExcerpt_RefusesSymlinkedReadme(t *testing.T) {
	outside := fsutiltest.WriteOutside(t, "Secret value is "+fsutiltest.Token+". More text follows.")
	for name, target := range map[string]string{
		"canary outside the repo": outside,
		"endless device":          fsutiltest.EndlessDevice,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			fsutiltest.Symlink(t, root, "README.md", target)
			var got string
			fsutiltest.Within(t, func() { got = readmeExcerpt(root) })
			if got != "" || strings.Contains(got, fsutiltest.Token) {
				t.Fatalf("symlinked README must yield no excerpt, got %q", got)
			}
		})
	}
}

func TestReadmeExcerpt_RegularReadmeStillWorks(t *testing.T) {
	root := t.TempDir()
	fsutiltest.WriteFile(t, root, "README.md", "# Title\n\nThis tool does a thing. It is useful.\n")
	if got := readmeExcerpt(root); !strings.Contains(got, "does a thing") {
		t.Fatalf("regular README not read: %q", got)
	}
}
