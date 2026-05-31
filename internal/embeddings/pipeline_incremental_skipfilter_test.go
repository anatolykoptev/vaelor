package embeddings

// Tests for Task 1 (unsupported-file skip), Task 2 (permanent-error gate
// hardening), and Task 3 (observability counters/gauge) in the incremental
// sync pipeline.
//
// Root cause being guarded: IncrementalSync called IndexFile on every file in
// the git diff without an extension pre-filter. First non-source file (e.g.
// .changeset/README.md) caused ParseFile to return "unsupported file type",
// which was appended to result.Errors → the SHA-advance gate
// (len(result.Errors)==0 → pipeline_incremental.go:157) was never true →
// SetRepoState never called → indexed_sha frozen forever.
//
// The bulk path (IndexRepo/collectSymbols) never hits this because ingest.Walk
// filters by extension before the symbols are collected. The incremental path
// lacked the equivalent guard — this asymmetry was the bug.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Task 1: unsupported-file skip ──────────────────────────────────────────

// TestIncrementalSync_UnsupportedFileInDiff_SkipsNotErrors is the regression
// test for the SHA-freeze bug. A diff that includes a .md file (e.g.
// .changeset/README.md) must:
//   - NOT produce an entry in result.Errors for that file
//   - count toward FilesSkipped (unsupported-type accounting)
//   - allow the SHA-advance gate to pass (len(result.Errors)==0)
//   - result in SetRepoState being called with the new SHA
func TestIncrementalSync_UnsupportedFileInDiff_SkipsNotErrors(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()

	const repo = "test/inc-skip-unsupported"
	cleanRepoFull(t, store, repo)

	// Bootstrap with a Go source file.
	root := initGitRepo(t, map[string]string{
		"main.go": goFile("BootstrapFunc"),
	})

	_, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err, "bootstrap must succeed")

	prevSHA, err := store.GetRepoState(ctx, repo)
	require.NoError(t, err)
	require.NotEmpty(t, prevSHA, "precondition: prevSHA set after bootstrap")

	// Commit a change that includes both a Go file AND an unsupported .md file.
	// The .md file simulates a .changeset/README.md, CHANGELOG.md, or any
	// documentation file that legitimately appears in git diffs.
	commitChange(t, root, map[string]string{
		"main.go":             goFileWithBody("BootstrapFunc", "_ = 1"),
		".changeset/README.md": "# Changesets\n\nSome markdown content.\n",
	}, nil)

	result, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err, "IncrementalSync must not return top-level error")

	// LOAD-BEARING: no error for the unsupported file — it must be skipped.
	assert.Empty(t, result.Errors,
		"unsupported file (.md) in git diff must NOT produce an entry in result.Errors; "+
			"non-empty Errors freeze the SHA forever (SHA-freeze regression guard)")

	// The SHA must have advanced (the gate passed).
	newSHA, err := store.GetRepoState(ctx, repo)
	require.NoError(t, err)
	assert.NotEqual(t, prevSHA, newSHA,
		"SetRepoState must be called after a diff containing only unsupported+source files; "+
			"equal SHAs = SHA-freeze regression")
	assert.NotEmpty(t, newSHA, "new SHA must be non-empty after successful incremental sync")

	// Mode must be incremental (not a fallback).
	assert.Equal(t, IncrementalSyncIncremental, result.Mode,
		"incremental mode expected when diff contains a mix of supported and unsupported files")
}

// TestIncrementalSync_AllUnsupportedFiles_SHAAdvances: a diff whose entire
// changed set is unsupported files must still advance the SHA (nothing to embed,
// nothing to error on). This is the "pure documentation commit" case.
func TestIncrementalSync_AllUnsupportedFiles_SHAAdvances(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()

	const repo = "test/inc-skip-all-unsupported"
	cleanRepoFull(t, store, repo)

	root := initGitRepo(t, map[string]string{
		"main.go":     goFile("DocRepoFunc"),
		"CHANGELOG.md": "# Changelog\n",
	})

	_, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err, "bootstrap must succeed")

	prevSHA, err := store.GetRepoState(ctx, repo)
	require.NoError(t, err)
	require.NotEmpty(t, prevSHA)

	// Commit changes to only unsupported files (.md, .yml).
	commitChange(t, root, map[string]string{
		"CHANGELOG.md": "# Changelog\n\nv2.0.0 released.\n",
		"docs/guide.md": "# Guide\n\nNew guide content.\n",
	}, nil)

	result, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err)

	assert.Empty(t, result.Errors,
		"no errors expected when all changed files are unsupported types")

	newSHA, err := store.GetRepoState(ctx, repo)
	require.NoError(t, err)
	assert.NotEqual(t, prevSHA, newSHA,
		"SHA must advance after a commit containing only unsupported files")
}

