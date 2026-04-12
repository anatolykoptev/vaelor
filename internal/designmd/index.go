// internal/designmd/index.go
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

const (
	RepoKey      = "design-md"
	maxEmbedText = 2000
)

type IndexResult struct {
	Brands  int
	Indexed int
	Skipped int
}

// Index reads all */DESIGN.md files under dir, embeds sections, and upserts to pgvector.
// Writes index.json with per-brand metadata next to dir.
func Index(ctx context.Context, dir string, client *embeddings.Client, store *embeddings.Store) (*IndexResult, error) {
	pattern := filepath.Join(dir, "*", "DESIGN.md")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("glob: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no DESIGN.md files found in %s", dir)
	}

	existing, err := store.GetHashes(ctx, RepoKey)
	if err != nil {
		return nil, fmt.Errorf("get hashes: %w", err)
	}

	var result IndexResult
	metaIndex := make(map[string]BrandMeta)
	var records []embeddings.EmbeddingRecord
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

		sections := SplitSections(string(content))
		for _, sec := range sections {
			relPath := brand + "/DESIGN.md"
			embedText := fmt.Sprintf("%s %s: %s", brand, sec.Title, sec.Body)
			if len(embedText) > maxEmbedText {
				embedText = embedText[:maxEmbedText]
			}

			h := textHash(embedText)
			key := relPath + ":" + sec.Title
			if prev, ok := existing[key]; ok && prev == h {
				result.Skipped++
				continue
			}

			records = append(records, embeddings.EmbeddingRecord{
				RepoKey:    RepoKey,
				FilePath:   relPath,
				SymbolName: sec.Title,
				SymbolKind: "design-section",
				Language:   "markdown",
				StartLine:  sec.StartLine,
				BodyHash:   h,
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

	metaPath := filepath.Join(dir, "index.json")
	data, err := json.MarshalIndent(metaIndex, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal meta: %w", err)
	}
	if err := os.WriteFile(metaPath, data, 0o644); err != nil {
		return nil, fmt.Errorf("write index.json: %w", err)
	}
	slog.Info("designmd: index.json written", slog.String("path", metaPath))

	return &result, nil
}

func textHash(text string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(text))
	return h.Sum64()
}
