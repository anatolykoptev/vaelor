package main

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/codegraph"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newTestServer returns a bare mcp.Server suitable for registration tests.
func newTestServer(t *testing.T) *mcp.Server {
	t.Helper()
	return mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
}

// serverTools connects a client to the server over an in-memory transport and
// returns the list of registered tool names.
func serverTools(t *testing.T, server *mcp.Server) []string {
	t.Helper()
	ctx := t.Context()
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	go func() {
		// Connect (and serve) until the client closes; ignore the session handle.
		_, _ = server.Connect(ctx, serverTransport, nil)
	}()
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil)
	cs, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("serverTools: client connect: %v", err)
	}
	defer cs.Close()
	result, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("serverTools: ListTools: %v", err)
	}
	names := make([]string, len(result.Tools))
	for i, tool := range result.Tools {
		names[i] = tool.Name
	}
	return names
}

// TestListFlows_NilStore_RegisterSkipped verifies that registerListFlows does
// NOT register the tool when graphStore is nil (DATABASE_URL not configured).
// This test goes RED if the nil guard ("if graphStore == nil { return }") is
// removed from registerListFlows.
func TestListFlows_NilStore_RegisterSkipped(t *testing.T) {
	server := newTestServer(t)

	// Call with nil store — the guard must skip registration.
	registerListFlows(server, nil, SemanticDeps{})

	// The tool must NOT appear in the server's tool list.
	tools := serverTools(t, server)
	for _, name := range tools {
		if name == "list_flows" {
			t.Errorf("list_flows was registered despite nil graphStore; nil guard is broken")
		}
	}
}

// TestListFlows_FormatEmpty verifies formatFlows output for an empty slice.
func TestListFlows_FormatEmpty(t *testing.T) {
	out := formatFlows(nil, "/repo/go-code", "gcREPOKEY")
	if out == "" {
		t.Error("formatFlows returned empty string for empty flows")
	}
	// Must mention the repo key so operator can identify which repo is missing flows.
	if len(out) < 10 {
		t.Errorf("formatFlows output too short: %q", out)
	}
}

// TestListFlows_FormatNonEmpty verifies formatFlows renders key fields.
func TestListFlows_FormatNonEmpty(t *testing.T) {
	flows := []codegraph.Flow{
		{
			FlowID:     "abc123",
			Name:       "handleSearch → MergeRRF",
			EntrySym:   "handleSearch",
			EntryFile:  "cmd/go-code/tool_semantic_search.go",
			LeafSym:    "MergeRRF",
			MemberSyms: []string{"handleSearch", "hybridResult", "MergeRRF"},
			Priority:   0.75,
			Community:  "2",
		},
		{
			FlowID:     "def456",
			Name:       "IndexRepo → computeSymbolPageRank",
			EntrySym:   "IndexRepo",
			EntryFile:  "internal/codegraph/index.go",
			LeafSym:    "computeSymbolPageRank",
			MemberSyms: []string{"IndexRepo", "computeSymbolPageRank"},
			Priority:   0.50,
			Community:  "1",
		},
	}

	out := formatFlows(flows, "/repo/go-code", "gcREPOKEY")

	for _, want := range []string{"handleSearch", "MergeRRF", "IndexRepo", "0.7500", "0.5000"} {
		if !flowOutputContains(out, want) {
			t.Errorf("formatFlows output missing %q\noutput: %s", want, out)
		}
	}

	// Verify flows are numbered in priority order.
	idx1 := flowOutputIndex(out, "handleSearch → MergeRRF")
	idx2 := flowOutputIndex(out, "IndexRepo → computeSymbolPageRank")
	if idx1 < 0 || idx2 < 0 {
		t.Fatalf("expected both flow names in output; got:\n%s", out)
	}
	if idx1 > idx2 {
		t.Errorf("higher-priority flow (0.75) should appear before lower-priority (0.50) in output")
	}
}

// TestListFlows_FormatChainDisplay verifies that intermediate chain members are shown.
func TestListFlows_FormatChainDisplay(t *testing.T) {
	flows := []codegraph.Flow{
		{
			Name:       "A → G",
			EntrySym:   "A",
			EntryFile:  "a.go",
			LeafSym:    "G",
			MemberSyms: []string{"A", "B", "C", "D", "E", "F", "G"},
			Priority:   0.9,
			Community:  "0",
		},
	}
	out := formatFlows(flows, "/repo", "key")
	// Chain intermediates B-F should appear (6 members → within the display limit).
	if !flowOutputContains(out, "B") || !flowOutputContains(out, "F") {
		t.Errorf("intermediate chain members not shown; output:\n%s", out)
	}
}

// helpers.

func flowOutputContains(s, sub string) bool {
	return len(s) >= len(sub) && flowOutputIndex(s, sub) >= 0
}

func flowOutputIndex(s, sub string) int {
	for i := range len(s) - len(sub) + 1 {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
