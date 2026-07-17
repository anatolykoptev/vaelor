package ingest

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	kitcache "github.com/anatolykoptev/go-kit/cache"
)

// fakeL2 is an in-memory implementation of kitcache.L2 for tests.
type fakeL2 struct {
	mu   sync.Mutex
	data map[string][]byte
	gets int
	sets int
	dels int
}

func newFakeL2() *fakeL2 {
	return &fakeL2{data: make(map[string][]byte)}
}

func (f *fakeL2) Get(ctx context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	if v, ok := f.data[key]; ok {
		return bytes.Clone(v), nil
	}
	return nil, kitcache.ErrCacheMiss
}

func (f *fakeL2) Set(ctx context.Context, key string, data []byte, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sets++
	f.data[key] = bytes.Clone(data)
	return nil
}

func (f *fakeL2) Del(ctx context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dels++
	delete(f.data, key)
	return nil
}

func (f *fakeL2) Close() error { return nil }

func installFakeIngestL2(t *testing.T) *fakeL2 {
	t.Helper()
	ResetCache()
	old := ingestCache.l2
	fake := newFakeL2()
	ingestCache.l2 = fake
	t.Cleanup(func() {
		ResetCache()
		ingestCache.l2 = old
	})
	return fake
}

// TestIngestCache_SetL2Empty leaves L2 nil when RedisURL is empty.
func TestIngestCache_SetL2Empty(t *testing.T) {
	ResetCache()
	ingestCache.l2 = &fakeL2{}
	SetL2("")
	if ingestCache.l2 != nil {
		t.Fatalf("SetL2(\"\") must leave L2 nil, got %T", ingestCache.l2)
	}
}

// TestIngestRepoCache_L1HitDoesNotTouchL2 verifies the second IngestRepo
// call is served from L1 and does not query the L2 store.
func TestIngestRepoCache_L1HitDoesNotTouchL2(t *testing.T) {
	fake := installFakeIngestL2(t)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	opts := IngestOpts{Root: dir}
	if _, err := IngestRepo(context.Background(), opts); err != nil {
		t.Fatalf("first IngestRepo: %v", err)
	}

	getsBefore := fake.gets
	r2, err := IngestRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("second IngestRepo: %v", err)
	}
	if fake.gets != getsBefore {
		t.Fatalf("L1 hit should not query L2: got %d extra L2.Get calls", fake.gets-getsBefore)
	}
	if len(r2.Files) != 1 {
		t.Fatalf("expected 1 file on L1 hit, got %d", len(r2.Files))
	}
}

// TestIngestRepoCache_L2HitRepopulatesL1 verifies that after L1 is cleared,
// an L2 hit deserializes the entry, repopulates L1, and returns the result.
func TestIngestRepoCache_L2HitRepopulatesL1(t *testing.T) {
	fake := installFakeIngestL2(t)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	opts := IngestOpts{Root: dir}
	r1, err := IngestRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("first IngestRepo: %v", err)
	}

	// Clear L1 but keep L2 populated.
	ingestCache.mu.Lock()
	ingestCache.entries = make(map[string]*ingestCacheEntry, ingestCacheMaxEntries)
	ingestCache.order = nil
	ingestCache.mu.Unlock()

	getsBefore := fake.gets
	r2, err := IngestRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("second IngestRepo (L2 hit): %v", err)
	}
	if fake.gets != getsBefore+1 {
		t.Fatalf("expected exactly one L2.Get on L1 miss, got %d", fake.gets-getsBefore)
	}
	if !ingestResultsEqual(r1, r2) {
		t.Fatalf("L2 hit result does not match original: %+v vs %+v", r1, r2)
	}

	// Third call should now be an L1 hit.
	getsBefore = fake.gets
	r3, err := IngestRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("third IngestRepo: %v", err)
	}
	if fake.gets != getsBefore {
		t.Fatalf("L1 repopulated; expected no L2.Get, got %d", fake.gets-getsBefore)
	}
	if !ingestResultsEqual(r1, r3) {
		t.Fatalf("L1 hit result does not match original: %+v vs %+v", r1, r3)
	}
}