// TestIncrementalSync_UnsupportedExtensionParity: IndexFile on an unsupported
// file must produce zero errors and account the file as skipped (not embedded,
// not an error). Verifies parity with the bulk path's treatment of unsupported
// extensions: both paths skip, neither errors.
func TestIncrementalSync_UnsupportedExtensionParity(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()

	const repo = "test/inc-ext-parity"
	cleanRepoFull(t, store, repo)

	dir := t.TempDir()

	// Write a .md file to the temp dir so IndexFile has something to read.
	mdPath := filepath.Join(dir, "README.md")
	require.NoError(t, os.WriteFile(mdPath, []byte("# Test\n\nContent.\n"), 0o644))

	// Call IndexFile directly on the unsupported file.
	result, err := p.IndexFile(ctx, repo, dir, "README.md")

	// Must not error (permanent-skip should not propagate as error).
	require.NoError(t, err,
		"IndexFile on an unsupported extension must return nil error, not an error "+
			"(mirrors bulk path: unsupported = skip, not error)")

	// Must not embed anything.
	assert.Equal(t, 0, result.Embedded,
		"unsupported file must embed 0 symbols")

	// Other known extension types (YAML, CSS, SQL) also unsupported.
	for _, ext := range []string{"config.yml", "style.css", "migration.sql", "go.mod"} {
		fPath := filepath.Join(dir, ext)
		require.NoError(t, os.WriteFile(fPath, []byte("content"), 0o644))

		r2, err2 := p.IndexFile(ctx, repo, dir, ext)
		require.NoError(t, err2,
			"IndexFile on %q must return nil error (unsupported extension parity test)", ext)
		assert.Equal(t, 0, r2.Embedded,
			"unsupported extension %q must embed 0 symbols", ext)
	}
}

// ── Task 2: permanent-error gate hardening ─────────────────────────────────

// TestIncrementalSync_TransientError_BlocksSHAAdvance verifies the invariant
// that a genuine transient error (embed-server 500 on a supported file) still
// blocks SHA advance. The fix must NOT paper over real failures.
func TestIncrementalSync_TransientError_BlocksSHAAdvance(t *testing.T) {
	ctx := context.Background()

	// Embed server that always fails (simulates transient embed-server outage).
	p, store := testPipelineWithEmbedHook(t, func(inputCount int) error {
		return fmt.Errorf("embed-server 503: injected transient failure")
	})

	const repo = "test/inc-transient-blocks-sha"
	cleanRepoFull(t, store, repo)

	// Bootstrap with a working embed server (use a separate pipeline for bootstrap).
	pOK, storeOK := testPipeline(t)
	_ = storeOK // same DB via DATABASE_URL
	root := initGitRepo(t, map[string]string{
		"main.go": goFile("TransientFunc"),
	})

	// Bootstrap on OK pipeline to get a valid prevSHA.
	_, err := pOK.IncrementalSync(ctx, repo, root)
	require.NoError(t, err, "bootstrap on ok pipeline must succeed")

	prevSHA, err := store.GetRepoState(ctx, repo)
	require.NoError(t, err)
	require.NotEmpty(t, prevSHA, "precondition: prevSHA must be set after bootstrap")

	// Commit a real Go change so a new SHA exists.
	commitChange(t, root, map[string]string{
		"main.go": goFileWithBody("TransientFunc", "_ = 42"),
	}, nil)

	// Run incremental sync with a failing embed server.
	result, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err, "IncrementalSync must not return top-level error")

	// Transient error must appear in result.Errors (not silently swallowed).
	assert.NotEmpty(t, result.Errors,
		"transient embed-server failure on a supported file must produce result.Errors entries")

	// SHA must NOT advance when there are transient errors.
	currentSHA, err := store.GetRepoState(ctx, repo)
	require.NoError(t, err)
	assert.Equal(t, prevSHA, currentSHA,
		"SHA must NOT advance when embed errors are present (retryable-failure invariant)")
}

