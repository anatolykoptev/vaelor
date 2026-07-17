package callgraph

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	kitcache "github.com/anatolykoptev/go-kit/cache"

	"github.com/anatolykoptev/go-code/internal/parser"
)

// fakeL2 is an in-memory implementation of kitcache.L2 for tests.
type fakeL2 struct {
	mu   sync.Mutex
	data map[string][]byte
	gets int
	sets int
	dels int
}

func newFakeL2() *fakeL2 {
	return &fakeL2{data: make(map[string][]byte)}
}

func (f *fakeL2) Get(ctx context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	if v, ok := f.data[key]; ok {
		return bytes.Clone(v), nil
	}
	return nil, kitcache.ErrCacheMiss
}

func (f *fakeL2) Set(ctx context.Context, key string, data []byte, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sets++
	f.data[key] = bytes.Clone(data)
	return nil
}

func (f *fakeL2) Del(ctx context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dels++
	delete(f.data, key)
	return nil
}

func (f *fakeL2) Close() error { return nil }

func installFakeCGL2(t *testing.T) *fakeL2 {
	t.Helper()
	InvalidateBuildCache()
	old := cgCache.l2
	fake := newFakeL2()
	cgCache.l2 = fake
	t.Cleanup(func() {
		InvalidateBuildCache()
		cgCache.l2 = old
	})
	return fake
}

// TestCallGraphCache_SetL2Empty leaves L2 nil when RedisURL is empty.
func TestCallGraphCache_SetL2Empty(t *testing.T) {
	InvalidateBuildCache()
	cgCache.l2 = &fakeL2{}
	SetL2("")
	if cgCache.l2 != nil {
		t.Fatalf("SetL2(\"\") must leave L2 nil, got %T", cgCache.l2)
	}
}

// TestCallGraphCache_L1HitDoesNotTouchL2 verifies a cached entry is served
// from L1 without querying L2.
func TestCallGraphCache_L1HitDoesNotTouchL2(t *testing.T) {
	fake := installFakeCGL2(t)

	cg := &CallGraph{Tier: "basic", Backend: BackendTreeSitter}
	cgCache.set("repo", cg)
	if fake.sets != 1 {
		t.Fatalf("set should write to L2 exactly once, got sets=%d", fake.sets)
	}

	getsBefore := fake.gets
	got, ok := cgCache.get("repo")
	if !ok {
		t.Fatalf("expected L1 hit")
	}
	if got != cg {
		t.Fatalf("L1 hit returned wrong pointer")
	}
	if fake.gets != getsBefore {
		t.Fatalf("L1 hit should not query L2: got %d extra L2.Get calls", fake.gets-getsBefore)
	}
}

// TestCallGraphCache_L2HitRepopulatesL1 verifies that an L2 hit deserializes
// the entry and repopulates L1.
func TestCallGraphCache_L2HitRepopulatesL1(t *testing.T) {
	fake := installFakeCGL2(t)

	cg := &CallGraph{
		Tier:    "enhanced",
		Backend: BackendGoTypes,
		Edges: []CallEdge{
			{Caller: parserSymbol("main"), Callee: parserSymbol("util"), CalleeName: "util", Line: 10},
		},
		UsesIndex: map[string][]string{"foo.astro": {"bar.astro"}},
	}
	at := time.Now()
	key := "repo::go:::fa=false"
	data, err := encodeCGEntry(cg, at)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if err := fake.Set(context.Background(), key, data, cgCacheTTL); err != nil {
		t.Fatalf("seed fake L2: %v", err)
	}

	got, ok := cgCache.get(key)
	if !ok {
		t.Fatalf("expected L2 hit")
	}
	if got.Tier != cg.Tier || got.Backend != cg.Backend || len(got.Edges) != len(cg.Edges) {
		t.Fatalf("L2 hit returned wrong data: %+v", got)
	}
	if len(got.UsesIndex["foo.astro"]) != 1 {
		t.Fatalf("UsesIndex not round-tripped")
	}

	// L1 should now be repopulated: a second get should not query L2.
	getsBefore := fake.gets
	_, ok = cgCache.get(key)
	if !ok {
		t.Fatalf("expected L1 hit after repopulation")
	}
	if fake.gets != getsBefore {
		t.Fatalf("L1 repopulated; expected no L2.Get, got %d", fake.gets-getsBefore)
	}
}

// TestCallGraphCache_RoundTrip verifies encode/decode of CallGraph.
func TestCallGraphCache_RoundTrip(t *testing.T) {
	cg := &CallGraph{
		Tier:          "enhanced",
		Backend:       BackendGoTypes,
		HookCallbacks: []string{"hook1", "hook2"},
		Symbols: []*parser.Symbol{
			{Name: "main", Kind: "function", Language: "go", File: "/repo/main.go", StartLine: 1, EndLine: 5},
		},
		Edges: []CallEdge{
			{Caller: parserSymbol("main"), Callee: parserSymbol("util"), CalleeName: "util", Line: 3},
		},
		TypeRels: []parser.TypeRelationship{
			{Subject: "Foo", Target: "Bar", Kind: parser.RelImplements, Line: 7, File: "/repo/foo.go"},
		},
		UsesIndex: map[string][]string{"a.astro": {"b.astro"}},
	}
	at := time.Unix(1234567890, 0).UTC()

	data, err := encodeCGEntry(cg, at)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := decodeCGEntry(data)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !decoded.At.Equal(at) {
		t.Errorf("timestamp mismatch: %v vs %v", decoded.At, at)
	}
	got := decoded.CG
	if got.Tier != cg.Tier || got.Backend != cg.Backend {
		t.Fatalf("metadata mismatch: %+v", got)
	}
	if len(got.HookCallbacks) != len(cg.HookCallbacks) || got.HookCallbacks[0] != cg.HookCallbacks[0] {
		t.Fatalf("HookCallbacks mismatch: %v", got.HookCallbacks)
	}
	if len(got.Symbols) != 1 || got.Symbols[0].Name != "main" {
		t.Fatalf("Symbols mismatch: %+v", got.Symbols)
	}
	if len(got.Edges) != 1 || got.Edges[0].CalleeName != "util" {
		t.Fatalf("Edges mismatch: %+v", got.Edges)
	}
	if len(got.TypeRels) != 1 || got.TypeRels[0].Target != "Bar" {
		t.Fatalf("TypeRels mismatch: %+v", got.TypeRels)
	}
	if len(got.UsesIndex["a.astro"]) != 1 {
		t.Fatalf("UsesIndex mismatch: %+v", got.UsesIndex)
	}
}

// parserSymbol returns a minimal parser.Symbol for test construction.
func parserSymbol(name string) *parser.Symbol {
	return &parser.Symbol{Name: name, Kind: "function", Language: "go", File: "/repo/main.go", StartLine: 1, EndLine: 2}
}

// compile-time guard: fakeL2 implements kitcache.L2.
var _ kitcache.L2 = (*fakeL2)(nil)
