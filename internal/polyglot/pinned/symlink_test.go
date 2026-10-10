package pinned

import (
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/fsutil/fsutiltest"
)

const composeWithMarker = "services:\n  web:\n    image: " + fsutiltest.Marker + ":1\n"

func collectWithin(t *testing.T, root string) []PinnedImage {
	t.Helper()
	var got []PinnedImage
	fsutiltest.Within(t, func() {
		var err error
		if got, err = Collect(root); err != nil {
			t.Errorf("Collect: %v", err)
		}
	})
	return got
}

func assertNoMarker(t *testing.T, got []PinnedImage) {
	t.Helper()
	if len(got) != 0 || strings.Contains(fmt.Sprintf("%+v", got), fsutiltest.Marker) {
		t.Fatalf("content from a refused file reached the result: %+v", got)
	}
}

func TestCollect_RefusesSymlinkedComposeAndDockerfile(t *testing.T) {
	outsideCompose := fsutiltest.WriteOutside(t, composeWithMarker)
	outsideDocker := fsutiltest.WriteOutside(t, "FROM "+fsutiltest.Marker+":1\n")
	root := t.TempDir()
	fsutiltest.Symlink(t, root, "a/docker-compose.yml", outsideCompose)
	fsutiltest.Symlink(t, root, "b/docker-compose.yml", fsutiltest.EndlessDevice)
	fsutiltest.Symlink(t, root, "c/Dockerfile", outsideDocker)
	fsutiltest.Symlink(t, root, "d/Dockerfile", fsutiltest.EndlessDevice)
	assertNoMarker(t, collectWithin(t, root))
}

func TestCollect_FifoDockerfileDoesNotHang(t *testing.T) {
	root := t.TempDir()
	fsutiltest.WriteFile(t, root, "x/.keep", "")
	if err := syscall.Mkfifo(filepath.Join(root, "x", "Dockerfile"), 0o600); err != nil {
		t.Fatal(err)
	}
	assertNoMarker(t, collectWithin(t, root))
}

func TestCollect_ComposeIncludesStayInsideRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "repo")
	// Escaping, absolute (even when it points inside the repo) and in-root
	// relative includes; only the last may contribute.
	fsutiltest.WriteFile(t, parent, "outside.yml", composeWithMarker)
	fsutiltest.WriteFile(t, root, "inside/abs.yml", "services:\n  abs:\n    image: abs-image:1\n")
	fsutiltest.WriteFile(t, root, "inside/rel.yml", "services:\n  rel:\n    image: rel-image:1\n")
	fsutiltest.WriteFile(t, root, "docker-compose.yml", fmt.Sprintf(
		"include:\n  - ../outside.yml\n  - %s\n  - inside/rel.yml\n", filepath.Join(root, "inside", "abs.yml")))

	got := collectWithin(t, root)
	var images []string
	for _, g := range got {
		images = append(images, g.Service)
	}
	if len(got) != 1 || got[0].Service != "rel" {
		t.Fatalf("want only the in-root relative include, got services %v (%+v)", images, got)
	}
}
