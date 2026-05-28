package embeddings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-kit/embed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -- Store-level helpers (no embed client needed) --

// testStore creates a Store backed by a real Postgres pool (skips if DATABASE_URL unset).
func testStore(t *testing.T) *Store {
	t.Helper()
	pool := testPool(t)
	s := NewStore(pool)
	ctx := context.Background()
	require.NoError(t, s.EnsureSchema(ctx))
	return s
}

// cleanRepo removes all embeddings for a given repo key at test start and end.
func cleanRepo(t *testing.T, s *Store, repoKey string) {
	t.Helper()
	ctx := context.Background()
	_ = s.DeleteRepo(ctx, repoKey)
	t.Cleanup(func() { _ = s.DeleteRepo(ctx, repoKey) })
}

// insertSymbols inserts EmbeddingRecord rows with fake zero vectors for Store tests.
func insertSymbols(t *testing.T, s *Store, repoKey, filePath string, names []string) {
	t.Helper()
	ctx := context.Background()
	records := make([]EmbeddingRecord, len(names))
	for i, name := range names {
		records[i] = EmbeddingRecord{
			RepoKey:    repoKey,
			FilePath:   filePath,
			SymbolName: name,
			SymbolKind: "function",
			Language:   "go",
			StartLine:  i + 1,
			BodyHash:   uint64(i + 1),
			Embedding:  makeVec(),
		}
	}
	require.NoError(t, s.Upsert(ctx, records))
}

// -- Store-level tests --

// TestGetSymbolsForFile_SortedByName verifies rows come back sorted alphabetically
// regardless of insertion order. Non-deterministic ordering would break IndexFile diffs.
func TestGetSymbolsForFile_SortedByName(t *testing.T) {
	s := testStore(t)
	const repo = "test/get-symbols-sort"
	cleanRepo(t, s, repo)
	ctx := context.Background()

	insertSymbols(t, s, repo, "file.go", []string{"zoo", "alpha", "middle"})

	rows, err := s.GetSymbolsForFile(ctx, repo, "file.go")
	require.NoError(t, err)
	require.Len(t, rows, 3)

	names := []string{rows[0].SymbolName, rows[1].SymbolName, rows[2].SymbolName}
	assert.Equal(t, []string{"alpha", "middle", "zoo"}, names,
		"GetSymbolsForFile must return rows sorted by symbol_name")
}

// TestDeleteSymbolsForFile_EmptyKeepListDeletesAll verifies that a nil keep-list
// removes every symbol for the file.
func TestDeleteSymbolsForFile_EmptyKeepListDeletesAll(t *testing.T) {
	s := testStore(t)
	const repo = "test/delete-all"
	cleanRepo(t, s, repo)
	ctx := context.Background()

	insertSymbols(t, s, repo, "file.go", []string{"a", "b", "c"})

	n, err := s.DeleteSymbolsForFile(ctx, repo, "file.go", nil)
	require.NoError(t, err)
	assert.EqualValues(t, 3, n, "should have deleted 3 rows")

	rows, err := s.GetSymbolsForFile(ctx, repo, "file.go")
	require.NoError(t, err)
	assert.Empty(t, rows, "file should have no symbols after delete-all")
}

// TestDeleteSymbolsForFile_PreservesKeepList verifies that symbols in the keep-list
// survive while excluded ones are removed.
func TestDeleteSymbolsForFile_PreservesKeepList(t *testing.T) {
	s := testStore(t)
	const repo = "test/delete-keep"
	cleanRepo(t, s, repo)
	ctx := context.Background()

	insertSymbols(t, s, repo, "file.go", []string{"a", "b", "c"})

	n, err := s.DeleteSymbolsForFile(ctx, repo, "file.go", []string{"a", "c"})
	require.NoError(t, err)
	assert.EqualValues(t, 1, n, "should have deleted exactly 1 row (b)")

	rows, err := s.GetSymbolsForFile(ctx, repo, "file.go")
	require.NoError(t, err)
	require.Len(t, rows, 2)
	names := []string{rows[0].SymbolName, rows[1].SymbolName}
	sort.Strings(names)
	assert.Equal(t, []string{"a", "c"}, names, "a and c must survive the delete")
}

// TestDeleteSymbolsForFile_DoesNotAffectOtherFiles verifies cross-file isolation.
func TestDeleteSymbolsForFile_DoesNotAffectOtherFiles(t *testing.T) {
	s := testStore(t)
	const repo = "test/delete-cross-file"
	cleanRepo(t, s, repo)
	ctx := context.Background()

	insertSymbols(t, s, repo, "file1.go", []string{"f1a", "f1b"})
	insertSymbols(t, s, repo, "file2.go", []string{"f2a", "f2b"})

	_, err := s.DeleteSymbolsForFile(ctx, repo, "file1.go", nil)
	require.NoError(t, err)

	rows, err := s.GetSymbolsForFile(ctx, repo, "file2.go")
	require.NoError(t, err)
	assert.Len(t, rows, 2, "file2.go symbols must not be affected by delete on file1.go")
}

