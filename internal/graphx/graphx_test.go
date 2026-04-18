package graphx_test

import (
	"context"
	"testing"

	"github.com/anatolykoptev/go-code/internal/graphx"
)

// Compile-time interface satisfaction checks (mirrored from graphx.go for
// test-package visibility).
var _ graphx.Analytics = graphx.Noop{}
var _ graphx.CrossRefs = graphx.Noop{}

func TestNoopSymbol(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		repoKey    string
		symbolName string
		file       string
	}{
		{name: "empty inputs"},
		{name: "populated inputs", repoKey: "repo", symbolName: "pkg.Fn", file: "main.go"},
	}

	n := graphx.Noop{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := n.Symbol(context.Background(), tc.repoKey, tc.symbolName, tc.file)
			if err != nil {
				t.Fatalf("Symbol() error = %v; want nil", err)
			}
			if got.Found {
				t.Errorf("Symbol().Found = true; want false")
			}
			if got.PageRank != 0 {
				t.Errorf("Symbol().PageRank = %v; want 0", got.PageRank)
			}
			if got.Community != "" {
				t.Errorf("Symbol().Community = %q; want empty", got.Community)
			}
			if got.Surprise != 0 {
				t.Errorf("Symbol().Surprise = %v; want 0", got.Surprise)
			}
		})
	}
}

func TestNoopTopPageRank(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		repoKey string
		k       int
	}{
		{name: "zero k"},
		{name: "positive k", repoKey: "repo", k: 10},
	}

	n := graphx.Noop{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := n.TopPageRank(context.Background(), tc.repoKey, tc.k)
			if err != nil {
				t.Fatalf("TopPageRank() error = %v; want nil", err)
			}
			if len(got) != 0 {
				t.Errorf("TopPageRank() len = %d; want 0", len(got))
			}
		})
	}
}

func TestNoopHandlesRoute(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		repoKey    string
		symbolName string
		file       string
	}{
		{name: "empty inputs"},
		{name: "populated inputs", repoKey: "repo", symbolName: "Handler", file: "handler.go"},
	}

	n := graphx.Noop{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			route, found, err := n.HandlesRoute(context.Background(), tc.repoKey, tc.symbolName, tc.file)
			if err != nil {
				t.Fatalf("HandlesRoute() error = %v; want nil", err)
			}
			if found {
				t.Errorf("HandlesRoute() found = true; want false")
			}
			if route != (graphx.Route{}) {
				t.Errorf("HandlesRoute() route = %+v; want zero Route", route)
			}
		})
	}
}

func TestNoopFetchedBy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		repoKey string
		route   graphx.Route
	}{
		{name: "zero route"},
		{name: "populated route", repoKey: "repo", route: graphx.Route{Method: "GET", Path: "/api/v1/items"}},
	}

	n := graphx.Noop{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := n.FetchedBy(context.Background(), tc.repoKey, tc.route)
			if err != nil {
				t.Fatalf("FetchedBy() error = %v; want nil", err)
			}
			if len(got) != 0 {
				t.Errorf("FetchedBy() len = %d; want 0", len(got))
			}
		})
	}
}

func TestNoopTestedBy(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		repoKey    string
		symbolName string
		file       string
	}{
		{name: "empty inputs"},
		{name: "populated inputs", repoKey: "repo", symbolName: "pkg.Fn", file: "fn.go"},
	}

	n := graphx.Noop{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := n.TestedBy(context.Background(), tc.repoKey, tc.symbolName, tc.file)
			if err != nil {
				t.Fatalf("TestedBy() error = %v; want nil", err)
			}
			if len(got) != 0 {
				t.Errorf("TestedBy() len = %d; want 0", len(got))
			}
		})
	}
}
