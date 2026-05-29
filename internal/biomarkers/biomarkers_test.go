package biomarkers

import (
	"context"
	"testing"
)

type fakeBM struct{ name string; score float64 }

func (f fakeBM) Name() string                                                       { return f.name }
func (f fakeBM) Score(_ context.Context, _ string, _ string) (float64, string, error) {
	return f.score, "fake reason", nil
}

func TestRegistry_RegisterAndList(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeBM{name: "x", score: 0.5})
	r.Register(fakeBM{name: "y", score: 0.8})
	got := r.Names()
	if len(got) != 2 || got[0] != "x" || got[1] != "y" {
		t.Fatalf("Names: got %v", got)
	}
}

func TestRegistry_DuplicateNamePanics(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeBM{name: "x"})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate biomarker name")
		}
	}()
	r.Register(fakeBM{name: "x"})
}
