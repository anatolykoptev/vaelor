package embeddings

// B2 (#641) regression tests: IndexFile must serialize against a concurrent
// bulk reindex (IndexRepoAsyncWithTool / indexRepoWithTool).
//
// IndexFile does DeleteSymbolsForFile + embedAndUpsert on the same rows the
// bulk path touches via DeleteExplicitOrphans + embedChunks. Without the guard
// the two interleave → a just-inserted watcher symbol can be swept by the bulk
// orphan-delete, double-embedded, or deadlock PG. The fix: when a bulk reindex
// is in flight (IsIndexing(repoKey)==true) IndexFile defers — the bulk pass
// walks every source file and re-embeds changed symbols, so the file's update
// is NOT dropped (it is covered by the authoritative full-repo pass). When no
// bulk is running (the common case) IndexFile runs unchanged.
//
// Falsification: revert the IsIndexing guard in IndexFile →
//   - TestIndexFile_DefersWhenBulkIndexing: IndexFile proceeds with a nil store
//     → nil-pointer panic (RED). The guard is the only thing standing between
//     the call and p.store.DeleteSymbolsForFile.
//   - TestIndexFile_DefersToBulkReindex_NoDrop: IndexFile runs concurrently
//     with the bulk → result.DeferredToBulk stays false → assertion REDS.

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anatolykoptev/go-kit/embed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIndexFile_DefersWhenBulkIndexing is the load-bearing no-DB unit guard.
//
// It constructs a Pipeline with a NIL store/client and manually marks a repoKey
// as bulk-indexing via the same progress map IsIndexing reads. IndexFile MUST
// observe IsIndexing==true and return DeferredToBulk=true BEFORE touching the
// store — otherwise the nil store panics.
//
// Falsifiable: remove the IsIndexing guard → IndexFile falls through to
// p.store.DeleteSymbolsForFile (test-file evict / oversized evict / parse) on a
// nil store → nil-pointer dereference → test REDS (panic). The guard is the
// only code path that returns before the first store call.
func TestIndexFile_DefersWhenBulkIndexing(t *testing.T) {
	// nil store + nil client: any store access panics. The guard must return
	// before the first p.store.* call.
	p := NewPipeline(nil, nil, "")

	const repoKey = "test/indexfile-defer-nodb"
	prog := &indexProgress{}
	prog.running.Store(true)
	p.progress.Store(repoKey, prog)
	t.Cleanup(func() { p.progress.Delete(repoKey) })

	// A normal source file that WOULD trigger parse+embed if the guard were
	// absent. Use a temp dir so the file exists on disk (the guard runs before
	// the os.Stat checks, so existence is not strictly required, but a real
	// file makes the no-guard path reach parseAndDiff → store).
	dir := t.TempDir()
	relPath := "foo.go"
	writeTestFile(t, dir, relPath, "package main\n\nfunc Foo() {}\n")

	res, err := p.IndexFile(context.Background(), repoKey, dir, relPath)
	require.NoError(t, err, "deferral must not surface an error")
	require.NotNil(t, res, "deferral must return a non-nil result")

	assert.True(t, res.DeferredToBulk,
		"IndexFile must defer (DeferredToBulk=true) when a bulk reindex holds the slot; "+
			"revert the IsIndexing guard and this panics on the nil store (RED)")
	assert.Equal(t, 0, res.Embedded, "deferred IndexFile must not embed")
	assert.Equal(t, 0, res.Skipped, "deferred IndexFile must not skip")
	assert.EqualValues(t, 0, res.Deleted, "deferred IndexFile must not delete")
}

// TestIndexFile_RunsWhenNoBulkIndexing confirms the common case is preserved:
// with no bulk reindex in flight IndexFile does NOT defer (DeferredToBulk=false)
// and performs the real per-file index. Guards against an over-broad guard that
// would always defer. DB-gated.
func TestIndexFile_RunsWhenNoBulkIndexing(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()
	const repo = "test/indexfile-nodefer"
	cleanRepo(t, store, repo)

	// No bulk index in flight: IsIndexing(repo)==false.
	require.False(t, p.IsIndexing(repo), "precondition: no bulk index running")

	dir := t.TempDir()
	root, relPath := writeTempGoFile(t, dir, "solo.go", []string{"Solo"})

	res, err := p.IndexFile(ctx, repo, root, relPath)
	require.NoError(t, err)
	require.NotNil(t, res)

	assert.False(t, res.DeferredToBulk,
		"IndexFile must NOT defer when no bulk reindex is in flight (common case preserved)")
	assert.Equal(t, 1, res.Embedded, "precondition: the per-file index ran and embedded Solo")
}

