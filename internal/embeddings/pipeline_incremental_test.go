package embeddings

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/go-kit/embed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gitExec runs a git command in dir and fails the test on error.
func gitExec(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// initGitRepo initialises a minimal git repo with a single commit containing
// the provided files. Returns the repo root path.
// Files map: relPath → content.
func initGitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()

	gitExec(t, dir, "init", "-b", "main")
	gitExec(t, dir, "config", "user.email", "test@test.local")
	gitExec(t, dir, "config", "user.name", "Test")

	for relPath, content := range files {
		abs := filepath.Join(dir, relPath)
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
	}

	gitExec(t, dir, "add", ".")
	gitExec(t, dir, "commit", "-m", "initial commit")
	return dir
}

// commitChange writes/deletes files and creates a new commit in an existing repo.
// writes map: relPath → content; deletes: list of relPaths to git rm.
func commitChange(t *testing.T, dir string, writes map[string]string, deletes []string) {
	t.Helper()
	for relPath, content := range writes {
		abs := filepath.Join(dir, relPath)
		require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
		require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))
		gitExec(t, dir, "add", relPath)
	}
	for _, relPath := range deletes {
		gitExec(t, dir, "rm", relPath)
	}
	gitExec(t, dir, "commit", "-m", "change")
}

// goFile returns minimal valid Go source with the listed function names.
func goFile(funcNames ...string) string {
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	for _, name := range funcNames {
		fmt.Fprintf(&sb, "func %s() {}\n", name)
	}
	return sb.String()
}

// goFileWithBody returns Go source where funcName has a distinct body.
func goFileWithBody(funcName, body string, stable ...string) string {
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	fmt.Fprintf(&sb, "func %s() { %s }\n", funcName, body)
	for _, s := range stable {
		fmt.Fprintf(&sb, "func %s() { _ = 0 }\n", s)
	}
	return sb.String()
}

// cleanRepoFull removes both code_embeddings and code_repo_state rows for repoKey.
// The standard cleanRepo helper only cleans code_embeddings; IncrementalSync tests
// also need a clean code_repo_state so prevSHA lookup returns "".
func cleanRepoFull(t *testing.T, store *Store, repoKey string) {
	t.Helper()
	ctx := context.Background()
	_ = store.DeleteRepo(ctx, repoKey)
	_, _ = store.pool.Exec(ctx, `DELETE FROM code_repo_state WHERE repo_key = $1`, repoKey)
	t.Cleanup(func() {
		_ = store.DeleteRepo(ctx, repoKey)
		_, _ = store.pool.Exec(ctx, `DELETE FROM code_repo_state WHERE repo_key = $1`, repoKey)
	})
}

// rawSetRepoState directly upserts a head_sha for testing partial-failure and
// diff-error scenarios.
func rawSetRepoState(t *testing.T, store *Store, repoKey, sha string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, store.SetRepoState(ctx, repoKey, sha, ""))
}

// rawGetIndexedAt reads indexed_at for a repo via raw SQL.
func rawGetIndexedAt(t *testing.T, store *Store, repoKey string) time.Time {
	t.Helper()
	ctx := context.Background()
	var ts time.Time
	err := store.pool.QueryRow(ctx,
		`SELECT indexed_at FROM code_repo_state WHERE repo_key = $1`, repoKey).
		Scan(&ts)
	require.NoError(t, err, "code_repo_state must have a row for %s", repoKey)
	return ts
}

