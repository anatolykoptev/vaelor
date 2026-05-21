package pinned

import (
	"path/filepath"
	"sort"
	"testing"
)

func TestCollect_SampleRepo(t *testing.T) {
	root := filepath.Join("testdata", "sample-repo")
	got, err := Collect(root)
	if err != nil {
		t.Fatalf("Collect(%s) error: %v", root, err)
	}

	// Expect: 2 from Dockerfile (multi-stage), 1 from api.Dockerfile,
	//         2 from docker-compose.yml.
	const wantTotal = 5
	if len(got) != wantTotal {
		t.Fatalf("len=%d want %d\ngot=%#v", len(got), wantTotal, got)
	}

	// Group by Source to assert per-file counts.
	bySource := map[string]int{}
	for _, p := range got {
		bySource[p.Source]++
	}
	for src, n := range bySource {
		t.Logf("source=%s count=%d", src, n)
	}
	if bySource[filepath.Join(root, "Dockerfile")] != 2 {
		t.Errorf("Dockerfile count=%d want 2", bySource[filepath.Join(root, "Dockerfile")])
	}
	if bySource[filepath.Join(root, "api.Dockerfile")] != 1 {
		t.Errorf("api.Dockerfile count=%d want 1", bySource[filepath.Join(root, "api.Dockerfile")])
	}
	if bySource[filepath.Join(root, "docker-compose.yml")] != 2 {
		t.Errorf("docker-compose.yml count=%d want 2", bySource[filepath.Join(root, "docker-compose.yml")])
	}

	// Sanity: all images have non-empty Image OR non-empty Unresolved.
	for i, p := range got {
		if p.Image == "" && p.Unresolved == "" {
			t.Errorf("[%d] %#v: both Image and Unresolved empty", i, p)
		}
	}

	// Sanity: deterministic ordering for stable output.
	sortedByKey := append([]PinnedImage(nil), got...)
	sort.SliceStable(sortedByKey, func(i, j int) bool {
		if sortedByKey[i].Source != sortedByKey[j].Source {
			return sortedByKey[i].Source < sortedByKey[j].Source
		}
		return sortedByKey[i].Line < sortedByKey[j].Line
	})
	for i := range got {
		if got[i] != sortedByKey[i] {
			t.Errorf("Collect not stably sorted by (Source,Line) at idx %d:\n got=%#v\nwant=%#v", i, got[i], sortedByKey[i])
			break
		}
	}
}

func TestCollect_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	got, err := Collect(dir)
	if err != nil {
		t.Fatalf("Collect(empty) error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty, got %#v", got)
	}
}
