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
		Imports: []string{
			"strings",
			"github.com/some/lib",
		},
	}

	got := ComputeMetrics(snap)

	if got.Files != 3 {
		t.Errorf("Files = %d, want 3", got.Files)
	}
	if got.TotalLines != 120 {
		t.Errorf("TotalLines = %d, want 120", got.TotalLines)
	}

	const expectedAvg = (20.0 + 10.0 + 10.0) / 3.0
	if got.AvgFuncLines < expectedAvg-0.01 || got.AvgFuncLines > expectedAvg+0.01 {
		t.Errorf("AvgFuncLines = %.4f, want %.4f", got.AvgFuncLines, expectedAvg)
	}

	if got.MaxFuncLines != 20 {
		t.Errorf("MaxFuncLines = %d, want 20", got.MaxFuncLines)
	}

	const expectedTestRatio = 1.0 / 3.0
	if got.TestRatio < expectedTestRatio-0.01 || got.TestRatio > expectedTestRatio+0.01 {
		t.Errorf("TestRatio = %.4f, want %.4f", got.TestRatio, expectedTestRatio)
	}

	const expectedDocRatio = 1.0 / 3.0
	if got.DocRatio < expectedDocRatio-0.01 || got.DocRatio > expectedDocRatio+0.01 {
		t.Errorf("DocRatio = %.4f, want %.4f", got.DocRatio, expectedDocRatio)
	}

	if got.ExternalDeps != 1 {
		t.Errorf("ExternalDeps = %d, want 1", got.ExternalDeps)
	}

	if got.Interfaces != 1 {
		t.Errorf("Interfaces = %d, want 1", got.Interfaces)
	}

	// Score should be populated.
	if got.Score == 0 && got.Files > 0 {
		t.Error("Score = 0 for non-empty repo")
	}

	// Grade should be populated.
	if got.Grade == "" {
		t.Error("Grade is empty")
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
	if got.AvgCognitiveComplexity != 0 {
		t.Errorf("AvgCognitiveComplexity = %f, want 0", got.AvgCognitiveComplexity)
	}
	if got.LargeFileRatio != 0 {
		t.Errorf("LargeFileRatio = %f, want 0", got.LargeFileRatio)
	}
	if got.DuplicationRatio != 0 {
		t.Errorf("DuplicationRatio = %f, want 0", got.DuplicationRatio)
	}
}

func TestErrorHandlingRatio(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"if err check", "if err != nil { return err }", true},
		{"errors.New", "return errors.New(\"fail\")", true},
		{"fmt.Errorf", "return fmt.Errorf(\"wrap: %w\", err)", true},
		{"fmt.Errorf multi-return", `return "", fmt.Errorf("bad: %q", x)`, true},
		{"filepath.SkipDir", "return filepath.SkipDir", true},
		{"try-catch", "try { x() } catch (e) { log(e) }", true},
		{"python except", "except ValueError as e:", true},
		{"false positive preferred", "preferred := getDefault()", false},
		{"false positive stderr", "log.Println(\"stderr output\")", false},
		{"false positive different", "if different { return }", false},
		{"empty body", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasErrorHandling(tt.body)
			if got != tt.want {
				t.Errorf("hasErrorHandling(%q) = %v, want %v", tt.body, got, tt.want)
			}
		})
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

