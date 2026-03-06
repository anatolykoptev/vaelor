package embeddings

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

const (
	maxEmbedText   = 2000
	maxIndexFileKB = 512 * 1024
)

// Pipeline orchestrates embedding indexing for repository symbols.
type Pipeline struct {
	client  *Client
	store   *Store
	running sync.Map // repoKey -> bool (true = indexing in progress)
}

// NewPipeline creates a Pipeline backed by the given client and store.
func NewPipeline(client *Client, store *Store) *Pipeline {
	return &Pipeline{client: client, store: store}
}

// IsIndexing returns true if background indexing is running for the given repo.
func (p *Pipeline) IsIndexing(repoKey string) bool {
	v, ok := p.running.Load(repoKey)
	return ok && v.(bool)
}

// IndexRepoAsync starts background indexing if not already running.
// Returns true if indexing was started, false if already in progress.
func (p *Pipeline) IndexRepoAsync(repoKey, root string) bool {
	if _, loaded := p.running.LoadOrStore(repoKey, true); loaded {
		return false
	}
	go func() {
		defer p.running.Delete(repoKey)
		ctx := context.Background()
		result, err := p.IndexRepo(ctx, repoKey, root)
		if err != nil {
			slog.Error("background index failed", slog.String("repo", repoKey), slog.Any("error", err))
			return
		}
		slog.Info("background index complete",
			slog.String("repo", repoKey),
			slog.Int("indexed", result.Indexed),
			slog.Int("skipped", result.Skipped),
			slog.Int("total", result.Total))
	}()
	return true
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
	seen := make(map[string]bool) // dedup within batch
	for i, sym := range symbols {
		key := files[i].RelPath + ":" + sym.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		h := bodyHash(sym.Body)
		if prev, ok := existing[key]; ok && prev == h {
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

