package compound_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/callgraph"
	"github.com/anatolykoptev/go-code/internal/compound"
	"github.com/anatolykoptev/go-code/internal/learnings"
	"github.com/anatolykoptev/go-code/internal/parser"
)

// fakeLearnings implements compound.LearningsLookup for tests.
type fakeLearnings struct {
	records []learnings.Record
	gotRepo string
	gotSym  string
	gotK    int
}

func (f *fakeLearnings) Nearest(_ context.Context, repo, symbol string, k int) ([]learnings.Record, error) {
	f.gotRepo = repo
	f.gotSym = symbol
	f.gotK = k
	return f.records, nil
}

func testSymFoo() *parser.Symbol {
	return &parser.Symbol{
		Name:      "Foo",
		Kind:      parser.KindFunction,
		File:      "foo.go",
		StartLine: 1,
		EndLine:   10,
	}
}

func testCallGraphFoo(target *parser.Symbol) *callgraph.CallGraph {
	return &callgraph.CallGraph{
		Symbols: []*parser.Symbol{target},
		Edges:   nil,
		Tier:    "basic",
	}
}

func TestUnderstand_PriorLearnings_EmptyOmitsField(t *testing.T) {
	t.Parallel()
	target := testSymFoo()
	cg := testCallGraphFoo(target)

	result := compound.Understand(context.Background(), target, cg, compound.UnderstandOpts{Repo: "r"})
	assertNoPriorLearnings(t, result)
}

func TestUnderstand_PriorLearnings_StoreReturnsNoRecords(t *testing.T) {
	t.Parallel()
	target := testSymFoo()
	cg := testCallGraphFoo(target)

	result := compound.Understand(context.Background(), target, cg, compound.UnderstandOpts{
		Repo:      "r",
		Learnings: &fakeLearnings{records: nil},
	})
	assertNoPriorLearnings(t, result)
}

func TestUnderstand_PriorLearnings_OneRecord(t *testing.T) {
	t.Parallel()
	const wantNote = "previous review flagged missing input validation"
	target := testSymFoo()
	cg := testCallGraphFoo(target)
	store := &fakeLearnings{records: []learnings.Record{
		{
			Repo:          "r",
			Symbol:        "Foo",
			ReviewOutcome: "bad",
			Flag:          "missing-input-validation",
			Note:          wantNote,
			PRURL:         "https://github.com/o/r/pull/42",
		},
	}}

	result := compound.Understand(context.Background(), target, cg, compound.UnderstandOpts{
		Repo:      "r",
		Learnings: store,
	})

	if len(result.PriorLearnings) != 1 {
		t.Fatalf("expected 1 prior learning, got %d", len(result.PriorLearnings))
	}
	if result.PriorLearnings[0].Note != wantNote {
		t.Errorf("note mismatch: want %q, got %q", wantNote, result.PriorLearnings[0].Note)
	}
	if store.gotRepo != "r" {
		t.Errorf("want Nearest repo=r, got %q", store.gotRepo)
	}
	if store.gotSym != "Foo" {
		t.Errorf("want Nearest symbol=Foo, got %q", store.gotSym)
	}
	if store.gotK != 3 {
		t.Errorf("want Nearest k=3, got %d", store.gotK)
	}

	body := jsonBody(t, result)
	if !strings.Contains(body, wantNote) {
		t.Errorf("expected JSON to contain note %q; got %s", wantNote, body)
	}
	if !strings.Contains(body, `"prior_learnings"`) {
		t.Errorf("expected JSON to contain prior_learnings key; got %s", body)
	}
}

func assertNoPriorLearnings(t *testing.T, result *compound.UnderstandResult) {
	t.Helper()
	if len(result.PriorLearnings) != 0 {
		t.Errorf("expected no prior learnings, got %d", len(result.PriorLearnings))
	}
	body := jsonBody(t, result)
	if strings.Contains(body, "prior_learnings") {
		t.Errorf("expected JSON to omit prior_learnings key when empty; got %s", body)
	}
}

func jsonBody(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(data)
}
