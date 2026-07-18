package polyglot

import (
	"testing"

	"github.com/anatolykoptev/vaelor/internal/ingest"
)

func TestDetectedLanguages_MultiLang(t *testing.T) {
	t.Parallel()
	files := []*ingest.File{
		{Language: "rust"},
		{Language: "rust"},
		{Language: "rust"},
		{Language: "rust"},
		{Language: "typescript"},
		{Language: "typescript"},
		{Language: "typescript"},
		{Language: "python"},
	}
	langs := DetectedLanguages(files)
	if len(langs) != 2 {
		t.Fatalf("expected 2 languages (rust, typescript), got %d: %v", len(langs), langs)
	}
	if langs[0] != "rust" {
		t.Errorf("expected rust first (most files), got %s", langs[0])
	}
	if langs[1] != "typescript" {
		t.Errorf("expected typescript second, got %s", langs[1])
	}
}

func TestDetectedLanguages_FiltersSmallLangs(t *testing.T) {
	t.Parallel()
	files := []*ingest.File{
		{Language: "rust"},
		{Language: "rust"},
		{Language: "rust"},
		{Language: "rust"},
		{Language: "rust"},
		{Language: "python"},
		{Language: "python"},
	}
	langs := DetectedLanguages(files)
	if len(langs) != 1 {
		t.Fatalf("expected 1 language (python below threshold), got %d: %v", len(langs), langs)
	}
	if langs[0] != "rust" {
		t.Errorf("expected rust, got %s", langs[0])
	}
}

func TestDetectedLanguages_Empty(t *testing.T) {
	t.Parallel()
	langs := DetectedLanguages(nil)
	if len(langs) != 0 {
		t.Errorf("expected 0 languages for empty input, got %v", langs)
	}
}
