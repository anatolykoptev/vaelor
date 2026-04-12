package designmd

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/anatolykoptev/go-code/internal/embeddings"
)

const maxEmbedText = 2000

// IndexResult summarizes an indexing run.
type IndexResult struct {
	Brands  int
	Indexed int
	Skipped int
}

// Index reads all */DESIGN.md files under dir, embeds sections via e5-large,
// and upserts to the design_embeddings table (1024-dim).
// Writes index.json with per-brand metadata.
func Index(ctx context.Context, dir string, client *embeddings.Client, store *Store) (*IndexResult, error) {
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

			h := textHash(embedText)
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
		vectors, err := client.Embed(ctx, texts)
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

func textHash(text string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(text))
	return h.Sum64()
}