// TestIngestRepoCache_RoundTrip verifies encode/decode of IngestResult.
func TestIngestRepoCache_RoundTrip(t *testing.T) {
	r1 := &IngestResult{
		Root:         "/repo",
		TotalBytes:   42,
		SkippedCount: 3,
		SkippedReasons: map[string]int{
			"oversize": 1,
			"language": 2,
		},
		Files: []*File{
			{
				Path:     "/repo/main.go",
				RelPath:  "main.go",
				Language: "go",
				Size:     7,
				ModTime:  time.Unix(1234567890, 0).UTC(),
			},
		},
	}

	e := &ingestCacheEntry{
		result:      r1,
		contentHash: "abc123",
		validatedAt: time.Unix(1234567891, 0).UTC(),
	}
	data, err := encodeIngestEntry(e)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := decodeIngestEntry(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.contentHash != e.contentHash {
		t.Errorf("contentHash mismatch: %q vs %q", decoded.contentHash, e.contentHash)
	}
	if !decoded.validatedAt.Equal(e.validatedAt) {
		t.Errorf("validatedAt mismatch: %v vs %v", decoded.validatedAt, e.validatedAt)
	}
	if !ingestResultsEqual(e.result, decoded.result) {
		t.Fatalf("round-trip result mismatch: %+v vs %+v", e.result, decoded.result)
	}
}

// TestIngestRepoCache_L2SkipsStaleEntry verifies that an L2 entry whose
// content hash no longer matches is rejected and removed from L2.
func TestIngestRepoCache_L2SkipsStaleEntry(t *testing.T) {
	fake := installFakeIngestL2(t)

	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(mainPath, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	opts := IngestOpts{Root: dir}
	r1, err := IngestRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("first IngestRepo: %v", err)
	}

	// Clear L1 and mutate the repo so the cached content hash is stale.
	ingestCache.mu.Lock()
	ingestCache.entries = make(map[string]*ingestCacheEntry, ingestCacheMaxEntries)
	ingestCache.order = nil
	ingestCache.mu.Unlock()

	if err := os.WriteFile(filepath.Join(dir, "util.go"), []byte("package main\nfunc util() {}\n"), 0o644); err != nil {
		t.Fatalf("write util.go: %v", err)
	}

	r2, err := IngestRepo(context.Background(), opts)
	if err != nil {
		t.Fatalf("second IngestRepo: %v", err)
	}
	if len(r2.Files) != 2 {
		t.Fatalf("expected fresh walk to find 2 files, got %d", len(r2.Files))
	}
	if fake.dels != 1 {
		t.Fatalf("expected stale L2 entry to be deleted, got dels=%d", fake.dels)
	}
	if len(r1.Files) != 1 {
		t.Fatalf("unexpected r1 file count: %d", len(r1.Files))
	}
}

func ingestResultsEqual(a, b *IngestResult) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Root != b.Root || a.TotalBytes != b.TotalBytes || a.SkippedCount != b.SkippedCount {
		return false
	}
	if len(a.SkippedReasons) != len(b.SkippedReasons) {
		return false
	}
	for k, v := range a.SkippedReasons {
		if b.SkippedReasons[k] != v {
			return false
		}
	}
	if len(a.Files) != len(b.Files) {
		return false
	}
	for i := range a.Files {
		af, bf := a.Files[i], b.Files[i]
		if af == nil || bf == nil {
			return af == bf
		}
		if af.Path != bf.Path || af.RelPath != bf.RelPath || af.Language != bf.Language || af.Size != bf.Size || !af.ModTime.Equal(bf.ModTime) {
			return false
		}
	}
	return true
}

// compile-time guard: fakeL2 implements kitcache.L2.
var _ kitcache.L2 = (*fakeL2)(nil)
