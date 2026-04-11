package compare

import (
	"context"
	"testing"
)

func TestCollectArchMetrics_NilStore(t *testing.T) {
	result := CollectArchMetrics(context.Background(), nil, "test")
	if result != nil {
		t.Error("expected nil for nil store")
	}
}

// TestCollectArchMetrics_Integration requires DATABASE_URL and an indexed graph.
// Skip in CI — tested manually via code_compare MCP tool.
func TestCollectArchMetrics_Integration(t *testing.T) {
	t.Skip("requires DATABASE_URL and indexed graph")
}