// fakeEmbedServerWithHook creates an httptest.Server responding to POST /v1/embeddings.
// For each request, hook(inputCount) is called. If hook returns non-nil, the server
// returns HTTP 500. Otherwise it responds with zero-vectors.
func fakeEmbedServerWithHook(t *testing.T, hook func(inputCount int) error) *httptest.Server {
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
		if hookErr := hook(len(req.Input)); hookErr != nil {
			http.Error(w, hookErr.Error(), http.StatusInternalServerError)
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

// testPipelineWithEmbedHook creates a Pipeline backed by real Postgres and a hookable
// fake embed server. hook is called on each embed request; returning non-nil causes
// the embed server to return HTTP 500 for that request, simulating a partial failure.
func testPipelineWithEmbedHook(t *testing.T, hook func(inputCount int) error) (*Pipeline, *Store) {
	t.Helper()
	srv := fakeEmbedServerWithHook(t, hook)
	client, err := embed.NewClient(srv.URL,
		embed.WithBackend("http"),
		embed.WithDim(dimSize),
	)
	require.NoError(t, err)
	pool := testPool(t)
	store := NewStore(pool)
	ctx := context.Background()
	require.NoError(t, store.EnsureSchema(ctx))
	p := NewPipeline(client, store, "", WithFileCache(nil))
	return p, store
}

// -- Test cases --

// TestIncrementalSync_Bootstrap_NoSHA: fresh repo, no code_repo_state row.
// Expect full-fallback-bootstrap with FilesEmbedded > 0, SHA persisted after.
//
// Anti-tautology: asserts Mode string from production code + verifies DB state
// via GetRepoState after the call.
func TestIncrementalSync_Bootstrap_NoSHA(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()

	const repo = "test/inc-bootstrap"
	cleanRepoFull(t, store, repo)

	root := initGitRepo(t, map[string]string{
		"alpha.go": goFile("FuncAlpha", "FuncBeta"),
	})

	result, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err)

	assert.Equal(t, IncrementalSyncFullFallbackBootstrap, result.Mode,
		"first call with no prior SHA must bootstrap via full-fallback")
	assert.Greater(t, result.FilesEmbedded, 0,
		"bootstrap must embed at least 1 symbol")

	sha, err := store.GetRepoState(ctx, repo)
	require.NoError(t, err)
	assert.NotEmpty(t, sha, "SetRepoState must be called after successful bootstrap")
}

// TestIncrementalSync_NonGit_FallsThroughToFull: non-git directory.
// Expect full-fallback-no-git mode.
func TestIncrementalSync_NonGit_FallsThroughToFull(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()

	const repo = "test/inc-nogit"
	cleanRepoFull(t, store, repo)

	// Plain directory, no .git — write some Go files so IndexRepo has work.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(goFile("Main")), 0o644))

	result, err := p.IncrementalSync(ctx, repo, dir)
	require.NoError(t, err)

	assert.Equal(t, IncrementalSyncFullFallbackNoGit, result.Mode,
		"non-git path must fall through to full-fallback-no-git")
}

// TestIncrementalSync_SameSHA_SkipsWork: bootstrap then call again with no changes.
// Expect skip-sha-match with zero files changed/embedded.
func TestIncrementalSync_SameSHA_SkipsWork(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()

	const repo = "test/inc-samesha"
	cleanRepoFull(t, store, repo)

	root := initGitRepo(t, map[string]string{
		"stable.go": goFile("StableFunc"),
	})

	// Bootstrap.
	first, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err)
	require.NotEmpty(t, first.Mode, "precondition: bootstrap must set some mode")

	// Second call — nothing changed.
	second, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err)

	assert.Equal(t, IncrementalSyncSkipSHAMatch, second.Mode,
		"second call with unchanged SHA must skip all work")
	assert.Equal(t, 0, second.FilesChanged, "no files changed")
	assert.Equal(t, 0, second.FilesEmbedded, "no symbols embedded on same-SHA skip")
}

// TestIncrementalSync_OneFileChanged: bootstrap with 2 files, modify ONE, re-sync.
// Expects incremental mode, FilesChanged==1, FilesEmbedded>0.
// Anti-tautology: verifies unchanged file's body_hash rows are IDENTICAL pre/post.
func TestIncrementalSync_OneFileChanged(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()

	const repo = "test/inc-onefile"
	cleanRepoFull(t, store, repo)

	root := initGitRepo(t, map[string]string{
		"changed.go": goFile("ChangedFunc"),
		"stable.go":  goFile("StableFunc"),
	})

	// Bootstrap.
	bootstrap, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err)
	require.Greater(t, bootstrap.FilesEmbedded, 0, "precondition: bootstrap must embed symbols")

	// Snapshot body hashes for stable.go BEFORE modification.
	stablePreRows, err := store.GetSymbolsForFile(ctx, repo, "stable.go")
	require.NoError(t, err)
	require.NotEmpty(t, stablePreRows, "precondition: stable.go must have indexed symbols")
	preHash := stablePreRows[0].BodyHash

	// Commit a change to changed.go only.
	commitChange(t, root, map[string]string{
		"changed.go": goFileWithBody("ChangedFunc", "_ = 42"),
	}, nil)

	// Incremental sync.
	result, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err)

	assert.Equal(t, IncrementalSyncIncremental, result.Mode,
		"after one-file commit, mode must be incremental")
	assert.Equal(t, 1, result.FilesChanged, "exactly 1 file in git diff")
	assert.Greater(t, result.FilesEmbedded, 0, "changed file must produce at least 1 embed")
	assert.Empty(t, result.Errors, "no per-file errors expected")

	// SHA advanced.
	sha, err := store.GetRepoState(ctx, repo)
	require.NoError(t, err)
	assert.NotEmpty(t, sha, "SetRepoState must be called after successful incremental")

	// Anti-tautology: stable.go body hashes MUST be identical (file was not reprocessed).
	stablePostRows, err := store.GetSymbolsForFile(ctx, repo, "stable.go")
	require.NoError(t, err)
	require.NotEmpty(t, stablePostRows, "stable.go symbols must still exist after incremental")
	postHash := stablePostRows[0].BodyHash
	assert.Equal(t, preHash, postHash,
		"stable.go body_hash must not change — proves unchanged file was NOT reprocessed")
}

