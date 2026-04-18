package learnings

import (
	"context"
	"os"
	"testing"
)

func TestStoreRoundtrip(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	s, err := New(context.Background(), dsn, nil /* embedder */)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.Close)

	rec := Record{Repo: "foo/bar", Symbol: "Svc.DoThing", Verdict: "medium", Flag: "policy:forbidden_import", Note: "use stdlib", PRURL: "https://x/1"}
	if err := s.Upsert(context.Background(), rec); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := s.Nearest(context.Background(), "foo/bar", "Svc.DoThing", 3)
	if err != nil {
		t.Fatalf("Nearest: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected at least 1 record")
	}
}


// fakeEmbedder returns pre-seeded embeddings for known inputs. Unknown inputs
// fall through to the default vector so callers still get a non-empty result.
type fakeEmbedder struct {
	vecs       map[string][]float32
	defaultVec []float32
}

func (f *fakeEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	if v, ok := f.vecs[text]; ok {
		return v, nil
	}
	return f.defaultVec, nil
}

// unitVector builds a 768-dim vector with 1.0 at the given index and zeros
// elsewhere. Cosine distance between any two such vectors is 1 unless they
// share the same index (distance 0).
func unitVector(idx int) []float32 {
	v := make([]float32, 768)
	v[idx] = 1
	return v
}

func TestStore_NearestByVector(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping pgvector integration test")
	}

	ctx := context.Background()
	const tag = "__test_vec__"

	// Seed embeddings: key them by the exact text Upsert will embed (flag+": "+note).
	emb := &fakeEmbedder{
		vecs: map[string][]float32{
			"flag-a: note-a":   unitVector(0),
			"flag-b: note-b":   unitVector(1),
			"flag-c: note-c":   unitVector(2),
			"query for flag-b": unitVector(1),
		},
		defaultVec: unitVector(100),
	}

	s, err := New(ctx, dsn, emb)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.Close)

	// Clean any leaked rows from prior runs, and clean up after ourselves.
	cleanup := func() {
		if _, err := s.pool.Exec(ctx, "DELETE FROM review_learnings WHERE repo = $1", tag); err != nil {
			t.Logf("cleanup delete: %v", err)
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	seeds := []Record{
		{Repo: tag, Symbol: "pkg.A", Verdict: "good", Flag: "flag-a", Note: "note-a"},
		{Repo: tag, Symbol: "pkg.B", Verdict: "neutral", Flag: "flag-b", Note: "note-b"},
		{Repo: tag, Symbol: "pkg.C", Verdict: "bad", Flag: "flag-c", Note: "note-c"},
	}
	for _, r := range seeds {
		if err := s.Upsert(ctx, r); err != nil {
			t.Fatalf("Upsert %q: %v", r.Symbol, err)
		}
	}

	// Query vector is equal to seed B's vector, so B must come first.
	got, err := s.NearestVector(ctx, "query for flag-b", 3)
	if err != nil {
		t.Fatalf("NearestVector: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected at least 1 record")
	}

	// Filter to our tagged rows (other tests may have leaked rows).
	var ours []Record
	for _, r := range got {
		if r.Repo == tag {
			ours = append(ours, r)
		}
	}
	if len(ours) == 0 {
		t.Fatalf("no tagged rows returned; got %+v", got)
	}
	if ours[0].Symbol != "pkg.B" {
		t.Fatalf("expected closest to be pkg.B, got %q (full: %+v)", ours[0].Symbol, ours)
	}
}

func TestStore_NearestVectorNoEmbedder(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping pgvector integration test")
	}
	s, err := New(context.Background(), dsn, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(s.Close)
	if _, err := s.NearestVector(context.Background(), "anything", 3); err == nil {
		t.Fatal("expected error when embedder is nil")
	}
}