// TestIndexFile_DefersToBulkReindex_NoDrop is the DB-gated integration guard.
//
// A bulk IndexRepo holds the per-repoKey slot (blocked in writeRepoState after
// its embed committed). While the slot is held, IndexFile is called for a file
// in the SAME repo root. IndexFile MUST defer (DeferredToBulk=true) — it must
// not interleave DELETE+INSERT with the bulk pass. The bulk pass walks every
// source file in the root, so the file's update is covered (no drop): after the
// bulk completes the file's symbols are present in the DB.
//
// Falsifiable: remove the IsIndexing guard → IndexFile runs concurrently with
// the bulk → result.DeferredToBulk stays false → the assertion REDS (and the
// concurrent DELETE+INSERT is the race this guard prevents).
func TestIndexFile_DefersToBulkReindex_NoDrop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const repo = "test/indexfile-defer-bulk-nodrop"
	store := testStore(t)
	cleanRepoFull(t, store, repo)

	srv := fakeEmbedServer(t)
	defer srv.Close()
	client, err := embed.NewClient(srv.URL,
		embed.WithBackend("http"),
		embed.WithDim(dimSize),
	)
	require.NoError(t, err)

	// Bulk blocks in writeRepoState (after embedChunks committed) until the
	// IndexFile call has returned, forcing IndexFile to observe the slot held.
	bulkStateEntered := make(chan struct{})
	bulkRelease := make(chan struct{})
	indexFileDone := make(chan struct{})
	var stateCalls int64
	writeFn := func(c context.Context, repoKey, sha, sourcePath string) error {
		if atomic.AddInt64(&stateCalls, 1) == 1 {
			close(bulkStateEntered)
			select {
			case <-bulkRelease:
			case <-c.Done():
				return c.Err()
			}
			return store.SetRepoStateWithPath(c, repoKey, sha, "", sourcePath)
		}
		return store.SetRepoStateWithPath(c, repoKey, sha, "", sourcePath)
	}
	p := NewPipeline(client, store, "", WithFileCache(nil),
		withWriteRepoStateFn(writeFn),
	)

	// One git repo root containing the file the watcher would re-index. The
	// bulk walks this root, so it covers foo.go (no-drop guarantee).
	root := initGitRepo(t, map[string]string{
		"foo.go": goFile("BulkCovA", "BulkCovB"),
	})

	// Start the bulk; it claims the slot and blocks in writeRepoState after
	// embedding foo.go's symbols.
	var bulkWg sync.WaitGroup
	var bulkErr error
	bulkWg.Add(1)
	go func() {
		defer bulkWg.Done()
		_, bulkErr = p.IndexRepo(ctx, repo, root)
	}()
	<-bulkStateEntered // bulk embeddings committed; slot held.

	// Mutate foo.go AFTER the bulk embedded it — this is the watcher event the
	// per-file path would race to index. The bulk already passed foo.go, so
	// only a subsequent bulk cycle OR an IndexFile would pick this up. We assert
	// IndexFile defers (does not race); the next bulk cycle covers the change.
	writeTestFile(t, root, "foo.go", goFile("BulkCovA", "BulkCovB", "WatcherNew"))

	// IndexFile while the bulk holds the slot.
	var ifRes *FileIndexResult
	var ifErr error
	go func() {
		defer close(indexFileDone)
		ifRes, ifErr = p.IndexFile(ctx, repo, root, "foo.go")
	}()
	select {
	case <-indexFileDone:
	case <-time.After(5 * time.Second):
	}

	// Release the bulk so it writes its state row and completes.
	close(bulkRelease)
	bulkWg.Wait()
	require.NoError(t, bulkErr, "bulk index must complete cleanly")

	// THE LOAD-BEARING GUARD: IndexFile deferred.
	require.NoError(t, ifErr, "deferral must not surface an error")
	require.NotNil(t, ifRes)
	assert.True(t, ifRes.DeferredToBulk,
		"IndexFile must defer to the in-flight bulk reindex (revert the IsIndexing guard → "+
			"IndexFile runs concurrently and DeferredToBulk stays false → RED)")

	// No-drop proof: the bulk pass covered foo.go — its symbols are in the DB.
	rows, gErr := store.GetSymbolsForFile(ctx, repo, "foo.go")
	require.NoError(t, gErr)
	assert.NotEmpty(t, rows,
		"the bulk pass must cover foo.go — deferring IndexFile must not drop the file's update")
}