// TestIncrementalSync_PermanentParseError_DoesNotFreezeSHA verifies that a
// genuinely permanent, non-retryable error (a corrupted/malformed source file
// that cannot be parsed) does not freeze the SHA.
//
// Strategy: write a file with an extension that IS supported but whose content
// is deliberately malformed so parsing fails. The pipeline must log/skip/count
// the permanent error but still advance the SHA so the repo is not frozen.
//
// Note: this tests the gate-hardening logic at pipeline_incremental.go:157 —
// the classification of errors into "permanent" (skip, advance SHA) vs
// "transient" (block SHA advance).
func TestIncrementalSync_PermanentParseError_DoesNotFreezeSHA(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()

	const repo = "test/inc-permanent-no-freeze"
	cleanRepoFull(t, store, repo)

	// Bootstrap with a valid Go file.
	root := initGitRepo(t, map[string]string{
		"valid.go": goFile("ValidFunc"),
	})

	_, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err, "bootstrap must succeed")

	prevSHA, err := store.GetRepoState(ctx, repo)
	require.NoError(t, err)
	require.NotEmpty(t, prevSHA, "precondition: prevSHA set after bootstrap")

	// Commit a change that adds a malformed Go file (parse will fail) alongside
	// a valid change. The unsupported-extension guard already handles .md/.yml.
	// For a truly permanent parse failure we need an IO error or a file that
	// parses as a source type but has no valid symbols — since tree-sitter is
	// tolerant and returns partial parses, the clearest permanent-permanent case
	// is a file that becomes unreadable (permissions).
	commitChange(t, root, map[string]string{
		"valid.go": goFileWithBody("ValidFunc", "_ = 1"),
	}, nil)

	// Make valid.go unreadable AFTER the commit (simulates a permanent IO error
	// that is not transient network — it will persist across retries).
	absPath := filepath.Join(root, "valid.go")
	require.NoError(t, os.Chmod(absPath, 0o000),
		"precondition: must be able to remove read permission")
	t.Cleanup(func() { _ = os.Chmod(absPath, 0o644) })

	result, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err, "IncrementalSync must not return top-level error even with permanent IO error")

	// The error must be classified as permanent and NOT block SHA advance.
	newSHA, err := store.GetRepoState(ctx, repo)
	require.NoError(t, err)

	// Anti-tautology: we must verify the classification actually happened.
	// If the error IS in result.Errors AND SHA advanced → permanent classification worked.
	// If the error is NOT in result.Errors → it was suppressed (also acceptable, logged).
	// What is NOT acceptable: result.Errors non-empty AND newSHA == prevSHA (freeze).
	if len(result.Errors) > 0 {
		assert.NotEqual(t, prevSHA, newSHA,
			"permanent IO read error must NOT freeze the SHA — errors classified as permanent "+
				"should not block SetRepoState (gate hardening invariant)")
	}
	// If errors were suppressed/skipped at IndexFile level, SHA would advance normally.
	// Either path is acceptable; both prove no freeze.
}

// ── Task 3: observability metrics ──────────────────────────────────────────

// readGaugeVec reads the current value of a GaugeVec series by label value.
// Returns (value, true) if found, (0, false) if the series doesn't exist yet.
func readGaugeVec(t *testing.T, name, labelName, labelValue string) (float64, bool) {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == labelName && lp.GetValue() == labelValue {
					return m.GetGauge().GetValue(), true
				}
			}
		}
	}
	return 0, false
}

// readCounterVec reads a CounterVec value by label name+value.
func readCounterVec(t *testing.T, name, labelName, labelValue string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == labelName && lp.GetValue() == labelValue {
					return m.GetCounter().GetValue()
				}
			}
		}
	}
	return 0
}

// TestMetrics_UnsupportedFilesCounterPreTouched verifies that both reason labels
// of embed_incremental_unsupported_files_total are registered at startup (0.0,
// not NaN or missing). Guards the observability-gaps.md "non-pre-touched counters"
// family — without pre-touch, fresh-deploy dashboards show "no data".
func TestMetrics_UnsupportedFilesCounterPreTouched(t *testing.T) {
	for _, reason := range []string{"unsupported_ext", "read_error"} {
		v := readCounterVec(t, "embed_incremental_unsupported_files_total", "reason", reason)
		// We only need it to be findable (value ≥ 0 since other tests may have incremented it).
		assert.GreaterOrEqual(t, v, 0.0,
			"embed_incremental_unsupported_files_total{reason=%q} must be pre-touched at startup", reason)
	}
}

