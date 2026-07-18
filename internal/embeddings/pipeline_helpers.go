package embeddings

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/langutil"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// rootHasEmbeddableFiles returns true when root contains at least one file with a
// parser-supported extension (i.e. the repo is a code repo, not docs-only).
//
// This is an early-exit walk — it stops at the first matching file. It is used to
// distinguish real desync (code repo with 0 stored rows) from a docs-only repo
// (e.g. /host/src/wiki which has .md files only) that legitimately produces 0
// embeddings. Docs-only repos must not trip the gocode_repo_state_advanced_with_zero_embeddings_total
// counter on every boot.
//
// Walk errors (permission denied, broken symlinks) are silently skipped so this
// function is always safe to call and never blocks the recovery path.
func rootHasEmbeddableFiles(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if parser.HandlerForExt(filepath.Ext(path)) != nil {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

// collectSymbols ingests a repo and parses all files, returning only functions and methods.
func collectSymbols(ctx context.Context, root string) ([]*parser.Symbol, []*ingest.File, error) {
	ir, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         root,
		MaxFileBytes: maxIndexFileBytes,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("ingest repo: %w", err)
	}

	var symbols []*parser.Symbol
	var files []*ingest.File

	for _, f := range ir.Files {
		if isTestFile(f.RelPath) {
			continue
		}
		source, err := os.ReadFile(f.Path)
		if err != nil {
			slog.Debug("embeddings: read failed", slog.String("file", f.Path), slog.Any("error", err))
			continue
		}
		pr, err := parser.ParseFile(f.Path, source, parser.ParseOpts{
			Language:    f.Language,
			IncludeBody: true,
		})
		if err != nil {
			slog.Debug("embeddings: parse failed", slog.String("file", f.Path), slog.Any("error", err))
			continue
		}
		for _, sym := range pr.Symbols {
			if sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod {
				continue
			}
			symbols = append(symbols, sym)
			files = append(files, f)
		}
	}

	return symbols, files, nil
}

// isTestFile returns true for test/spec files that should be excluded from indexing.
func isTestFile(relPath string) bool {
	return langutil.IsTestFile(relPath)
}

// buildEmbedText formats a symbol for embedding with file path context.
// Includes the doc comment (if present) between signature and body to
// improve NL-query MRR (CodeSearchNet: ~2× improvement).
// Truncates at line boundary within maxEmbedText chars.
func buildEmbedText(sym *parser.Symbol, filePath string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s %s %s: %s\n", filePath, sym.Language, sym.Kind, sym.Name, sym.Signature)
	if doc := strings.TrimSpace(sym.DocComment); doc != "" {
		sb.WriteString(doc)
		sb.WriteString("\n")
	}
	header := sb.String()
	remaining := maxEmbedText - len(header)
	if remaining <= 0 {
		return header[:maxEmbedText]
	}
	body := sym.Body
	if len(body) > remaining {
		cut := strings.LastIndex(body[:remaining], "\n")
		if cut > 0 {
			body = body[:cut+1]
		} else {
			body = body[:remaining]
		}
	}
	return header + body
}

// GetHashes returns a map of symbol_name -> body_hash for all embeddings in a repo.
func (s *Store) GetHashes(ctx context.Context, repoKey string) (map[string]uint64, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx,
		"SELECT file_path, symbol_name, body_hash FROM public.code_embeddings WHERE repo_key=$1", repoKey)
	if err != nil {
		return nil, fmt.Errorf("query hashes: %w", err)
	}
	defer rows.Close()

	result := make(map[string]uint64)
	for rows.Next() {
		var file, name string
		var hash int64
		if err := rows.Scan(&file, &name, &hash); err != nil {
			return nil, fmt.Errorf("scan hash: %w", err)
		}
		result[file+symKeySep+name] = uint64(hash)
	}
	return result, rows.Err()
}
