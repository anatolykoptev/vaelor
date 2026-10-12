package embeddings

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DB-gated (skip without PR_TEST_DATABASE_URL; run in CI).

// A repo with no resolvable git SHA (a worktree whose gitdir is not mounted)
// never gets a code_repo_state row, yet keeps its embeddings across attempts.
// A failed re-embed of such a repo must not run the first-index compensating
// DeleteRepo: those rows were committed by earlier attempts, not this one.
//
// Falsifiable: dropping the `existingRows == 0` condition in firstIndexVerdict
// (pipeline.go) makes deleteCalls 1 and the embeddings vanish.
func TestIndexRepo_NoStateRowButExistingRows_FailureKeepsEmbeddings(t *testing.T) {
	ctx := context.Background()
	const repo = "test/loop-nogit-existing-rows"
	store := testStore(t)
	cleanRepoFull(t, store, repo)

	root := t.TempDir() // not a git repo: no SHA, hence no state row, ever
	src := filepath.Join(root, "src.go")
	require.NoError(t, os.WriteFile(src, []byte(goFile("KeptFunc")), 0o644))

	var fail atomic.Bool
	srv := fakeEmbedServerWithHook(t, func(int) error {
		if fail.Load() {
			return errors.New("injected embed failure")
		}
		return nil
	})
	writeFn := func(ctx context.Context, repoKey, sha, sourcePath string) error {
		return store.SetRepoStateWithPath(ctx, repoKey, sha, "", sourcePath)
	}
	p, delCalls := seamedPipeline(t, store, srv.URL, writeFn)

	_, err := p.IndexRepo(ctx, repo, root)
	require.NoError(t, err, "precondition: the first index succeeds")
	before, err := store.CountEmbeddings(ctx, repo)
	require.NoError(t, err)
	require.Positive(t, before, "precondition: rows exist")
	stored, err := store.GetRepoState(ctx, repo)
	require.NoError(t, err)
	require.Empty(t, stored, "precondition: a non-git repo has no state row")

	// The file is edited; the re-embed fails.
	require.NoError(t, os.WriteFile(src, []byte(goFileWithBody("KeptFunc", "_ = 1")), 0o644))
	fail.Store(true)
	_, err = p.IndexRepo(ctx, repo, root)
	require.Error(t, err, "the embed failure must propagate")

	assert.Equal(t, 0, deleteCalls(delCalls), "pre-existing embeddings must never be rolled back by a later failed attempt")
	after, cErr := store.CountEmbeddings(ctx, repo)
	require.NoError(t, cErr)
	assert.Equal(t, before, after, "embeddings committed by earlier attempts must survive")
}

// The per-pass file cap stops an incremental sync after N RE-EMBEDDED files,
// leaves head_sha where it was, and the next sync resumes: files already done
// hash-skip and do not count against the cap.
//
// Falsifiable: removing the cap check in IncrementalSync makes the first call
// embed all four files (Truncated=false, SHA advanced).
func TestIncrementalSync_FileCapTruncatesThenResumes(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()
	const repo = "test/loop-file-cap"
	cleanRepoFull(t, store, repo)

	root := initGitRepo(t, map[string]string{
		"f1.go": goFile("F1"), "f2.go": goFile("F2"), "f3.go": goFile("F3"), "f4.go": goFile("F4"),
	})
	_, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err)
	prevSHA, err := store.GetRepoState(ctx, repo)
	require.NoError(t, err)

	commitChange(t, root, map[string]string{
		"f1.go": goFileWithBody("F1", "_ = 1"), "f2.go": goFileWithBody("F2", "_ = 2"),
		"f3.go": goFileWithBody("F3", "_ = 3"), "f4.go": goFileWithBody("F4", "_ = 4"),
	}, nil)

	capped := withMaxEmbeddedFiles(ctx, 2)
	first, err := p.IncrementalSync(capped, repo, root)
	require.NoError(t, err)
	assert.True(t, first.Truncated, "two of four changed files re-embedded: the sync must report it stopped at the cap")
	assert.Equal(t, 2, first.FilesChanged, "files past the cap are not visited")
	sha, err := store.GetRepoState(ctx, repo)
	require.NoError(t, err)
	assert.Equal(t, prevSHA, sha, "head_sha must not advance while changed files are left")

	second, err := p.IncrementalSync(capped, repo, root)
	require.NoError(t, err)
	assert.False(t, second.Truncated, "the remaining two files fit under the cap")
	sha, err = store.GetRepoState(ctx, repo)
	require.NoError(t, err)
	assert.NotEqual(t, prevSHA, sha, "head_sha advances once every changed file is done")
}

// LoopSkipReason keeps the periodic pass to cheap, resumable work.
func TestLoopSkipReason(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()
	const repo = "test/loop-skip-reason"
	cleanRepoFull(t, store, repo)

	assert.Equal(t, loopSkipNoGit, p.LoopSkipReason(ctx, repo, t.TempDir()), "no resolvable HEAD")

	root := initGitRepo(t, map[string]string{"a.go": goFile("Alpha")})
	assert.Equal(t, loopSkipBootstrap, p.LoopSkipReason(ctx, repo, root), "never indexed: boot / lazy path owns the first index")

	_, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err)
	assert.Empty(t, p.LoopSkipReason(ctx, repo, root), "indexed and unchanged: the loop syncs it")

	commitChange(t, root, map[string]string{"a.go": goFileWithBody("Alpha", "_ = 1")}, nil)
	assert.Empty(t, p.LoopSkipReason(ctx, repo, root), "indexed and moved: the loop syncs it")

	require.NoError(t, store.DeleteRepo(ctx, repo))
	rawSetRepoState(t, store, repo, mustHead(t, root))
	assert.Equal(t, loopSkipNoEmbeddings, p.LoopSkipReason(ctx, repo, root), "same SHA and zero rows: full-index territory")
}

func mustHead(t *testing.T, root string) string {
	t.Helper()
	sha, err := repoMainBranchSHA(root)
	require.NoError(t, err)
	return sha
}
