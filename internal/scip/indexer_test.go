package scip_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	gocodescip "github.com/anatolykoptev/go-code/internal/scip"
)

func TestRunIndexer_MissingBinary(t *testing.T) {
	cfg := gocodescip.IndexerConfig{
		Name: "nonexistent-scip-indexer-xyz",
		Args: nil,
	}
	_, err := gocodescip.RunIndexer(context.Background(), cfg, t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
}

func TestRunIndexer_ScipGo(t *testing.T) {
	if _, err := exec.LookPath("scip-go"); err != nil {
		t.Skip("scip-go not in PATH, skipping integration test")
	}

	// Create a minimal Go module in a temp directory.
	dir := t.TempDir()

	goMod := []byte("module example.com/testmod\n\ngo 1.21\n")
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), goMod, 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	mainGo := []byte("package main\n\nfunc main() {}\n")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), mainGo, 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	cfg := gocodescip.IndexerConfig{
		Name: "scip-go",
		Args: nil,
	}

	indexPath, err := gocodescip.RunIndexer(context.Background(), cfg, dir)
	if err != nil {
		t.Fatalf("RunIndexer: %v", err)
	}

	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		t.Errorf("index.scip not found at %s", indexPath)
	}
}
