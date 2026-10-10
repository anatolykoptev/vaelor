package impact

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/fsutil/fsutiltest"
)

// The call snippet is echoed to the caller, so a linked source file must
// produce no snippet at all.
func TestAttachCallSnippets_RefusesSymlinkedSource(t *testing.T) {
	outside := fsutiltest.WriteOutside(t, "line1\nsecret "+fsutiltest.Marker+"\nline3\n")
	root := t.TempDir()
	fsutiltest.Symlink(t, root, "leak.go", outside)
	fsutiltest.Symlink(t, root, "zero.go", fsutiltest.EndlessDevice)
	fsutiltest.WriteFile(t, root, "real.go", "a\nb\nc\n")
	callers := []AffectedSymbol{
		{File: "leak.go", CallLine: 2},
		{File: "zero.go", CallLine: 2},
		{File: "real.go", CallLine: 2},
	}
	fsutiltest.Within(t, func() { AttachCallSnippets(callers, root) })
	for i, c := range callers[:2] {
		if c.Snippet != "" || strings.Contains(c.Snippet, fsutiltest.Marker) {
			t.Errorf("caller %d: snippet from a symlinked file: %q", i, c.Snippet)
		}
	}
	if callers[2].Snippet == "" {
		t.Fatal("regular file produced no snippet")
	}
}
