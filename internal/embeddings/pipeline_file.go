package embeddings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/anatolykoptev/vaelor/internal/strutil"
)

// FileIndexResult summarizes the outcome of a single-file incremental index.
type FileIndexResult struct {
	Embedded int   // newly-embedded symbol count (new or body-changed)
	Skipped  int   // hash-matched symbols — no embed call issued
	Deleted  int64 // symbols removed from DB (file shrank or was deleted)
}

// IndexFile incrementally indexes one file: parses it, extracts symbols,
// computes the diff against the currently-indexed set (Store.GetSymbolsForFile),
// embeds only NEW or CHANGED symbols (body_hash differs), and DELETEs symbols
// that disappeared from this file in the new parse.
//
// root is the repo's absolute path on the host (for symbol path resolution);
// relPath is the file path relative to root (used as code_embeddings.file_path).
// repoKey is the canonical repo identifier (see codegraph.GraphNameFor).
//
// Returns counts of newly-embedded, unchanged (skipped), and deleted symbols.
//
// Side effects: Postgres writes; HTTP call to embed-server ONLY for new/changed
// symbols (zero HTTP calls if file unchanged from last index).
//
// IndexFile does NOT touch repo_state / repoMainBranchSHA — that is repo-level
// fingerprinting owned by IndexRepo. IndexFile is file-level only.
func (p *Pipeline) IndexFile(ctx context.Context, repoKey, root, relPath string) (*FileIndexResult, error) {
	absPath := filepath.Join(root, relPath)

	// Mirror indexRepo's isTestFile filter: test files are never indexed via the
	// bulk path. If an older code version mis-indexed this file, evict its rows
	// now to converge state.
	if isTestFile(relPath) {
		deleted, err := p.store.DeleteSymbolsForFile(ctx, repoKey, relPath, nil)
		if err != nil {
			return nil, fmt.Errorf("test-file evict: %w", err)
		}
		return &FileIndexResult{Deleted: deleted}, nil
	}

	// Mirror indexRepo's MaxFileBytes filter: oversized files (vendored bundles,
	// generated lockfiles, etc) are never indexed via the bulk path. Evict any
	// pre-existing rows for this path to converge state.
	// When the file does not exist, Stat returns a non-nil error so the condition
	// is false; the os.ErrNotExist path below handles that case.
	if fi, statErr := os.Stat(absPath); statErr == nil && fi.Size() > maxIndexFileBytes {
		deleted, err := p.store.DeleteSymbolsForFile(ctx, repoKey, relPath, nil)
		if err != nil {
			return nil, fmt.Errorf("oversized-file evict: %w", err)
		}
		return &FileIndexResult{Deleted: deleted}, nil
	}

	// Fast path: file no longer exists on disk → evict all its symbols.
	if _, err := os.Stat(absPath); errors.Is(err, os.ErrNotExist) {
		n, delErr := p.store.DeleteSymbolsForFile(ctx, repoKey, relPath, nil)
		if delErr != nil {
			return nil, fmt.Errorf("indexFile: delete for missing file %s: %w", relPath, delErr)
		}
		slog.Debug("indexFile: file absent — evicted all symbols",
			slog.String("repo", repoKey),
			slog.String("file", relPath),
			slog.Int64("deleted", n))
		return &FileIndexResult{Deleted: n}, nil
	}

	// Parse the file, collect current symbols, and compute the diff vs DB.
	toEmbed, currentNames, result, err := p.parseAndDiff(ctx, repoKey, relPath, absPath)
	if err != nil {
		return nil, err
	}

	// Delete symbols in DB that are no longer in the current parse.
	deleted, err := p.store.DeleteSymbolsForFile(ctx, repoKey, relPath, currentNames)
	if err != nil {
		return nil, fmt.Errorf("indexFile: delete stale symbols: %w", err)
	}
	result.Deleted = deleted

	// Embed and upsert new/changed symbols.
	if len(toEmbed) > 0 {
		n, err := p.embedAndUpsert(ctx, repoKey, toEmbed)
		if err != nil {
			return nil, fmt.Errorf("indexFile: embed %s: %w", relPath, err)
		}
		result.Embedded = n
	}

	slog.Debug("indexFile: done",
		slog.String("repo", repoKey),
		slog.String("file", relPath),
		slog.Int("embedded", result.Embedded),
		slog.Int("skipped", result.Skipped),
		slog.Int64("deleted", result.Deleted))

	return result, nil
}

