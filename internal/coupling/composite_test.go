package coupling

import (
	"context"
	"errors"
	"testing"
)

type stubVerifier struct {
	ev  []Evidence
	err error
}

func (s stubVerifier) Verify(context.Context, FilePair, FilePair) ([]Evidence, error) {
	return s.ev, s.err
}

func TestCompositeVerifier_ConcatenatesEvidence(t *testing.T) {
	c := NewCompositeVerifier(
		stubVerifier{ev: []Evidence{{Kind: "route", Detail: "POST /api/x", Tier: "offline"}}},
		stubVerifier{ev: []Evidence{{Kind: "symbol", Detail: "RELAY_JWT_SECRET", Tier: "offline"}}},
	)
	ev, err := c.Verify(context.Background(), FilePair{}, FilePair{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 2 {
		t.Fatalf("got %d evidence, want 2: %v", len(ev), ev)
	}
	if ev[0].Kind != "route" || ev[1].Kind != "symbol" {
		t.Errorf("evidence order = %q,%q; want route,symbol", ev[0].Kind, ev[1].Kind)
	}
}

func TestCompositeVerifier_SkipsErroringVerifier(t *testing.T) {
	c := NewCompositeVerifier(
		stubVerifier{err: errors.New("boom")},
		stubVerifier{ev: []Evidence{{Kind: "symbol", Detail: "MAX_PEERS", Tier: "offline"}}},
	)
	ev, err := c.Verify(context.Background(), FilePair{}, FilePair{})
	if err != nil {
		t.Fatalf("composite must swallow sub-verifier errors, got %v", err)
	}
	if len(ev) != 1 || ev[0].Detail != "MAX_PEERS" {
		t.Errorf("got %v, want single MAX_PEERS evidence", ev)
	}
}

func TestCompositeVerifier_NoEvidence(t *testing.T) {
	c := NewCompositeVerifier(stubVerifier{}, stubVerifier{})
	ev, err := c.Verify(context.Background(), FilePair{}, FilePair{})
	if err != nil {
		t.Fatal(err)
	}
	if len(ev) != 0 {
		t.Errorf("got %v, want none", ev)
	}
}
