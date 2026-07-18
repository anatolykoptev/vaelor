package scip_test

import (
	"os"
	"testing"

	gocodescip "github.com/anatolykoptev/vaelor/internal/scip"
)

func TestReadIndex_EmptyFile(t *testing.T) {
	t.Parallel()
	f, err := os.CreateTemp(t.TempDir(), "scip-*.scip")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	f.Close()

	idx, err := gocodescip.ReadIndex(f.Name())
	if err != nil {
		t.Fatalf("expected no error for empty file, got: %v", err)
	}
	if idx.DocumentCount() != 0 {
		t.Errorf("expected 0 documents, got %d", idx.DocumentCount())
	}
}

func TestReadIndex_NotFound(t *testing.T) {
	t.Parallel()
	_, err := gocodescip.ReadIndex("/nonexistent/path/index.scip")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}
