package embeddings

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	kitcache "github.com/anatolykoptev/go-kit/cache"

	"github.com/anatolykoptev/vaelor/internal/ingest"
	"github.com/anatolykoptev/vaelor/internal/parser"
	"github.com/anatolykoptev/vaelor/internal/strutil"
)

// Per-file cache scope: when fileCache is nil the pipeline falls back to the
// classic single-shot collectSymbols path (byte-identical to v0.32.0).
//
// Encoding: gob. Compact, internal-only payload, struct-shape evolves with
// parser.Symbol; JSON would inflate ~3× and force an external schema. Codec
// lives in pipeline_cache_codec.go.
//
// Cache key: kitcache.Key("embed_pipeline", repoKey, file.RelPath). repoKey is
// included so the same RelPath in two different repos cannot collide.
//
// Validator: re-stat the file on every L1 hit, compare modTime+size against
// the sidecar header (cachedFileSymbols.ModTime / .Size). On any drift the
// validator returns false, kitcache evicts the entry, and we fall through to
// the parse path. This caps the blast radius even if a producer renames a
// file under a stable path.
//
// Metrics: published via go-kit cache.WithMetrics (v0.33.0+). Standard names:
//
//	gokit_cache_hits_total{cache="embed_pipeline",tier="L1"}
//	gokit_cache_misses_total{cache="embed_pipeline",tier="L1"}
//	gokit_cache_evictions_total{cache="embed_pipeline"}
//	gokit_cache_size{cache="embed_pipeline"}
//
// NOTE: dashboards that tracked the old hand-rolled counter
// gokit_cache_hit_total{cache="embed_pipeline",result="hit|miss"}
// must be updated to the new names above (plural "hits"/"misses", no
// "result" label; tier label added for L1/L2 split).

const cacheLabelEmbedPipeline = "embed_pipeline"

// collectSymbolsCached returns (symbols, files) like collectSymbols but uses
// the optional per-file cache when available. When p.fileCache is nil the
// behavior is identical to the v0.32.0 collectSymbols implementation.
func (p *Pipeline) collectSymbolsCached(
	ctx context.Context, repoKey, root string,
) ([]*parser.Symbol, []*ingest.File, error) {
	if p.fileCache == nil {
		return collectSymbols(ctx, root)
	}

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
		entries, ok := p.lookupCachedEntries(ctx, repoKey, f)
		if ok {
			for i := range entries {
				symbols = append(symbols, entries[i].sym)
				files = append(files, entries[i].file)
			}
			continue
		}

		built, err := p.buildSymbolEntriesForFile(f)
		if err != nil {
			// buildSymbolEntriesForFile already logs at debug; skip the file.
			continue
		}
		p.storeCachedEntries(ctx, repoKey, f, built)
		for i := range built {
			symbols = append(symbols, built[i].sym)
			files = append(files, built[i].file)
		}
	}

	return symbols, files, nil
}

// buildSymbolEntriesForFile is the per-file equivalent of the inner loop in
// the legacy collectSymbols: read source, parse, filter to function/method
// kinds, and pair each symbol with its precomputed embedText + hash.
func (p *Pipeline) buildSymbolEntriesForFile(f *ingest.File) ([]symbolEntry, error) {
	source, err := os.ReadFile(f.Path)
	if err != nil {
		slog.Debug("embeddings: read failed", slog.String("file", f.Path), slog.Any("error", err))
		return nil, err
	}
	pr, err := parser.ParseFile(f.Path, source, parser.ParseOpts{
		Language:    f.Language,
		IncludeBody: true,
	})
	if err != nil {
		slog.Debug("embeddings: parse failed", slog.String("file", f.Path), slog.Any("error", err))
		return nil, err
	}
	out := make([]symbolEntry, 0, len(pr.Symbols))
	for _, sym := range pr.Symbols {
		if sym.Kind != parser.KindFunction && sym.Kind != parser.KindMethod {
			continue
		}
		text := buildEmbedText(sym, f.RelPath)
		out = append(out, symbolEntry{
			sym:       sym,
			file:      f,
			hash:      strutil.TextHash(text),
			embedText: text,
		})
	}
	return out, nil
}

// lookupCachedEntries fetches a previously cached entry slice for f using the
// kitcache GetIfValid hook + a modTime+size validator. Returns (entries, true)
// on a fresh hit, (nil, false) on a miss or stale entry.
func (p *Pipeline) lookupCachedEntries(
	ctx context.Context, repoKey string, f *ingest.File,
) ([]symbolEntry, bool) {
	key := pipelineCacheKey(repoKey, f.RelPath)
	validator := func(cached []byte) bool {
		info, err := os.Stat(f.Path)
		if err != nil {
			return false
		}
		header, err := decodePayload(cached)
		if err != nil {
			return false
		}
		return header.ModTime == info.ModTime().UnixNano() && header.Size == info.Size()
	}
	data, ok := p.fileCache.GetIfValid(ctx, key, validator)
	if !ok {
		return nil, false
	}
	payload, err := decodePayload(data)
	if err != nil {
		slog.Debug("embeddings: cache decode failed",
			slog.String("file", f.RelPath), slog.Any("error", err))
		return nil, false
	}
	return inflateEntries(payload, f), true
}

// storeCachedEntries persists entries under the per-file cache key. Stale
// metadata (modTime/size at the moment of write) is embedded into the payload
// so the validator can detect drift on subsequent reads.
func (p *Pipeline) storeCachedEntries(
	ctx context.Context, repoKey string, f *ingest.File, entries []symbolEntry,
) {
	payload := cachedFileSymbols{
		ModTime: f.ModTime.UnixNano(),
		Size:    f.Size,
		Entries: deflateEntries(entries),
	}
	buf, err := encodePayload(payload)
	if err != nil {
		slog.Debug("embeddings: cache encode failed",
			slog.String("file", f.RelPath), slog.Any("error", err))
		return
	}
	p.fileCache.Set(ctx, pipelineCacheKey(repoKey, f.RelPath), buf)
}

// pipelineCacheKey is the deterministic key shared by lookup + store paths.
// Stream 4 plan calls for kitcache.Key("embed", file.RelPath); we extend with
// the repoKey so two repos sharing a RelPath cannot collide.
func pipelineCacheKey(repoKey, relPath string) string {
	return kitcache.Key("embed_pipeline", repoKey, relPath)
}

// NewPipelineCache constructs a *kitcache.Cache pre-tuned for the embed
// pipeline (15-minute L1 TTL, modest item ceiling). Lives here rather than in
// register.go so wiring code stays free of cache-policy details.
//
// Prometheus metrics are published via go-kit cache.WithMetrics using
// prometheus.DefaultRegisterer and the "embed_pipeline" cache name.
func NewPipelineCache() *kitcache.Cache {
	return kitcache.New(kitcache.Config{
		L1MaxItems:    1024,
		L1TTL:         15 * time.Minute,
		JitterPercent: 0.1,
		Metrics:       kitcache.WithMetrics(prometheus.DefaultRegisterer, cacheLabelEmbedPipeline),
	})
}
