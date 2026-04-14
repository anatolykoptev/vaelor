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
	defer s.Close()

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
