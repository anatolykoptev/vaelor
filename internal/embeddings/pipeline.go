package embeddings

import (
	"context"
	"fmt"
	"hash/fnv"
	"log/slog"
	"os"

	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

const (
	maxEmbedText   = 2000
	maxIndexFileKB = 512 * 1024
)

// Pipeline orchestrates embedding indexing for repository symbols.
type Pipeline struct {
	client *Client
	store  *Store
}

// NewPipeline creates a Pipeline backed by the given client and store.
func NewPipeline(client *Client, store *Store) *Pipeline {
	return &Pipeline{client: client, store: store}
}

// IndexResult summarizes the outcome of an indexing run.
type IndexResult struct {
	Indexed int
	Skipped int
	Total   int
}

// IndexRepo indexes all functions and methods in a repository for semantic search.
func (p *Pipeline) IndexRepo(ctx context.Context, repoKey, root string) (*IndexResult, error) {
	symbols, files, err := collectSymbols(ctx, root)
	if err != nil {
		return nil, err
	}

	result := &IndexResult{Total: len(symbols)}

	existing, err := p.store.GetHashes(ctx, repoKey)
	if err != nil {
		return nil, fmt.Errorf("get existing hashes: %w", err)
	}

	var toEmbed []symbolEntry
	for i, sym := range symbols {
		h := bodyHash(sym.Body)
		if prev, ok := existing[sym.Name]; ok && prev == h {
			result.Skipped++
			continue
		}
		toEmbed = append(toEmbed, symbolEntry{sym: sym, file: files[i], hash: h})
	}

	if len(toEmbed) == 0 {
		result.Indexed = 0
		return result, nil
	}

	texts := make([]string, len(toEmbed))
	for i, e := range toEmbed {
		texts[i] = buildEmbedText(e.sym)
	}

	vectors, err := p.client.Embed(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("embed symbols: %w", err)
	}

	records := make([]EmbeddingRecord, len(toEmbed))
	for i, e := range toEmbed {
		records[i] = EmbeddingRecord{
			RepoKey:    repoKey,
			FilePath:   e.file.RelPath,
			SymbolName: e.sym.Name,
			SymbolKind: string(e.sym.Kind),
			Language:   e.sym.Language,
			StartLine:  int(e.sym.StartLine),
			BodyHash:   e.hash,
			Embedding:  vectors[i],
		}
	}

	if err := p.store.Upsert(ctx, records); err != nil {
		return nil, fmt.Errorf("upsert embeddings: %w", err)
	}

	result.Indexed = len(toEmbed)
	return result, nil
}

// symbolEntry pairs a symbol with its source file and precomputed hash.
type symbolEntry struct {
	sym  *parser.Symbol
	file *ingest.File
	hash uint64
}

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

// buildEmbedText formats a symbol for embedding, truncated to maxEmbedText chars.
func buildEmbedText(sym *parser.Symbol) string {
	text := fmt.Sprintf("%s %s %s: %s\n%s", sym.Language, sym.Kind, sym.Name, sym.Signature, sym.Body)
	if len(text) > maxEmbedText {
		return text[:maxEmbedText]
	}
	return text
}

// bodyHash computes an FNV-64a hash of the symbol body for change detection.
func bodyHash(body string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(body))
	return h.Sum64()
}

// GetHashes returns a map of symbol_name -> body_hash for all embeddings in a repo.
func (s *Store) GetHashes(ctx context.Context, repoKey string) (map[string]uint64, error) {
	if err := s.EnsureSchema(ctx); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx,
		"SELECT symbol_name, body_hash FROM code_embeddings WHERE repo_key=$1", repoKey)
	if err != nil {
		return nil, fmt.Errorf("query hashes: %w", err)
	}
	defer rows.Close()

	result := make(map[string]uint64)
	for rows.Next() {
		var name string
		var hash int64
		if err := rows.Scan(&name, &hash); err != nil {
			return nil, fmt.Errorf("scan hash: %w", err)
		}
		result[name] = uint64(hash)
	}
	return result, rows.Err()
}
