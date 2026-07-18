package scip_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	gocodescip "github.com/anatolykoptev/vaelor/internal/scip"
)

// TestCacheKey_StableAcrossMtimeChange verifies that changing file mtimes
// (e.g. via git checkout) does NOT change the cache key — the key is
// content-based, not mtime-based.
func TestCacheKey_StableAcrossMtimeChange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	fpath := filepath.Join(dir, "main.rs")
	content := []byte("fn main() { println!(\"hello\"); }\n")
	if err := os.WriteFile(fpath, content, 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	key1 := gocodescip.CacheKey(dir)

	// Change mtime to a different time (simulating git checkout)
	pastTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(fpath, pastTime, pastTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	key2 := gocodescip.CacheKey(dir)

	if key1 != key2 {
		t.Errorf("CacheKey changed across mtime change (content identical): %q vs %q", key1, key2)
	}
}

// TestCacheKey_ChangesOnContentChange verifies that changing file content
// DOES change the cache key.
func TestCacheKey_ChangesOnContentChange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	fpath := filepath.Join(dir, "main.rs")
	if err := os.WriteFile(fpath, []byte("fn main() {}\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	key1 := gocodescip.CacheKey(dir)

	if err := os.WriteFile(fpath, []byte("fn main() { println!(\"changed\"); }\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	key2 := gocodescip.CacheKey(dir)

	if key1 == key2 {
		t.Errorf("CacheKey did not change after content modification: %q == %q", key1, key2)
	}
}