// parseAndDiff reads and parses absPath, fetches existing DB symbols for relPath,
// and computes which symbols need embedding vs skipping. Extracted to reduce
// cyclomatic complexity of IndexFile.
//
// Returns:
//   - toEmbed: symbols to embed (new or body-hash changed)
//   - currentNames: all symbol names in the new parse (keep-list for DeleteSymbolsForFile)
//   - result: partial FileIndexResult with Skipped populated
//   - err: first error encountered
func (p *Pipeline) parseAndDiff(
	ctx context.Context,
	repoKey, relPath, absPath string,
) (toEmbed []symbolEntry, currentNames []string, result *FileIndexResult, err error) {
	result = &FileIndexResult{}

	// Mirror the bulk path's language filter: unsupported extensions (e.g.
	// .md, .yml, .yaml, .sql, .css, go.mod) are skipped rather than errored.
	// The bulk path never reaches IndexFile for these files — ingest.Walk
	// filters by extension before symbol collection. The incremental path
	// calls IndexFile per file from the git diff, so the guard must live here.
	//
	// Returning (nil, nil, result, nil) with Skipped=0 signals "nothing to do,
	// no error" to the caller. The SHA-advance gate (len(IncrementalSyncResult.Errors)==0,
	// checked in IncrementalSync one frame up) stays reachable even when every
	// changed file is unsupported.
	lang := ingest.DetectLanguage(filepath.Base(relPath))
	if lang == "" {
		incrementalFilesUnsupportedTotal.WithLabelValues("unsupported_ext").Inc()
		return nil, nil, result, nil
	}

	source, readErr := os.ReadFile(absPath)
	if readErr != nil {
		// Permanent IO error (permission denied, unreadable mount, etc).
		// This error is not transient: retrying the same commit will hit the
		// same result. Treat as a permanent skip so SHA can advance rather than
		// freezing the repo forever.
		//
		// DELIBERATE DECISION: skip + advance SHA, not block. Read errors on a
		// local bind mount are rare; the read_error counter (incremented below)
		// provides visibility. If the indexed filesystem is networked or flaky,
		// reconsider: a transient-vs-permanent classifier would be needed.
		//
		// Contrast: transient embed-server failures are returned from embedAndUpsert
		// (later in IndexFile), appended to IncrementalSyncResult.Errors by the
		// caller (IncrementalSync), and correctly block SHA advance.
		slog.Warn("indexFile: permanent read error — skipping file",
			slog.String("repo", repoKey),
			slog.String("file", relPath),
			slog.Any("error", readErr))
		incrementalFilesUnsupportedTotal.WithLabelValues("read_error").Inc()
		return nil, nil, result, nil
	}

	pr, parseErr := parser.ParseFile(absPath, source, parser.ParseOpts{
		Language:    lang,
		IncludeBody: true,
	})
	if parseErr != nil {
		return nil, nil, nil, fmt.Errorf("indexFile: parse %s: %w", relPath, parseErr)
	}

	// Build current symbol map: name → {sym, hash, embedText} for embeddable
	// kinds (functions, methods, AND type-level symbols: class/interface/trait/
	// struct/enum/type). The same predicate feeds currentNames (the keep-list
	// for orphan reconciliation) so widening the set does not cause add-then-
	// delete churn on re-index: a type symbol that is now embedded stays in the
	// keep-list on the next pass and is hash-skipped, never orphan-deleted.
	//
	// Keying is name-only to match the code_embeddings PRIMARY KEY
	// (repo_key, file_path, symbol_name); diverging to (name+start_line) would
	// decouple the in-memory map from the DB identity and cause perpetual churn
	// (DB reports 1 row per name, map expects N → re-embed every pass). The
	// cost is that two embeddable symbols sharing a name in one file (e.g. a
	// Java class `Foo` and its constructor method `Foo`) collapse to one row —
	// a pre-existing limitation of the name-only PK, not introduced here.
	type currentEntry struct {
		sym       *parser.Symbol
		hash      uint64
		embedText string
	}
	current := make(map[string]currentEntry, len(pr.Symbols))
	for _, sym := range pr.Symbols {
		if !parser.IsEmbeddableKind(sym.Kind) {
			continue
		}
		et := buildEmbedText(sym, relPath)
		current[sym.Name] = currentEntry{sym: sym, hash: strutil.TextHash(et), embedText: et}
	}

	// Fetch current DB state and build name→hash lookup.
	existing, fetchErr := p.store.GetSymbolsForFile(ctx, repoKey, relPath)
	if fetchErr != nil {
		return nil, nil, nil, fmt.Errorf("indexFile: fetch existing: %w", fetchErr)
	}
	dbHashes := make(map[string]uint64, len(existing))
	for _, si := range existing {
		dbHashes[si.SymbolName] = si.BodyHash
	}

	// Diff: classify each current symbol as embed (new/changed) or skip.
	fileRef := &ingest.File{RelPath: relPath, Language: lang}
	currentNames = make([]string, 0, len(current))
	for name, ce := range current {
		currentNames = append(currentNames, name)
		if dbHash, inDB := dbHashes[name]; inDB && dbHash == ce.hash {
			result.Skipped++
			continue
		}
		toEmbed = append(toEmbed, symbolEntry{
			sym:       ce.sym,
			file:      fileRef,
			hash:      ce.hash,
			embedText: ce.embedText,
		})
	}
	return toEmbed, currentNames, result, nil
}
