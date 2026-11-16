package designmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/go-kit/embed"

	"github.com/anatolykoptev/vaelor/internal/strutil"
)

// recordingEmbedder captures every text handed to Embed/EmbedQuery and
// returns a deterministic vector per input.
type recordingEmbedder struct {
	texts   []string
	queries []string
}

var _ embed.Embedder = (*recordingEmbedder)(nil)

func (r *recordingEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	r.texts = append(r.texts, texts...)
	out := make([][]float32, len(texts))
	for i := range out {
		out[i] = []float32{float32(i)}
	}
	return out, nil
}

func (r *recordingEmbedder) EmbedQuery(_ context.Context, text string) ([]float32, error) {
	r.queries = append(r.queries, text)
	return []float32{0}, nil
}

func (r *recordingEmbedder) Dimension() int { return 1024 }
func (r *recordingEmbedder) Close() error   { return nil }

// fakeSectionStore serves canned body hashes and records what Index upserts.
type fakeSectionStore struct {
	hashes   map[string]uint64
	upserted []Record
}

var _ SectionStore = (*fakeSectionStore)(nil)

func (f *fakeSectionStore) GetHashes(context.Context) (map[string]uint64, error) {
	return f.hashes, nil
}

func (f *fakeSectionStore) Upsert(_ context.Context, records []Record) error {
	f.upserted = append(f.upserted, records...)
	return nil
}

// designFixture is the two-section DESIGN.md the tests index. The section
// bodies below must stay in sync — the old-scheme hash in
// TestIndex_ReembedsRowsFromOldScheme is computed over the literal wire text.
func designFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	brand := filepath.Join(dir, "acme")
	if err := os.MkdirAll(brand, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "# Acme Design\n\n" +
		"## 1. Colors\nDeep navy with gold accents.\n\n" +
		"## 2. Type\nSerif headings everywhere.\n"
	if err := os.WriteFile(filepath.Join(brand, "DESIGN.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return dir
}

func TestIndex_DocumentsEmbedAsE5Passages(t *testing.T) {
	dir := designFixture(t)
	rec := &recordingEmbedder{}
	store := &fakeSectionStore{hashes: map[string]uint64{}}

	res, err := Index(context.Background(), dir, rec, store)
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	if res.Indexed != 2 {
		t.Fatalf("indexed = %d, want 2", res.Indexed)
	}

	// multilingual-e5-large was trained on "passage: "-prefixed documents; the
	// text reaching the embedder must carry that prefix.
	want := []string{
		"passage: acme Colors: ## 1. Colors\nDeep navy with gold accents.",
		"passage: acme Type: ## 2. Type\nSerif headings everywhere.",
	}
	if len(rec.texts) != len(want) {
		t.Fatalf("embedder saw %d texts, want %d: %q", len(rec.texts), len(want), rec.texts)
	}
	for i, w := range want {
		if rec.texts[i] != w {
			t.Errorf("text[%d] = %q, want %q", i, rec.texts[i], w)
		}
	}
}

// TestIndex_ReembedsRowsFromOldScheme pins the body_hash scheme: rows stamped
// under the pre-prefix scheme (plain TextHash of the embed text) must be
// re-embedded, while rows stamped under the current scheme skip.
func TestIndex_ReembedsRowsFromOldScheme(t *testing.T) {
	dir := designFixture(t)
	ctx := context.Background()

	// Pass 1: empty store embeds both sections; capture the stamped hashes.
	fresh := &fakeSectionStore{hashes: map[string]uint64{}}
	if _, err := Index(ctx, dir, &recordingEmbedder{}, fresh); err != nil {
		t.Fatalf("index pass1: %v", err)
	}
	if len(fresh.upserted) != 2 {
		t.Fatalf("upserted %d records, want 2", len(fresh.upserted))
	}
	current := make(map[string]uint64, len(fresh.upserted))
	for _, r := range fresh.upserted {
		current[r.Brand+":"+r.Section] = r.BodyHash
	}

	// Pass 2: rows stamped under the current scheme are skipped, not re-embedded.
	rec2 := &recordingEmbedder{}
	res2, err := Index(ctx, dir, rec2, &fakeSectionStore{hashes: current})
	if err != nil {
		t.Fatalf("index pass2: %v", err)
	}
	if res2.Skipped != 2 || len(rec2.texts) != 0 {
		t.Fatalf("current-scheme rows: skipped=%d embedded=%d, want skipped=2 embedded=0",
			res2.Skipped, len(rec2.texts))
	}

	// Pass 3: a row stamped under the old scheme — plain TextHash of the
	// embed text, what the indexer wrote before e5 prefixes — reads as stale
	// and is re-embedded; its current-scheme sibling still skips.
	oldScheme := map[string]uint64{
		"acme:Colors": strutil.TextHash("acme Colors: ## 1. Colors\nDeep navy with gold accents."),
		"acme:Type":   current["acme:Type"],
	}
	rec3 := &recordingEmbedder{}
	stale := &fakeSectionStore{hashes: oldScheme}
	if _, err := Index(ctx, dir, rec3, stale); err != nil {
		t.Fatalf("index pass3: %v", err)
	}
	if len(stale.upserted) != 1 || stale.upserted[0].Section != "Colors" {
		t.Fatalf("old-scheme re-embed: upserted %+v, want exactly the Colors section", stale.upserted)
	}
	if len(rec3.texts) != 1 {
		t.Fatalf("old-scheme re-embed: %d texts embedded, want 1", len(rec3.texts))
	}
}