func TestComputeMetrics_Complexity(t *testing.T) {
	snap := &RepoSnapshot{
		FileCount:  1,
		TotalLines: 100,
		Symbols: []*parser.Symbol{
			{
				Name: "Simple", Kind: parser.KindFunction,
				Body: "func Simple() { return 1 }", StartLine: 1, EndLine: 3,
			},
			{
				Name: "Complex", Kind: parser.KindFunction,
				Body: "func Complex() { if a { } if b && c { } for i := range x { } }",
				StartLine: 5, EndLine: 15,
			},
		},
	}

	m := ComputeMetrics(snap)

	if m.AvgComplexity == 0 {
		t.Error("AvgComplexity = 0, want > 0")
	}
	if m.MaxComplexity < 2 {
		t.Errorf("MaxComplexity = %d, want >= 2", m.MaxComplexity)
	}
	// Cognitive complexity should also be computed.
	if m.AvgCognitiveComplexity < 0 {
		t.Errorf("AvgCognitiveComplexity = %f, want >= 0", m.AvgCognitiveComplexity)
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

func TestCountFuncParams(t *testing.T) {
	tests := []struct {
		sig  string
		want int
	}{
		{"func foo()", 0},
		{"func foo(x int)", 1},
		{"func foo(x int, y string)", 2},
		{"func (r *Recv) Method(x int, y string)", 2},
		{"func (r *Recv) Method()", 0},
		{"def foo(self, x, y)", 2}, // Python: self skipped
		{"func foo(a, b, c int)", 3},
		{"func Foo[T any](x T)", 1},                                                     // generics
		{"func (r *Recv) Foo[T any](x T)", 1},                                            // generic method with receiver
		{"func Foo(f func(int, int) bool)", 1},                                            // nested func type
		{"func Map[K comparable, V any](m map[K]V, f func(K, V) bool) []K", 2},           // generics + func param
		{"", 0},
	}

	for _, tt := range tests {
		t.Run(tt.sig, func(t *testing.T) {
			got := countFuncParams(tt.sig)
			if got != tt.want {
				t.Errorf("countFuncParams(%q) = %d, want %d", tt.sig, got, tt.want)
			}
		})
	}
}

func TestComputeLargeFileRatio(t *testing.T) {
	files := []SnapshotFile{
		{Lines: 100},
		{Lines: 200},
		{Lines: 300}, // > 250
		{Lines: 500}, // > 250
	}
	ratio := computeLargeFileRatio(files)
	expected := 2.0 / 4.0
	if ratio < expected-0.01 || ratio > expected+0.01 {
		t.Errorf("computeLargeFileRatio() = %f, want %f", ratio, expected)
	}
}

func TestComputeDuplicationRatio(t *testing.T) {
	symbols := []*parser.Symbol{
		{Kind: parser.KindFunction, BodyHash: 111},
		{Kind: parser.KindFunction, BodyHash: 222},
		{Kind: parser.KindFunction, BodyHash: 111}, // duplicate
		{Kind: parser.KindFunction, BodyHash: 333},
	}
	ratio := computeDuplicationRatio(symbols)
	// 2 out of 4 hashed functions share hash 111 (all have non-zero hash).
	expected := 2.0 / 4.0
	if ratio < expected-0.01 || ratio > expected+0.01 {
		t.Errorf("computeDuplicationRatio() = %f, want %f", ratio, expected)
	}
}

func TestComputeDuplicationRatio_BodyHashZero(t *testing.T) {
	// Functions with BodyHash=0 are excluded from both numerator and denominator.
	symbols := []*parser.Symbol{
		{Kind: parser.KindFunction, BodyHash: 0},   // excluded
		{Kind: parser.KindFunction, BodyHash: 0},   // excluded
		{Kind: parser.KindFunction, BodyHash: 111},  // duplicate
		{Kind: parser.KindFunction, BodyHash: 111},  // duplicate
	}
	ratio := computeDuplicationRatio(symbols)
	// Only 2 hashed functions, both duplicates → 2/2 = 1.0
	expected := 1.0
	if ratio < expected-0.01 || ratio > expected+0.01 {
		t.Errorf("computeDuplicationRatio() = %f, want %f", ratio, expected)
	}
}

func TestComputeDuplicationRatio_NoDups(t *testing.T) {
	symbols := []*parser.Symbol{
		{Kind: parser.KindFunction, BodyHash: 111},
		{Kind: parser.KindFunction, BodyHash: 222},
	}
	ratio := computeDuplicationRatio(symbols)
	if ratio != 0 {
		t.Errorf("computeDuplicationRatio() = %f, want 0", ratio)
	}
}

func TestReturnsError(t *testing.T) {
	tests := []struct {
		sig  string
		want bool
	}{
		{"func Foo() error", true},
		{"func Foo() (*T, error)", true},
		{"func (s *Server) Handle() error", true},
		{"func (s *Server) Handle() (int, error)", true},
		{"func Foo() int", false},
		{"func Foo()", false},
		{"func Foo(err error) int", false}, // error in params, not return
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.sig, func(t *testing.T) {
			got := returnsError(tt.sig)
			if got != tt.want {
				t.Errorf("returnsError(%q) = %v, want %v", tt.sig, got, tt.want)
			}
		})
	}
}

func TestNeedsErrorHandling(t *testing.T) {
	tests := []struct {
		name string
		sym  *parser.Symbol
		want bool
	}{
		{
			"returns error",
			&parser.Symbol{
				Kind:      parser.KindFunction,
				Signature: "func Open() (*File, error)",
				Body:      "return os.Open(path)",
			},
			true,
		},
		{
			"assigns err",
			&parser.Symbol{
				Kind:      parser.KindFunction,
				Signature: "func Process() int",
				Body:      "data, err := json.Unmarshal(b)",
			},
			true,
		},
		{
			"does IO",
			&parser.Symbol{
				Kind:      parser.KindFunction,
				Signature: "func Fetch() string",
				Body:      "resp := http.Get(url)",
			},
			true,
		},
		{
			"pure getter",
			&parser.Symbol{
				Kind:      parser.KindFunction,
				Signature: "func Name() string",
				Body:      "return s.name",
			},
			false,
		},
		{
			"pure computation",
			&parser.Symbol{
				Kind:      parser.KindFunction,
				Signature: "func Add(a, b int) int",
				Body:      "return a + b",
			},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := needsErrorHandling(tt.sym)
			if got != tt.want {
				t.Errorf("needsErrorHandling(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestComputeErrorHandlingRatio_Filtered(t *testing.T) {
	symbols := []*parser.Symbol{
		{
			// Explicit error handling — counted as eligible + handling.
			Kind: parser.KindFunction, File: "/repo/server.go",
			Signature: "func Handle() error",
			Body:      "if err != nil { return err }",
			StartLine: 1, EndLine: 5,
		},
		{
			// Returns error, short body, no explicit pattern → propagation.
			Kind: parser.KindFunction, File: "/repo/server.go",
			Signature: "func Cleanup() error",
			Body:      "return os.RemoveAll(path)",
			StartLine: 10, EndLine: 12,
		},
		{
			// Returns error, LONG body, no explicit pattern → NOT propagation.
			Kind: parser.KindFunction, File: "/repo/server.go",
			Signature: "func ProcessLong() error",
			Body:      "step1()\nstep2()\nstep3()\nstep4()\nstep5()",
			StartLine: 20, EndLine: 40,
		},
		{
			// Pure getter — excluded from denominator.
			Kind: parser.KindFunction, File: "/repo/util.go",
			Signature: "func Name() string",
			Body:      "return s.name",
		},
		{
			// Test file — excluded entirely.
			Kind: parser.KindFunction, File: "/repo/server_test.go",
			Signature: "func TestFoo() error",
			Body:      "assert.NoError(t, err)",
		},
	}

	ratio := computeErrorHandlingRatio(symbols)
	// Eligible: Handle (explicit), Cleanup (propagation), ProcessLong (no handling)
	// Excluded: Name (pure), TestFoo (test file)
	// Handling: Handle + Cleanup = 2/3
	expected := 2.0 / 3.0
	if ratio < expected-0.01 || ratio > expected+0.01 {
		t.Errorf("computeErrorHandlingRatio() = %f, want %f", ratio, expected)
	}
}