// TestDeleteSymbolsForFile_DoesNotAffectOtherRepos verifies cross-repo isolation.
func TestDeleteSymbolsForFile_DoesNotAffectOtherRepos(t *testing.T) {
	s := testStore(t)
	const (
		repoA = "test/delete-cross-repo-A"
		repoB = "test/delete-cross-repo-B"
	)
	cleanRepo(t, s, repoA)
	cleanRepo(t, s, repoB)
	ctx := context.Background()

	insertSymbols(t, s, repoA, "shared.go", []string{"x"})
	insertSymbols(t, s, repoB, "shared.go", []string{"x"})

	_, err := s.DeleteSymbolsForFile(ctx, repoA, "shared.go", nil)
	require.NoError(t, err)

	rows, err := s.GetSymbolsForFile(ctx, repoB, "shared.go")
	require.NoError(t, err)
	assert.Len(t, rows, 1, "repoB symbols must not be affected by delete on repoA")
}

// -- Pipeline.IndexFile helpers --

// fakeEmbedServer returns an httptest.Server responding to POST /v1/embeddings
// with 768-dim zero vectors (one per input text), using the OpenAI-compatible
// response shape that HTTPEmbedder expects. Allows IndexFile tests to run
// against a real Postgres without a live embed-server.
func fakeEmbedServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		type embedData struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}
		type embedResp struct {
			Data []embedData `json:"data"`
		}
		resp := embedResp{Data: make([]embedData, len(req.Input))}
		for i := range resp.Data {
			resp.Data[i] = embedData{Embedding: makeVec(), Index: i}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// testPipeline creates a Pipeline backed by a real Postgres store and a fake
// embed server for IndexFile integration tests.
func testPipeline(t *testing.T) (*Pipeline, *Store) {
	t.Helper()
	srv := fakeEmbedServer(t)
	client, err := embed.NewClient(srv.URL,
		embed.WithBackend("http"),
		embed.WithDim(dimSize),
	)
	require.NoError(t, err)
	pool := testPool(t)
	store := NewStore(pool)
	ctx := context.Background()
	require.NoError(t, store.EnsureSchema(ctx))
	p := NewPipeline(client, store, WithFileCache(nil))
	return p, store
}

// writeTempGoFile writes a minimal Go file with named no-body functions.
func writeTempGoFile(t *testing.T, dir, filename string, funcNames []string) (root, relPath string) {
	t.Helper()
	root = dir
	relPath = filename
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	for _, name := range funcNames {
		sb.WriteString("func " + name + "() {}\n")
	}
	writeTestFile(t, dir, filename, sb.String())
	return root, relPath
}

// writeTempGoFileWithBodies writes functions with distinct bodies so hash changes
// are detectable when the body is mutated between index calls.
func writeTempGoFileWithBodies(t *testing.T, dir, filename string, funcs map[string]string) {
	t.Helper()
	// Iterate in deterministic order so test output is stable.
	keys := make([]string, 0, len(funcs))
	for k := range funcs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	for _, name := range keys {
		sb.WriteString("func " + name + "() " + funcs[name] + "\n")
	}
	writeTestFile(t, dir, filename, sb.String())
}

// -- Pipeline.IndexFile integration tests --

// TestIndexFile_FirstTimeIndex verifies that indexing a new file embeds all
// symbols, skips none, and the DB reflects the expected symbol set.
func TestIndexFile_FirstTimeIndex(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()
	const repo = "test/indexfile-first"
	cleanRepo(t, store, repo)

	dir := t.TempDir()
	root, relPath := writeTempGoFile(t, dir, "foo.go", []string{"Alpha", "Beta", "Gamma"})

	result, err := p.IndexFile(ctx, repo, root, relPath)
	require.NoError(t, err)

	assert.Equal(t, 3, result.Embedded, "first index must embed all 3 symbols")
	assert.Equal(t, 0, result.Skipped, "first index: nothing skipped")
	assert.EqualValues(t, 0, result.Deleted, "first index: nothing deleted")

	rows, err := store.GetSymbolsForFile(ctx, repo, relPath)
	require.NoError(t, err)
	require.Len(t, rows, 3, "DB must have exactly 3 symbols after first index")

	names := make([]string, len(rows))
	for i, r := range rows {
		names[i] = r.SymbolName
	}
	sort.Strings(names)
	assert.Equal(t, []string{"Alpha", "Beta", "Gamma"}, names)
}

// TestIndexFile_ReindexUnchanged is the core regression guard for incremental
// indexing: a second call on the same file content MUST embed 0 symbols.
//
// Failure mode without fix: IndexFile without body_hash check re-embeds
// everything, producing Embedded==2 instead of 0.
func TestIndexFile_ReindexUnchanged(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()
	const repo = "test/indexfile-noop"
	cleanRepo(t, store, repo)

	dir := t.TempDir()
	root, relPath := writeTempGoFile(t, dir, "bar.go", []string{"Foo", "Baz"})

	first, err := p.IndexFile(ctx, repo, root, relPath)
	require.NoError(t, err)
	require.Equal(t, 2, first.Embedded, "precondition: first call must embed 2")

	// Second call — identical file on disk.
	second, err := p.IndexFile(ctx, repo, root, relPath)
	require.NoError(t, err)

	// THE CORE GUARD: Embedded must be 0 because hashes match.
	// A broken implementation (no hash-skip) would return Embedded==2 here.
	assert.Equal(t, 0, second.Embedded, "re-index of unchanged file must embed 0 (hash-skip)")
	assert.Equal(t, 2, second.Skipped, "re-index of unchanged file must skip all symbols")
	assert.EqualValues(t, 0, second.Deleted, "re-index of unchanged file must delete 0")
}

// TestIndexFile_BodyChanged verifies that only the symbol whose body hash
// changed is re-embedded while unchanged symbols are skipped.
func TestIndexFile_BodyChanged(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()
	const repo = "test/indexfile-changed"
	cleanRepo(t, store, repo)

	dir := t.TempDir()
	relPath := "changed.go"

	// First pass.
	writeTempGoFileWithBodies(t, dir, relPath, map[string]string{
		"Mutable": "{ return }",
		"Stable1": "{ _ = 1 }",
		"Stable2": "{ _ = 2 }",
	})
	first, err := p.IndexFile(ctx, repo, dir, relPath)
	require.NoError(t, err)
	require.Equal(t, 3, first.Embedded, "precondition: first pass embeds 3")

	// Second pass: only Mutable body changed.
	writeTempGoFileWithBodies(t, dir, relPath, map[string]string{
		"Mutable": "{ _ = 999 }",
		"Stable1": "{ _ = 1 }",
		"Stable2": "{ _ = 2 }",
	})
	second, err := p.IndexFile(ctx, repo, dir, relPath)
	require.NoError(t, err)

	assert.Equal(t, 1, second.Embedded, "only Mutable (hash changed) should be re-embedded")
	assert.Equal(t, 2, second.Skipped, "Stable1+Stable2 must be skipped (hash unchanged)")
	assert.EqualValues(t, 0, second.Deleted, "no symbol was removed from the file")
}

// TestIndexFile_SymbolRemoved verifies that symbols no longer in the new parse
// are deleted from the DB.
func TestIndexFile_SymbolRemoved(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()
	const repo = "test/indexfile-removed"
	cleanRepo(t, store, repo)

	dir := t.TempDir()
	relPath := "rm.go"

	// First pass: three functions.
	writeTempGoFile(t, dir, relPath, []string{"Foo", "Bar", "Baz"})
	first, err := p.IndexFile(ctx, repo, dir, relPath)
	require.NoError(t, err)
	require.Equal(t, 3, first.Embedded, "precondition: first pass embeds 3")

	// Second pass: Baz removed.
	writeTempGoFile(t, dir, relPath, []string{"Foo", "Bar"})
	second, err := p.IndexFile(ctx, repo, dir, relPath)
	require.NoError(t, err)

	assert.EqualValues(t, 1, second.Deleted, "Baz must be deleted from DB")
	assert.Equal(t, 2, second.Skipped, "Foo+Bar unchanged: must be skipped")
	assert.Equal(t, 0, second.Embedded, "no new/changed symbols")

	rows, err := store.GetSymbolsForFile(ctx, repo, relPath)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	names := []string{rows[0].SymbolName, rows[1].SymbolName}
	sort.Strings(names)
	assert.Equal(t, []string{"Bar", "Foo"}, names)
}

// TestIndexFile_FileDeleted verifies that when the file no longer exists on disk
// all previously indexed symbols are evicted.
func TestIndexFile_FileDeleted(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()
	const repo = "test/indexfile-deleted"
	cleanRepo(t, store, repo)

	dir := t.TempDir()
	relPath := "gone.go"

	// First pass: index the file.
	writeTempGoFile(t, dir, relPath, []string{"One", "Two", "Three"})
	first, err := p.IndexFile(ctx, repo, dir, relPath)
	require.NoError(t, err)
	require.Equal(t, 3, first.Embedded, "precondition: first pass embeds 3")

	// Delete the file from disk.
	require.NoError(t, os.Remove(filepath.Join(dir, relPath)))

	// Second pass: file gone.
	second, err := p.IndexFile(ctx, repo, dir, relPath)
	require.NoError(t, err)

	assert.Greater(t, int(second.Deleted), 0, "file deletion must evict indexed symbols")
	assert.Equal(t, 0, second.Embedded)
	assert.Equal(t, 0, second.Skipped)

	rows, err := store.GetSymbolsForFile(ctx, repo, relPath)
	require.NoError(t, err)
	assert.Empty(t, rows, "no symbols should remain after file deletion")
}