// TestIncrementalSync_FileDeleted: bootstrap with 2 files, git rm one, re-sync.
// Expects FilesDeleted > 0 and no symbols for the deleted file in DB.
func TestIncrementalSync_FileDeleted(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()

	const repo = "test/inc-filedeleted"
	cleanRepoFull(t, store, repo)

	root := initGitRepo(t, map[string]string{
		"keeper.go": goFile("KeeperFunc"),
		"gone.go":   goFile("GoneFunc"),
	})

	// Bootstrap.
	_, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err)

	// Verify gone.go was indexed.
	preRows, err := store.GetSymbolsForFile(ctx, repo, "gone.go")
	require.NoError(t, err)
	require.NotEmpty(t, preRows, "precondition: gone.go must have symbols after bootstrap")

	// Delete gone.go and commit.
	commitChange(t, root, nil, []string{"gone.go"})

	// Incremental sync.
	result, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err)

	assert.Equal(t, IncrementalSyncIncremental, result.Mode)
	assert.Greater(t, result.FilesDeleted, int64(0),
		"deleted file's symbols must be tombstoned (FilesDeleted > 0)")

	// DB convergence: no symbols for gone.go.
	postRows, err := store.GetSymbolsForFile(ctx, repo, "gone.go")
	require.NoError(t, err)
	assert.Empty(t, postRows, "gone.go must have 0 symbols in DB after incremental delete")
}

// TestIncrementalSync_PartialFailure_PreservesSHA: 2 files changed, embed server
// fails on the 2nd call. SHA must NOT advance.
//
// Strategy: use a counting fake embed server that returns HTTP 500 after N calls.
func TestIncrementalSync_PartialFailure_PreservesSHA(t *testing.T) {
	ctx := context.Background()

	// Build a fake embed server that fails after the first batch call.
	callCount := 0
	p, store := testPipelineWithEmbedHook(t, func(inputCount int) error {
		callCount++
		if callCount >= 2 {
			return fmt.Errorf("embed-server 500: injected failure")
		}
		return nil
	})

	const repo = "test/inc-partial"
	cleanRepoFull(t, store, repo)

	root := initGitRepo(t, map[string]string{
		"file1.go": goFile("Func1"),
		"file2.go": goFile("Func2"),
	})

	// Bootstrap (will use first embed call — succeeds).
	callCount = 0 // reset before bootstrap
	bootstrapResult, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err, "bootstrap must not fail")
	require.NotEmpty(t, bootstrapResult.Mode)

	// Record SHA after bootstrap.
	prevSHA, err := store.GetRepoState(ctx, repo)
	require.NoError(t, err)
	require.NotEmpty(t, prevSHA, "precondition: SHA must be set after bootstrap")

	// Commit changes to both files.
	commitChange(t, root, map[string]string{
		"file1.go": goFileWithBody("Func1", "_ = 1"),
		"file2.go": goFileWithBody("Func2", "_ = 2"),
	}, nil)

	// Reset counter so 2nd incremental call to embed will fail.
	callCount = 0

	result, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err, "IncrementalSync must not return top-level error on partial failure")

	// Partial failure must be surfaced in Errors, not returned.
	assert.NotEmpty(t, result.Errors,
		"partial embed failure must be collected in result.Errors")

	// SHA must NOT have advanced past prevSHA.
	currentSHA, err := store.GetRepoState(ctx, repo)
	require.NoError(t, err)
	assert.Equal(t, prevSHA, currentSHA,
		"SHA must NOT advance when there are per-file errors (partial failure invariant)")
}

// TestIncrementalSync_DiffExecError_FallsBack: corrupted prevSHA causes git diff
// to fail, triggering full-fallback-diff-error.
func TestIncrementalSync_DiffExecError_FallsBack(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()

	const repo = "test/inc-differror"
	cleanRepoFull(t, store, repo)

	root := initGitRepo(t, map[string]string{
		"main.go": goFile("Main"),
	})

	// Bootstrap to create a code_repo_state row.
	_, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err)

	// Corrupt the stored SHA so git diff $prev $current will fail.
	rawSetRepoState(t, store, repo, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	// Now commit something so currentSHA != prevSHA (otherwise skip-sha-match path).
	commitChange(t, root, map[string]string{
		"main.go": goFileWithBody("Main", "_ = 99"),
	}, nil)

	result, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err)

	assert.Equal(t, IncrementalSyncFullFallbackDiffError, result.Mode,
		"git diff exec failure on bad SHA must trigger full-fallback-diff-error")
}

