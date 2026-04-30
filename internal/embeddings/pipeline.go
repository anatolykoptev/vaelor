package embeddings

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"

	kitcache "github.com/anatolykoptev/go-kit/cache"
	"github.com/anatolykoptev/go-kit/embed"

	"github.com/anatolykoptev/go-code/internal/ingest"
	"github.com/anatolykoptev/go-code/internal/parser"
)

const (
	maxEmbedText      = 2000
	maxIndexFileBytes = 512 * 1024
	indexChunkSize    = 100
)

// indexProgress tracks the progress of a background indexing run.
type indexProgress struct {
	total   int64
	done    int64
	running bool
}

// Pipeline orchestrates embedding indexing for repository symbols.
type Pipeline struct {
	client    *embed.Client
	store     *Store
	progress  sync.Map // repoKey -> *indexProgress
	fileCache *kitcache.Cache // optional per-file symbol-entry cache; nil disables.
}

// NewPipeline creates a Pipeline backed by the given client and store.
//
// Pass a non-nil fileCache via WithFileCache to enable per-file symbol-entry
// caching keyed on (repoKey, file.RelPath) and validated by file modTime+size.
// When fileCache is nil, behavior is byte-identical to the v0.32.0 baseline.
func NewPipeline(client *embed.Client, store *Store, opts ...PipelineOpt) *Pipeline {
	p := &Pipeline{client: client, store: store}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// PipelineOpt configures a Pipeline at construction time.
type PipelineOpt func(*Pipeline)

// WithFileCache wires a *kitcache.Cache to memoize per-file symbol entries.
// Validator (modTime+size) ensures stale entries are evicted on the next
// indexRepo pass after a file is touched. Pass nil to disable explicitly.
func WithFileCache(c *kitcache.Cache) PipelineOpt {
	return func(p *Pipeline) { p.fileCache = c }
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
		defer func() {
			prog.running = false
			p.progress.Delete(repoKey)
		}()
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
	// Fast path: skip the entire parse + embed cycle when the repo's main
	// branch has not moved since the last successful index. Cuts boot-time
	// embed-server load from "48 repos × N symbols" to zero for unchanged
	// repos. A repo with no main/master/HEAD ref (non-git path) returns
	// sha="" and falls through to the full path.
	currentSHA, _ := repoMainBranchSHA(root)
	if currentSHA != "" {
		prevSHA, err := p.store.GetRepoState(ctx, repoKey)
		if err == nil && prevSHA == currentSHA {
			slog.Debug("indexRepo: skip — main branch unchanged",
				slog.String("repo", repoKey),
				slog.String("sha", currentSHA[:min(8, len(currentSHA))]))
			return &IndexResult{Total: 0, Indexed: 0, Skipped: 0}, nil
		}
	}

	symbols, files, err := p.collectSymbolsCached(ctx, repoKey, root)
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
		embedText := buildEmbedText(sym, files[i].RelPath)
		h := textHash(embedText)
		if prev, ok := existing[key]; ok && prev == h {
			result.Skipped++
			continue
		}
		toEmbed = append(toEmbed, symbolEntry{sym: sym, file: files[i], hash: h, embedText: embedText})
	}

	if prog != nil {
		atomic.StoreInt64(&prog.total, int64(len(toEmbed)))
	}

	if len(toEmbed) == 0 {
		// Even the no-embed path advances the repo fingerprint so the next
		// boot can short-circuit. Without this we'd fall back to the parse
		// path forever on stable repos.
		if currentSHA != "" {
			if err := p.store.SetRepoState(ctx, repoKey, currentSHA); err != nil {
				slog.Debug("indexRepo: SetRepoState failed",
					slog.String("repo", repoKey), slog.Any("error", err))
			}
		}
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

	if currentSHA != "" {
		if err := p.store.SetRepoState(ctx, repoKey, currentSHA); err != nil {
			slog.Debug("indexRepo: SetRepoState failed",
				slog.String("repo", repoKey), slog.Any("error", err))
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
		texts[i] = e.embedText
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

// symbolEntry pairs a symbol with its source file, precomputed hash, and embed text.
type symbolEntry struct {
	sym       *parser.Symbol
	file      *ingest.File
	hash      uint64
	embedText string
}
