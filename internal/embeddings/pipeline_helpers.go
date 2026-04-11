package embeddings

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"os"
	"strings"

	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/langutil"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// collectSymbols ingests a repo and parses all files, returning only functions and methods.
func collectSymbols(ctx context.Context, root string) ([]*parser.Symbol, []*ingest.File, error) {
	ir, err := ingest.IngestRepo(ctx, ingest.IngestOpts{
		Root:         root,
		MaxFileBytes: maxIndexFileKB,
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
	sb.WriteString(fmt.Sprintf("%s %s %s %s: %s\n", filePath, sym.Language, sym.Kind, sym.Name, sym.Signature))
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

// textHash computes an FNV-64a hash of the embed text for change detection.
func textHash(text string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(text))
	return h.Sum64()
}

// GetHashes returns a map of symbol_name -> body_hash for all embeddings in a repo.
func (s *Store) GetHashes(ctx context.Context, repoKey string) (map[string]uint64, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx,
		"SELECT file_path, symbol_name, body_hash FROM code_embeddings WHERE repo_key=$1", repoKey)
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
		result[file+":"+name] = uint64(hash)
	}
	return result, rows.Err()
}
