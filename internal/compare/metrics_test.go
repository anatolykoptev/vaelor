package compare

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestComputeMetrics(t *testing.T) {
	snap := &RepoSnapshot{
		Name:       "testrepo",
		FileCount:  3,
		TotalLines: 120,
		// 4 symbols: 1 interface, 2 functions, 1 method
		Symbols: []*parser.Symbol{
			{
				Name:      "Handler",
				Kind:      parser.KindInterface,
				File:      "/repo/handler.go",
				StartLine: 1,
				EndLine:   5,
			},
			{
				Name:       "NewServer",
				Kind:       parser.KindFunction,
				File:       "/repo/server.go",
				StartLine:  10,
				EndLine:    29, // 20 lines
				Signature:  "func NewServer() (*Server, error)",
				DocComment: "NewServer creates a new server instance.",
			},
			{
				Name:      "internalHelper",
				Kind:      parser.KindFunction,
				File:      "/repo/server_test.go",
				StartLine: 1,
				EndLine:   10, // 10 lines
				Signature: "func internalHelper(x int) int",
			},
			{
				Name:      "ServeHTTP",
				Kind:      parser.KindMethod,
				File:      "/repo/handler.go",
				StartLine: 7,
				EndLine:   16, // 10 lines
				Signature: "func (h *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) error",
			},
		},
		// 1 external dep (github.com/...), 1 stdlib (strings)
		Imports: []string{
			"strings",
			"github.com/some/lib",
		},
	}

	got := ComputeMetrics(snap)

	// Files and TotalLines are copied from snapshot.
	if got.Files != 3 {
		t.Errorf("Files = %d, want 3", got.Files)
	}
	if got.TotalLines != 120 {
		t.Errorf("TotalLines = %d, want 120", got.TotalLines)
	}

	// AvgFuncLines: (20 + 10 + 10) / 3 = 13.333...
	const expectedAvg = (20.0 + 10.0 + 10.0) / 3.0
	if got.AvgFuncLines < expectedAvg-0.01 || got.AvgFuncLines > expectedAvg+0.01 {
		t.Errorf("AvgFuncLines = %.4f, want %.4f", got.AvgFuncLines, expectedAvg)
	}

	// MaxFuncLines: 20 (NewServer: lines 10–29)
	if got.MaxFuncLines != 20 {
		t.Errorf("MaxFuncLines = %d, want 20", got.MaxFuncLines)
	}

	// TestRatio: 1 test file (server_test.go) / 3 total files
	const expectedTestRatio = 1.0 / 3.0
	if got.TestRatio < expectedTestRatio-0.01 || got.TestRatio > expectedTestRatio+0.01 {
		t.Errorf("TestRatio = %.4f, want %.4f", got.TestRatio, expectedTestRatio)
	}

	// DocRatio: exported symbols = Handler(interface), NewServer(function), ServeHTTP(method) = 3
	// with doc comment = NewServer only = 1
	// internalHelper is not exported
	const expectedDocRatio = 1.0 / 3.0
	if got.DocRatio < expectedDocRatio-0.01 || got.DocRatio > expectedDocRatio+0.01 {
		t.Errorf("DocRatio = %.4f, want %.4f", got.DocRatio, expectedDocRatio)
	}

	// ExternalDeps: github.com/some/lib = 1 (strings is stdlib)
	if got.ExternalDeps != 1 {
		t.Errorf("ExternalDeps = %d, want 1", got.ExternalDeps)
	}

	// Interfaces: 1 (Handler)
	if got.Interfaces != 1 {
		t.Errorf("Interfaces = %d, want 1", got.Interfaces)
	}
}

func TestComputeMetrics_Empty(t *testing.T) {
	snap := &RepoSnapshot{}
	got := ComputeMetrics(snap)

	if got.AvgFuncLines != 0 {
		t.Errorf("AvgFuncLines = %f, want 0", got.AvgFuncLines)
	}
	if got.MaxFuncLines != 0 {
		t.Errorf("MaxFuncLines = %d, want 0", got.MaxFuncLines)
	}
	if got.TestRatio != 0 {
		t.Errorf("TestRatio = %f, want 0", got.TestRatio)
	}
	if got.DocRatio != 0 {
		t.Errorf("DocRatio = %f, want 0", got.DocRatio)
	}
	if got.ExternalDeps != 0 {
		t.Errorf("ExternalDeps = %d, want 0", got.ExternalDeps)
	}
}

func TestIsTestFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/repo/server_test.go", true},
		{"/repo/handler_test.py", true},
		{"/repo/util.test.ts", true},
		{"/repo/app.test.js", true},
		{"/repo/comp.spec.ts", true},
		{"/repo/comp.spec.js", true},
		{"/repo/test/helper.go", true},
		{"/repo/tests/integration.go", true},
		{"/repo/server.go", false},
		{"/repo/testing_utils.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := isTestFile(tt.path)
			if got != tt.want {
				t.Errorf("isTestFile(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsExternalImport(t *testing.T) {
	tests := []struct {
		imp  string
		want bool
	}{
		{"fmt", false},
		{"strings", false},
		{"net/http", false},
		{"encoding/json", false},
		{"github.com/some/pkg", true},
		{"golang.org/x/text", true},
		{"gopkg.in/yaml.v3", true},
		{"k8s.io/client-go", true},
	}

	for _, tt := range tests {
		t.Run(tt.imp, func(t *testing.T) {
			got := isExternalImport(tt.imp)
			if got != tt.want {
				t.Errorf("isExternalImport(%q) = %v, want %v", tt.imp, got, tt.want)
			}
		})
	}
}
