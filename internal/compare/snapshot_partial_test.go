package compare

import (
	"testing"

	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// TestBuildSnapshotResult_PartialOnReadError proves that a file enumerated by
// ingest but unreadable at parse time (the use-after-delete signal on a shared
// clone dir) is DROPPED, COUNTED, and surfaces as Partial=true rather than
// silently inflating the file list with a hollow 0-line entry.
func TestBuildSnapshotResult_PartialOnReadError(t *testing.T) {
	ir := &ingest.IngestResult{
		Files: []*ingest.File{
			{Path: "/x/a.go", RelPath: "a.go", Language: "go"},
			{Path: "/x/b.go", RelPath: "b.go", Language: "go"},
			{Path: "/x/c_test.go", RelPath: "c_test.go", Language: "go"},
		},
	}
	// a.go parsed fine; b.go and c_test.go vanished (read error).
	parsed := []snapshotParseResult{
		{file: ir.Files[0], result: &parser.ParseResult{}, lines: 10},
		{file: ir.Files[1], readErr: true},
		{file: ir.Files[2], readErr: true},
	}

	snap := buildSnapshotResult("/x", ir, parsed)

	if !snap.Partial {
		t.Fatal("Partial = false, want true (2 of 3 files failed to read)")
	}
	if snap.DroppedReadError != 2 {
		t.Errorf("DroppedReadError = %d, want 2", snap.DroppedReadError)
	}
	if snap.DroppedCtxCancel != 0 {
		t.Errorf("DroppedCtxCancel = %d, want 0", snap.DroppedCtxCancel)
	}
	// Vanished files must NOT appear as hollow entries that corrupt downstream
	// metrics (test-file detection, line totals).
	if len(snap.Files) != 1 {
		t.Errorf("len(Files) = %d, want 1 (only a.go survived)", len(snap.Files))
	}
	// FileCount keeps the ingest-enumerated total so the gap (FileCount vs
	// len(Files)) is observable to the caller.
	if snap.FileCount != 3 {
		t.Errorf("FileCount = %d, want 3 (ingest enumerated 3)", snap.FileCount)
	}
}

// TestBuildSnapshotResult_PartialOnCtxCancel proves a parse worker that bailed
// on ctx.Err() (leaving a zero-value result with a nil file) is counted as a
// ctx-cancel drop and marks the snapshot partial.
func TestBuildSnapshotResult_PartialOnCtxCancel(t *testing.T) {
	ir := &ingest.IngestResult{
		Files: []*ingest.File{
			{Path: "/x/a.go", RelPath: "a.go", Language: "go"},
			{Path: "/x/b.go", RelPath: "b.go", Language: "go"},
		},
	}
	parsed := []snapshotParseResult{
		{file: ir.Files[0], result: &parser.ParseResult{}, lines: 5},
		{}, // worker returned early on ctx.Err(): zero value, nil file.
	}

	snap := buildSnapshotResult("/x", ir, parsed)

	if !snap.Partial {
		t.Fatal("Partial = false, want true (1 file skipped on ctx-cancel)")
	}
	if snap.DroppedCtxCancel != 1 {
		t.Errorf("DroppedCtxCancel = %d, want 1", snap.DroppedCtxCancel)
	}
	if snap.DroppedReadError != 0 {
		t.Errorf("DroppedReadError = %d, want 0", snap.DroppedReadError)
	}
}

// TestBuildSnapshotResult_CompleteNotPartial guards against a false-positive
// partial flag: a fully-read snapshot must report Partial=false.
func TestBuildSnapshotResult_CompleteNotPartial(t *testing.T) {
	ir := &ingest.IngestResult{
		Files: []*ingest.File{
			{Path: "/x/a.go", RelPath: "a.go", Language: "go"},
		},
	}
	parsed := []snapshotParseResult{
		{file: ir.Files[0], result: &parser.ParseResult{}, lines: 7},
	}

	snap := buildSnapshotResult("/x", ir, parsed)

	if snap.Partial {
		t.Error("Partial = true, want false (all files read)")
	}
	if snap.DroppedReadError != 0 || snap.DroppedCtxCancel != 0 {
		t.Errorf("dropped = (%d,%d), want (0,0)", snap.DroppedReadError, snap.DroppedCtxCancel)
	}
}
