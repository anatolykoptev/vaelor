package embeddings

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

const (
	maxEmbedText   = 2000
	maxIndexFileKB = 512 * 1024
	indexChunkSize = 100
)

// indexProgress tracks the progress of a background indexing run.
type indexProgress struct {
	total   int64
	done    int64
	running bool
}

// Pipeline orchestrates embedding indexing for repository symbols.
type Pipeline struct {
	client   *Client
	store    *Store
	progress sync.Map // repoKey -> *indexProgress
}

// NewPipeline creates a Pipeline backed by the given client and store.
func NewPipeline(client *Client, store *Store) *Pipeline {
	return &Pipeline{client: client, store: store}
}

// IsIndexing returns true if background indexing is running for the given repo.
func (p *Pipeline) IsIndexing(repoKey string) bool {
	v, ok := p.progress.Load(repoKey)
	if !ok {
		return false
	}
	return v.(*indexProgress).running
}

// IndexProgress returns (done, total, running) for the given repo.
func (p *Pipeline) IndexProgress(repoKey string) (done, total int, running bool) {
	v, ok := p.progress.Load(repoKey)
	if !ok {
		return 0, 0, false
	}
	prog := v.(*indexProgress)
	return int(atomic.LoadInt64(&prog.done)), int(atomic.LoadInt64(&prog.total)), prog.running
}

// IndexRepoAsync starts background indexing if not already running.
// Returns true if indexing was started, false if already in progress.
func (p *Pipeline) IndexRepoAsync(repoKey, root string) bool {
	if v, loaded := p.progress.Load(repoKey); loaded {
		if v.(*indexProgress).running {
			return false
		}
	}
	prog := &indexProgress{running: true}
	p.progress.Store(repoKey, prog)
	go func() {
		defer func() { prog.running = false }()
		ctx := context.Background()
		result, err := p.indexRepo(ctx, repoKey, root, prog)
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
	return p.indexRepo(ctx, repoKey, root, nil)
}

// indexRepo is the internal implementation that optionally reports progress.
func (p *Pipeline) indexRepo(
	ctx context.Context, repoKey, root string, prog *indexProgress,
) (*IndexResult, error) {
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

	if prog != nil {
		atomic.StoreInt64(&prog.total, int64(len(toEmbed)))
	}

	if len(toEmbed) == 0 {
		return result, nil
	}

	// Process in chunks to avoid OOM and show progress.
	for start := 0; start < len(toEmbed); start += indexChunkSize {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		end := start + indexChunkSize
		if end > len(toEmbed) {
			end = len(toEmbed)
		}
		chunk := toEmbed[start:end]

		indexed, err := p.embedAndUpsert(ctx, repoKey, chunk)
		if err != nil {
			return nil, err
		}
		result.Indexed += indexed

		if prog != nil {
			atomic.StoreInt64(&prog.done, int64(end))
		}
	}

	return result, nil
}

// embedAndUpsert embeds a chunk of symbols and upserts them into the store.
func (p *Pipeline) embedAndUpsert(
	ctx context.Context, repoKey string, chunk []symbolEntry,
) (int, error) {
	texts := make([]string, len(chunk))
	for i, e := range chunk {
		texts[i] = buildEmbedText(e.sym, e.file.RelPath)
	}

	vectors, err := p.client.Embed(ctx, texts)
	if err != nil {
		return 0, fmt.Errorf("embed symbols: %w", err)
	}

	records := make([]EmbeddingRecord, len(chunk))
	for i, e := range chunk {
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
		return 0, fmt.Errorf("upsert embeddings: %w", err)
	}

	return len(chunk), nil
}

// symbolEntry pairs a symbol with its source file and precomputed hash.
type symbolEntry struct {
	sym  *parser.Symbol
	file *ingest.File
	hash uint64
}
