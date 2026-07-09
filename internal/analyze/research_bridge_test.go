package analyze

import (
	"reflect"
	"testing"
)

func TestResearchDataFieldName(t *testing.T) {
	t.Parallel()
	d := ResearchData{}
	v := reflect.TypeOf(d)
	if _, ok := v.FieldByName("FusedScores"); !ok {
		t.Fatal("ResearchData must expose FusedScores (was BM25Scores — misnomer)")
	}
	if _, ok := v.FieldByName("BM25Scores"); ok {
		t.Fatal("ResearchData.BM25Scores must be removed/renamed")
	}
}
