package embeddings

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	kitcache "github.com/anatolykoptev/go-kit/cache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureRepo writes a single Go source file with two functions and returns
// the repo root directory. Reusable across cache scenarios.
func fixtureRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := `package main

// Alpha is the first function.
func Alpha() int { return 1 }

// Beta is the second function.
func Beta() int { return 2 }
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644))
	return dir
}

// newCachedPipeline returns a Pipeline wired with an in-memory kitcache and no
// store/client (cache-layer tests don't exercise embedAndUpsert).
func newCachedPipeline(_ *testing.T) (*Pipeline, *kitcache.Cache) {
	c := kitcache.New(kitcache.Config{
		L1MaxItems: 64,
		L1TTL:      15 * time.Minute,
	})
	return NewPipeline(nil, nil, WithFileCache(c)), c
}

func TestCollectSymbolsCached_FirstCallMissThenHit(t *testing.T) {
	dir := fixtureRepo(t)
	p, c := newCachedPipeline(t)
	defer c.Close()

	syms1, files1, err := p.collectSymbolsCached(context.Background(), "repoA", dir)
	require.NoError(t, err)
	require.Len(t, syms1, 2, "first pass must produce 2 symbols")
	require.Len(t, files1, 2)

	statsAfter1 := c.Stats()
	require.Equal(t, int64(0), statsAfter1.L1Hits, "first pass = pure miss")
	require.Greater(t, statsAfter1.L1Size, 0, "first pass must populate at least one entry")

	syms2, files2, err := p.collectSymbolsCached(context.Background(), "repoA", dir)
	require.NoError(t, err)
	require.Len(t, syms2, 2)
	require.Len(t, files2, 2)

	statsAfter2 := c.Stats()
	require.Greater(t, statsAfter2.L1Hits, statsAfter1.L1Hits, "second pass must hit cache")

	// Symbol fields survive gob roundtrip.
	assert.Equal(t, syms1[0].Name, syms2[0].Name)
	assert.Equal(t, syms1[0].Kind, syms2[0].Kind)
	assert.Equal(t, syms1[0].Signature, syms2[0].Signature)
}

func TestCollectSymbolsCached_TouchModTimeInvalidates(t *testing.T) {
	dir := fixtureRepo(t)
	p, c := newCachedPipeline(t)
	defer c.Close()

	_, _, err := p.collectSymbolsCached(context.Background(), "repoA", dir)
	require.NoError(t, err)
	hitsBeforeTouch := c.Stats().L1Hits

	// Bump modtime forward without changing size — validator must reject.
	future := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(filepath.Join(dir, "main.go"), future, future))

	_, _, err = p.collectSymbolsCached(context.Background(), "repoA", dir)
	require.NoError(t, err)
	hitsAfterTouch := c.Stats().L1Hits

	assert.Equal(t, hitsBeforeTouch, hitsAfterTouch,
		"modtime change must NOT register a cache hit (validator evicts)")
}

func TestCollectSymbolsCached_TruncateInvalidates(t *testing.T) {
	dir := fixtureRepo(t)
	p, c := newCachedPipeline(t)
	defer c.Close()

	_, _, err := p.collectSymbolsCached(context.Background(), "repoA", dir)
	require.NoError(t, err)
	hitsBeforeTruncate := c.Stats().L1Hits

	// Truncate to a different size; modtime moves too but size alone is enough.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n"), 0o644))

	_, _, err = p.collectSymbolsCached(context.Background(), "repoA", dir)
	require.NoError(t, err)
	hitsAfterTruncate := c.Stats().L1Hits

	assert.Equal(t, hitsBeforeTruncate, hitsAfterTruncate,
		"size change must NOT register a cache hit")
}

func TestCollectSymbolsCached_NilCacheFallsBackToBaseline(t *testing.T) {
	dir := fixtureRepo(t)
	// Pipeline without WithFileCache → fileCache=nil → baseline path.
	p := NewPipeline(nil, nil)

	got, files, err := p.collectSymbolsCached(context.Background(), "repoA", dir)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Len(t, files, 2)

	want, wantFiles, err := collectSymbols(context.Background(), dir)
	require.NoError(t, err)
	require.Equal(t, len(want), len(got), "nil cache must match collectSymbols output length")

	// Order must match too — both paths walk ingest.IngestRepo identically.
	for i := range got {
		assert.Equal(t, want[i].Name, got[i].Name)
		assert.Equal(t, wantFiles[i].RelPath, files[i].RelPath)
	}
}

func TestCollectSymbolsCached_CrossRepoKeyIsolation(t *testing.T) {
	dir := fixtureRepo(t)
	p, c := newCachedPipeline(t)
	defer c.Close()

	_, _, err := p.collectSymbolsCached(context.Background(), "repoA", dir)
	require.NoError(t, err)
	statsA := c.Stats()
	require.Greater(t, statsA.L1Size, 0)

	// Same on-disk path, different repoKey → key namespace must isolate.
	_, _, err = p.collectSymbolsCached(context.Background(), "repoB", dir)
	require.NoError(t, err)
	statsAB := c.Stats()
	assert.Greater(t, statsAB.L1Size, statsA.L1Size,
		"repoB indexing must NOT collide with repoA's keys")
}

func TestPipelineCacheKey_StableAcrossInputs(t *testing.T) {
	k1 := pipelineCacheKey("repoA", "foo/bar.go")
	k2 := pipelineCacheKey("repoA", "foo/bar.go")
	k3 := pipelineCacheKey("repoB", "foo/bar.go")

	assert.Equal(t, k1, k2, "same inputs must produce same key")
	assert.NotEqual(t, k1, k3, "different repoKey must produce different key")
}