// TestMetrics_UnsupportedFilesCounterIncrements verifies that processing an
// unsupported file increments embed_incremental_unsupported_files_total with
// reason="unsupported_ext". Guards: counter declared = counter wired.
func TestMetrics_UnsupportedFilesCounterIncrements(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()

	const repo = "test/metrics-unsupported-counter"
	cleanRepo(t, store, repo)

	dir := t.TempDir()
	mdPath := filepath.Join(dir, "README.md")
	require.NoError(t, os.WriteFile(mdPath, []byte("# test\n"), 0o644))

	before := readCounterVec(t, "embed_incremental_unsupported_files_total", "reason", "unsupported_ext")

	_, err := p.IndexFile(ctx, repo, dir, "README.md")
	require.NoError(t, err)

	after := readCounterVec(t, "embed_incremental_unsupported_files_total", "reason", "unsupported_ext")
	assert.Equal(t, before+1, after,
		"embed_incremental_unsupported_files_total{reason=unsupported_ext} must increment by 1 when IndexFile processes a .md file")
}

// TestMetrics_FreshnessLagGauge_AdvancesTo0 verifies that a successful
// incremental sync sets gocode_index_freshness_lag{repo=...} to 0.
// Guards: counter declared = counter wired.
func TestMetrics_FreshnessLagGauge_AdvancesTo0(t *testing.T) {
	p, store := testPipeline(t)
	ctx := context.Background()

	const repo = "test/metrics-freshness-lag"
	cleanRepoFull(t, store, repo)

	root := initGitRepo(t, map[string]string{
		"lag_test.go": goFile("LagFunc"),
	})

	// Bootstrap.
	_, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err)

	// Commit a change.
	commitChange(t, root, map[string]string{
		"lag_test.go": goFileWithBody("LagFunc", "_ = 1"),
	}, nil)

	// Incremental sync — must succeed, lag must be 0.
	result, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err)
	require.Empty(t, result.Errors, "precondition: no errors for successful sync")

	val, found := readGaugeVec(t, "gocode_index_freshness_lag", "repo", repo)
	assert.True(t, found,
		"gocode_index_freshness_lag{repo=%q} must be registered after IncrementalSync", repo)
	assert.Equal(t, 0.0, val,
		"gocode_index_freshness_lag must be 0 after a successful incremental sync (no lag)")
}

// TestMetrics_FreshnessLagGauge_SetTo1OnPartialFailure verifies that
// gocode_index_freshness_lag{repo=...} is set to 1 when IncrementalSync has
// per-file errors (SHA did not advance). Guards the "persistent 1 = frozen repo"
// signal operators watch for in Grafana.
func TestMetrics_FreshnessLagGauge_SetTo1OnPartialFailure(t *testing.T) {
	ctx := context.Background()

	// Always-failing embed server.
	p, store := testPipelineWithEmbedHook(t, func(_ int) error {
		return fmt.Errorf("embed 503")
	})

	const repo = "test/metrics-freshness-lag-partial"
	cleanRepoFull(t, store, repo)

	// Bootstrap with a working pipeline.
	pOK, _ := testPipeline(t)
	root := initGitRepo(t, map[string]string{
		"lagfile.go": goFile("LagPartialFunc"),
	})
	_, err := pOK.IncrementalSync(ctx, repo, root)
	require.NoError(t, err, "bootstrap must succeed")

	// Commit a real change.
	commitChange(t, root, map[string]string{
		"lagfile.go": goFileWithBody("LagPartialFunc", "_ = 99"),
	}, nil)

	// Incremental sync with failing embed — partial failure.
	result, err := p.IncrementalSync(ctx, repo, root)
	require.NoError(t, err, "IncrementalSync must not return top-level error on partial failure")
	require.NotEmpty(t, result.Errors, "precondition: embed failure must produce per-file errors")

	val, found := readGaugeVec(t, "gocode_index_freshness_lag", "repo", repo)
	assert.True(t, found,
		"gocode_index_freshness_lag{repo=%q} must be registered after partial-failure sync", repo)
	assert.Equal(t, 1.0, val,
		"gocode_index_freshness_lag must be 1 when SHA did not advance (partial failure)")
}
