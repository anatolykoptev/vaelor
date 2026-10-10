package embeddings

import (
	"context"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/fsutil/fsutiltest"
)

// TestParseAndDiff_RefusesSymlinkedSource: a path from a git diff or watcher
// event that is a link must be skipped as a read error before any parsing or
// store access (the zero Pipeline has no store, so getting past the read
// guard panics, which the test reports as a failure).
func TestParseAndDiff_RefusesSymlinkedSource(t *testing.T) {
	outside := fsutiltest.WriteOutside(t, "package x\n\nfunc F() {}\n")
	root := t.TempDir()
	fsutiltest.Symlink(t, root, "leak.go", outside)
	fsutiltest.Symlink(t, root, "zero.go", fsutiltest.EndlessDevice)
	for _, rel := range []string{"leak.go", "zero.go"} {
		fsutiltest.Within(t, func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: read guard bypassed, parse reached the store: %v", rel, r)
				}
			}()
			p := &Pipeline{}
			toEmbed, names, res, err := p.parseAndDiff(context.Background(), "k", root, rel)
			if err != nil || toEmbed != nil || names != nil || res == nil {
				t.Errorf("%s: want a clean permanent skip, got %v %v %v %v", rel, toEmbed, names, res, err)
			}
		})
	}
}
