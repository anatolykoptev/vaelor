package review

import (
	"context"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/graphx"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

type stubAnalytics struct {
	signals map[string]graphx.Signals
}

func (s *stubAnalytics) Symbol(_ context.Context, _, name, file string) (graphx.Signals, error) {
	if sig, ok := s.signals[name+":"+file]; ok {
		return sig, nil
	}
	return graphx.Signals{}, nil
}

func (s *stubAnalytics) TopPageRank(_ context.Context, _ string, _ int) ([]graphx.Signal, error) {
	return nil, nil
}

func sym(name, file string) *parser.Symbol {
	return &parser.Symbol{Name: name, File: file}
}

func TestApplyGraphFlags_NilAnalytics_NoMutation(t *testing.T) {
	t.Parallel()
	changed := []ChangedSymbol{{Symbol: sym("Foo", "a.go")}}
	ApplyGraphFlags(context.Background(), nil, "repo", changed, nil)
	if changed[0].Flag != "" || changed[0].Note != "" {
		t.Errorf("expected no mutation, got Flag=%q Note=%q", changed[0].Flag, changed[0].Note)
	}
}

func TestApplyGraphFlags_EmptyRepoKey_NoMutation(t *testing.T) {
	t.Parallel()
	stub := &stubAnalytics{signals: map[string]graphx.Signals{
		"Foo:a.go": {Community: "2", Surprise: 0.9, Found: true},
	}}
	changed := []ChangedSymbol{{Symbol: sym("Foo", "a.go")}}
	ApplyGraphFlags(context.Background(), stub, "", changed, nil)
	if changed[0].Flag != "" {
		t.Errorf("expected no mutation with empty repo key, got Flag=%q", changed[0].Flag)
	}
}

func TestApplyGraphFlags_CommunityMove(t *testing.T) {
	t.Parallel()
	stub := &stubAnalytics{signals: map[string]graphx.Signals{
		"Foo:a.go": {Community: "5", Found: true},
	}}
	before := map[string]string{"Foo:a.go": "2"}
	changed := []ChangedSymbol{{Symbol: sym("Foo", "a.go")}}
	ApplyGraphFlags(context.Background(), stub, "repo", changed, before)
	if changed[0].Flag != "community_move" {
		t.Errorf("Flag = %q, want community_move", changed[0].Flag)
	}
	if changed[0].Note == "" {
		t.Error("Note should describe the move")
	}
}

func TestApplyGraphFlags_HighSurprise(t *testing.T) {
	t.Parallel()
	stub := &stubAnalytics{signals: map[string]graphx.Signals{
		"Foo:a.go": {Community: "2", Surprise: 0.75, Found: true},
	}}
	changed := []ChangedSymbol{{Symbol: sym("Foo", "a.go")}}
	ApplyGraphFlags(context.Background(), stub, "repo", changed, nil)
	if changed[0].Flag != "high_surprise" {
		t.Errorf("Flag = %q, want high_surprise", changed[0].Flag)
	}
}

func TestApplyGraphFlags_CommunityMoveWinsOverSurprise(t *testing.T) {
	t.Parallel()
	stub := &stubAnalytics{signals: map[string]graphx.Signals{
		"Foo:a.go": {Community: "5", Surprise: 0.9, Found: true},
	}}
	before := map[string]string{"Foo:a.go": "2"}
	changed := []ChangedSymbol{{Symbol: sym("Foo", "a.go")}}
	ApplyGraphFlags(context.Background(), stub, "repo", changed, before)
	if changed[0].Flag != "community_move" {
		t.Errorf("community_move should win over high_surprise, got %q", changed[0].Flag)
	}
}

func TestApplyGraphFlags_ColdGraph_NoMutation(t *testing.T) {
	t.Parallel()
	stub := &stubAnalytics{signals: map[string]graphx.Signals{}}
	changed := []ChangedSymbol{{Symbol: sym("Foo", "a.go")}}
	ApplyGraphFlags(context.Background(), stub, "repo", changed, nil)
	if changed[0].Flag != "" {
		t.Errorf("expected no mutation on Found=false, got Flag=%q", changed[0].Flag)
	}
}

func TestApplyGraphFlags_PreExistingFlagPreserved(t *testing.T) {
	t.Parallel()
	stub := &stubAnalytics{signals: map[string]graphx.Signals{
		"Foo:a.go": {Community: "5", Surprise: 0.9, Found: true},
	}}
	before := map[string]string{"Foo:a.go": "2"}
	changed := []ChangedSymbol{{Symbol: sym("Foo", "a.go"), Flag: "policy_violation", Note: "keep me"}}
	ApplyGraphFlags(context.Background(), stub, "repo", changed, before)
	if changed[0].Flag != "policy_violation" || changed[0].Note != "keep me" {
		t.Errorf("pre-existing Flag/Note overwritten: Flag=%q Note=%q", changed[0].Flag, changed[0].Note)
	}
}
