package designmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/anatolykoptev/go-kit/embed"

	"github.com/anatolykoptev/vaelor/internal/strutil"
)

const maxEmbedText = 2000

// embedScheme identifies the document embedding scheme folded into body_hash.
// Rows stamped under a different scheme — e.g. the un-prefixed e5 rows written
// before instruction prefixes — no longer match and are re-embedded on the
// next index-designs run. Bump the version when the wire format changes
// independently of the prefix.
const embedScheme = "v1:" + embed.E5PassagePrefix

// SectionStore is the persistence seam Index needs: existing body hashes for
// skip detection plus batched upserts of freshly embedded sections.
// *Store implements it; tests substitute a fake.
type SectionStore interface {
	GetHashes(ctx context.Context) (map[string]uint64, error)
	Upsert(ctx context.Context, records []Record) error
}

// IndexResult summarizes an indexing run.
type IndexResult struct {
	Brands  int
	Indexed int
	Skipped int
}

// Index reads all */DESIGN.md files under dir, embeds sections via e5-large
// (with the "passage: " document prefix the model was trained on), and upserts
// to the design_embeddings table (1024-dim).
// Writes index.json with per-brand metadata.
func Index(ctx context.Context, dir string, client embed.Embedder, store SectionStore) (*IndexResult, error) {
	pattern := filepath.Join(dir, "*", "DESIGN.md")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no DESIGN.md files found in %s", dir)
	}

	existing, err := store.GetHashes(ctx)
	if err != nil {
		return nil, fmt.Errorf("get hashes: %w", err)
	}

	var result IndexResult
	metaIndex := make(map[string]BrandMeta)
	var records []Record
	var texts []string

	for _, f := range files {
		brand := filepath.Base(filepath.Dir(f))
		content, err := os.ReadFile(f)
		if err != nil {
			slog.Warn("designmd: read failed", slog.String("file", f), slog.Any("error", err))
			continue
		}

		result.Brands++
		metaIndex[brand] = ExtractMeta(string(content))

		for _, sec := range SplitSections(string(content)) {
			embedText := fmt.Sprintf("%s %s: %s", brand, sec.Title, sec.Body)
			if len(embedText) > maxEmbedText {
				embedText = embedText[:maxEmbedText]
			}

			h := strutil.TextHash(embedScheme + embedText)
			key := brand + ":" + sec.Title
			if prev, ok := existing[key]; ok && prev == h {
				result.Skipped++
				continue
			}

			records = append(records, Record{
				Brand:     brand,
				Section:   sec.Title,
				FilePath:  brand + "/DESIGN.md",
				StartLine: sec.StartLine,
				BodyHash:  h,
			})
			texts = append(texts, embedText)
		}
	}

	if len(texts) > 0 {
		slog.Info("designmd: embedding sections", slog.Int("count", len(texts)))
		vectors, err := embed.Prefixed(client, embed.E5PassagePrefix).Embed(ctx, texts)
		if err != nil {
			return nil, fmt.Errorf("embed: %w", err)
		}
		for i := range records {
			records[i].Embedding = vectors[i]
		}
		if err := store.Upsert(ctx, records); err != nil {
			return nil, fmt.Errorf("upsert: %w", err)
		}
		result.Indexed = len(records)
	}

	// Write index.json metadata sidecar.
	metaPath := filepath.Join(dir, "index.json")
	data, err := json.MarshalIndent(metaIndex, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal meta: %w", err)
	}
	if err := os.WriteFile(metaPath, data, 0o644); err != nil {
		metaPath = "/tmp/go-code-output/design-md-index.json"
		if err2 := os.WriteFile(metaPath, data, 0o644); err2 != nil {
			return nil, fmt.Errorf("write index.json: %w (fallback: %w)", err, err2)
		}
	}
	slog.Info("designmd: index.json written", slog.String("path", metaPath))

	return &result, nil
}