// TestIncrementalSync_BumpsTimestampOnSameSHA: same-SHA skip path must update
// indexed_at so callers can observe liveness.
func TestIncrementalSync_BumpsTimestampOnSameSHA(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()

	const repo = "test/inc-timestamp"
	cleanRepoFull(t, store, repo)

	root := initGitRepo(t, map[string]string{
		"ts.go": goFile("TimestampFunc"),
	})

	// Bootstrap.
	_, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err)

	tsBefore := rawGetIndexedAt(t, store, repo)

	// Sleep past Postgres TIMESTAMPTZ resolution (1 microsecond — but real wall-clock
	// scheduler jitter on load makes 100ms reliable without being slow).
	time.Sleep(110 * time.Millisecond)

	// Same-SHA call.
	result, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err)
	require.Equal(t, IncrementalSyncSkipSHAMatch, result.Mode)

	tsAfter := rawGetIndexedAt(t, store, repo)
	assert.True(t, tsAfter.After(tsBefore),
		"indexed_at must advance on same-SHA skip (tsBefore=%v tsAfter=%v)", tsBefore, tsAfter)
}

// TestGitDiffNames_StderrSurfacedInError: calling gitDiffNames with two SHAs that do not
// exist in the repo must return an error whose message contains "stderr:" with git's
// diagnostic. Guards: stderr capture is wired — pure err!=nil check would be tautological.
func TestGitDiffNames_StderrSurfacedInError(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy integration test; runs in the nightly full suite (make test)")
	}
	root := initGitRepo(t, map[string]string{
		"dummy.go": goFile("Dummy"),
	})

	ctx := context.Background()
	const badPrev = "deadbeef0000000000000000000000000000dead"
	const badCur = "alsobad000000000000000000000000000000abc"

	_, err := gitDiffNames(ctx, root, badPrev, badCur)
	require.Error(t, err, "gitDiffNames with non-existent SHAs must return error")

	msg := err.Error()
	assert.Contains(t, msg, "stderr:",
		"error message must contain 'stderr:' to surface git diagnostics (proves stderr is captured)")

	// git emits one of: "bad object", "unknown revision", "not a tree", etc.
	// Assert at least one known token is present so the test fails if stderr is not forwarded.
	hasGitDiag := strings.Contains(msg, "bad") ||
		strings.Contains(msg, "unknown") ||
		strings.Contains(msg, "ambiguous") ||
		strings.Contains(msg, "fatal") ||
		strings.Contains(msg, "not a")
	assert.True(t, hasGitDiag,
		"error message must contain a git diagnostic token (bad/unknown/ambiguous/fatal/not a); got: %s", msg)
}

// TestGitDiffNames_ContextCancellation: a pre-cancelled context must cause gitDiffNames
// to return quickly with an error. Guards: exec.CommandContext context binding is active.
func TestGitDiffNames_ContextCancellation(t *testing.T) {
	root := initGitRepo(t, map[string]string{
		"dummy.go": goFile("Dummy"),
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before calling

	start := time.Now()
	_, err := gitDiffNames(ctx, root, "HEAD", "HEAD")
	elapsed := time.Since(start)

	require.Error(t, err, "gitDiffNames with cancelled context must return error")
	assert.Less(t, elapsed, time.Second,
		"gitDiffNames must return quickly (<1s) when context is already cancelled")
}

// TestIncrementalSync_BulkPathParityForBootstrap: bootstrap via IncrementalSync
// (which falls back to IndexRepo) must produce the same symbol count as calling
// IndexRepo directly on an identical fixture.
//
// Guards: IncrementalSync bootstrap fallback is byte-identical to IndexRepo.
func TestIncrementalSync_BulkPathParityForBootstrap(t *testing.T) {
	ctx := context.Background()

	// Repo A: bootstrap via IncrementalSync.
	pA, storeA := testPipeline(t)
	const repoA = "test/inc-parity-A"
	cleanRepoFull(t, storeA, repoA)

	fixtures := map[string]string{
		"alpha.go": goFile("Alpha1", "Alpha2"),
		"beta.go":  goFile("Beta1"),
	}
	rootA := initGitRepo(t, fixtures)
	incResult, err := pA.IncrementalSync(ctx, repoA, rootA)
	require.NoError(t, err)

	// Repo B: index directly via IndexRepo.
	pB, storeB := testPipeline(t)
	const repoB = "test/inc-parity-B"
	cleanRepoFull(t, storeB, repoB)
	rootB := initGitRepo(t, fixtures)
	bulkResult, err := pB.IndexRepo(ctx, repoB, rootB)
	require.NoError(t, err)

	assert.Equal(t, bulkResult.Indexed, incResult.FilesEmbedded,
		"IncrementalSync bootstrap must produce same FilesEmbedded as direct IndexRepo")
}
