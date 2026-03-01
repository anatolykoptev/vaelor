package codegraph

import (
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/ingest"
)

func TestComputeChangedFiles(t *testing.T) {
	t.Parallel()

	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	stored := map[string]time.Time{
		"a.go": t1,
		"b.go": t1,
		"c.go": t1, // will be removed
	}

	current := []*ingest.File{
		{RelPath: "a.go", ModTime: t1}, // unchanged
		{RelPath: "b.go", ModTime: t2}, // changed
		{RelPath: "d.go", ModTime: t1}, // new
	}

	changed, removed := computeChangedFiles(current, stored)

	if len(changed) != 2 {
		t.Errorf("changed = %d, want 2 (b.go modified, d.go new)", len(changed))
	}
	if len(removed) != 1 || removed[0] != "c.go" {
		t.Errorf("removed = %v, want [c.go]", removed)
	}
}

func TestComputeChangedFilesAllNew(t *testing.T) {
	t.Parallel()
	current := []*ingest.File{
		{RelPath: "a.go", ModTime: time.Now()},
	}
	changed, removed := computeChangedFiles(current, nil)
	if len(changed) != 1 {
		t.Errorf("changed = %d, want 1", len(changed))
	}
	if len(removed) != 0 {
		t.Errorf("removed = %d, want 0", len(removed))
	}
}

func TestComputeChangedFilesNoChange(t *testing.T) {
	t.Parallel()
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	stored := map[string]time.Time{"a.go": t1}
	current := []*ingest.File{{RelPath: "a.go", ModTime: t1}}
	changed, removed := computeChangedFiles(current, stored)
	if len(changed) != 0 {
		t.Errorf("changed = %d, want 0", len(changed))
	}
	if len(removed) != 0 {
		t.Errorf("removed = %d, want 0", len(removed))
	}
}
