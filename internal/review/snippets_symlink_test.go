package review

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/fsutil/fsutiltest"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

func TestExtractSnippets_RefusesSymlinkedSource(t *testing.T) {
	outside := fsutiltest.WriteOutside(t, "line1\nSecret "+fsutiltest.Marker+"\nline3\n")
	root := t.TempDir()
	fsutiltest.Symlink(t, root, "leak.go", outside)
	fsutiltest.Symlink(t, root, "zero.go", fsutiltest.EndlessDevice)
	fsutiltest.WriteFile(t, root, "real.go", "a\nb\nc\n")

	mk := func(path string) ChangedSymbol {
		return ChangedSymbol{
			Symbol:   &parser.Symbol{Name: "F", StartLine: 1, EndLine: 3},
			FileDiff: FileDiff{Path: path},
		}
	}
	var got []Snippet
	fsutiltest.Within(t, func() {
		got = ExtractSnippets([]ChangedSymbol{mk("leak.go"), mk("zero.go"), mk("real.go")}, root)
	})
	for _, s := range got {
		if strings.Contains(s.Code, fsutiltest.Marker) || s.File != "real.go" {
			t.Fatalf("snippet from a symlinked file: %+v", s)
		}
	}
	if len(got) != 1 {
		t.Fatalf("want exactly the regular file's snippet, got %d", len(got))
	}
}
