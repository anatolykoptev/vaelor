package compare

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestComputeRelStats(t *testing.T) {
	rels := []parser.TypeRelationship{
		{Subject: "Dog", Target: "Animal", Kind: parser.RelExtends},
		{Subject: "Cat", Target: "Animal", Kind: parser.RelExtends},
		{Subject: "Dog", Target: "Runnable", Kind: parser.RelImplements},
		{Subject: "MyReader", Target: "Reader", Kind: parser.RelEmbeds},
	}

	stats := ComputeRelStats(rels)

	if stats.Total != 4 {
		t.Errorf("Total = %d, want 4", stats.Total)
	}
	if stats.Extends != 2 {
		t.Errorf("Extends = %d, want 2", stats.Extends)
	}
	if stats.Implements != 1 {
		t.Errorf("Implements = %d, want 1", stats.Implements)
	}
	if stats.Embeds != 1 {
		t.Errorf("Embeds = %d, want 1", stats.Embeds)
	}
	if stats.UniqueSubjects != 3 {
		t.Errorf("UniqueSubjects = %d, want 3", stats.UniqueSubjects)
	}
}

func TestComputeRelStats_Empty(t *testing.T) {
	stats := ComputeRelStats(nil)
	if stats != nil {
		t.Error("expected nil for empty rels")
	}
}
