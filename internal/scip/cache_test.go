package scip_test

import (
	"os"
	"path/filepath"
	"testing"

	gocodescip "github.com/anatolykoptev/go-code/internal/scip"
)

func TestCacheKey_StableAndNonEmpty(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	key1 := gocodescip.CacheKey(dir)
	key2 := gocodescip.CacheKey(dir)

	if key1 == "" {
		t.Fatal("CacheKey returned empty string")
	}
	if key1 != key2 {
		t.Errorf("CacheKey not stable: got %q then %q", key1, key2)
	}
}

func TestCacheLookup_Miss(t *testing.T) {
	cacheDir := t.TempDir()
	c := gocodescip.NewCache(cacheDir)

	_, ok := c.Get("nonexistent-key-xyz")
	if ok {
		t.Fatal("expected cache miss, got hit")
	}
}

func TestCachePutGet(t *testing.T) {
	cacheDir := t.TempDir()
	c := gocodescip.NewCache(cacheDir)

	// Create a fake index.scip file.
	srcDir := t.TempDir()
	indexPath := filepath.Join(srcDir, "index.scip")
	wantContent := []byte("fake scip index content")
	if err := os.WriteFile(indexPath, wantContent, 0o644); err != nil {
		t.Fatalf("write index.scip: %v", err)
	}

	key := "test-key-abc123"

	if err := c.Put(key, indexPath); err != nil {
		t.Fatalf("Put: %v", err)
	}

	gotPath, ok := c.Get(key)
	if !ok {
		t.Fatal("expected cache hit after Put, got miss")
	}

	gotContent, err := os.ReadFile(gotPath)
	if err != nil {
		t.Fatalf("read cached file: %v", err)
	}
	if string(gotContent) != string(wantContent) {
		t.Errorf("cached content mismatch: got %q, want %q", gotContent, wantContent)
	}
}
