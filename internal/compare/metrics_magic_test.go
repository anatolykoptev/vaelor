package compare

import (
	"testing"

	"github.com/anatolykoptev/go-code/internal/parser"
)

func TestCountMagicNumbers(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		language string
		want     int
	}{
		{
			name: "no magic numbers",
			body: `func Foo() { return 0 }`,
			want: 0,
		},
		{
			name: "accepted literals 0 1 -1 2",
			body: `func Foo() {
				x := 0
				y := 1
				z := -1
				w := 2
			}`,
			want: 0,
		},
		{
			name: "float identity 1.0 not magic",
			body: `func Foo() {
				score := clamp01(1.0 - ratio)
				x := 1.0 / nf
			}`,
			want: 0,
		},
		{
			name: "float zero 0.0 not magic",
			body: `func Foo() { total := 0.0 }`,
			want: 0,
		},
		{
			name: "negative float -1.0 not magic",
			body: `func Foo() { x := -1.0 }`,
			want: 0,
		},
		{
			name: "float 2.0 not magic (normalizes to 2)",
			body: `func Foo() { x := 2.0 }`,
			want: 0,
		},
		{
			name: "float 1.00 not magic (trailing zeros)",
			body: `func Foo() { x := 1.00 }`,
			want: 0,
		},
		{
			name: "float 0.5 is magic",
			body: `func Foo() { x := 0.5 }`,
			want: 1,
		},
		{
			name: "float 3.14 is magic",
			body: `func Foo() { x := 3.14 }`,
			want: 1,
		},
		{
			name: "dollar-prefixed digits skipped",
			body: `func Foo() { x := $3 + $8 }`,
			want: 0, // $-prefixed digits are SQL-style positional params
		},
		{
			name: "single magic number",
			body: `func Foo() { timeout := 30 }`,
			want: 1,
		},
		{
			name: "multiple magic numbers",
			body: `func Foo() {
				timeout := 30
				maxRetries := 5
				bufSize := 4096
			}`,
			want: 3,
		},
		{
			name: "float magic number 3.14",
			body: `func Foo() { ratio := 3.14 }`,
			want: 1,
		},
		{
			name: "negative magic number",
			body: `func Foo() { offset := -42 }`,
			want: 1,
		},
		{
			name: "string with digits not counted",
			body: `func Foo() { s := "port 8080" }`,
			want: 0,
		},
		{
			name: "comment with digits not counted",
			body: `func Foo() {
				// retry 5 times
				return 0
			}`,
			language: "go",
			want:     0,
		},
		{
			name: "http status codes are magic",
			body: `func Foo() {
				if resp.StatusCode == 200 { ok() }
				if resp.StatusCode == 404 { notFound() }
			}`,
			want: 2,
		},
		{
			name: "bit shift operand not magic",
			body: `func Foo() { mask := 1 << 8 }`,
			want: 1, // 8 is magic, 1 is accepted
		},
		{
			name: "const declaration not counted",
			body: `const maxSize = 1024`,
			want: 0,
		},
		{
			name:     "nolint:mnd skips line",
			body:     "func Foo() {\n\tx := 42 //nolint:mnd\n\ty := 100\n}",
			language: "go",
			want:     1, // only 100 is magic, 42 skipped by nolint
		},
		{
			name:     "nolint:mnd with reason",
			body:     "func Foo() {\n\tx := 42 //nolint:mnd // default timeout\n}",
			language: "go",
			want:     0,
		},
		{
			name: "single digit array indices not magic",
			body: `func Foo() {
				a := m[3]
				b := m[4]
				c := arr[5]
				d := data[9]
			}`,
			want: 0,
		},
		{
			name: "multi digit array index is magic",
			body: `func Foo() { x := arr[42] }`,
			want: 1,
		},
		{
			name: "empty body",
			body: "",
			want: 0,
		},
		{
			name: "hex literal is magic",
			body: `func Foo() { color := 0xFF00FF }`,
			want: 1,
		},
		{
			name: "octal literal is magic",
			body: `func Foo() { perm := 0o755 }`,
			want: 1,
		},
		{
			name: "python magic numbers",
			body: `def foo():
    timeout = 30
    max_retries = 5`,
			language: "python",
			want:     2,
		},
		{
			name: "array/slice index with small literal not magic",
			body: `func Foo() {
				x := arr[0]
				y := arr[1]
				z := arr[2]
			}`,
			want: 0,
		},
		{
			name: "large array index is magic",
			body: `func Foo() { x := arr[42] }`,
			want: 1,
		},
		{
			name: "time duration multiplier",
			body: `func Foo() { d := 60 * time.Second }`,
			want: 1, // 60 is magic — should be a const
		},
		{
			name: "mixed accepted and magic",
			body: `func Foo() {
				if x == 0 { return }
				if y > 100 { return }
				for i := 0; i < 1; i++ {}
			}`,
			want: 1, // only 100 is magic
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := countMagicNumbers(tt.body, tt.language)
			if got != tt.want {
				t.Errorf("countMagicNumbers() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestComputeMagicNumberRatio(t *testing.T) {
	symbols := []*parser.Symbol{
		{
			Kind: parser.KindFunction, File: "/repo/server.go",
			Body:      `func Handle() { timeout := 30; retries := 5 }`,
			StartLine: 1, EndLine: 5,
		},
		{
			Kind: parser.KindFunction, File: "/repo/util.go",
			Body:      `func Add(a, b int) int { return a + b }`, // no magic
			StartLine: 1, EndLine: 3,
		},
		{
			Kind: parser.KindFunction, File: "/repo/config.go",
			Body:      `func Defaults() { port := 8080; workers := 16 }`,
			StartLine: 1, EndLine: 5,
		},
		// Test file — excluded.
		{
			Kind: parser.KindFunction, File: "/repo/server_test.go",
			Body:      `func TestFoo() { assert.Equal(t, 42, result) }`,
			StartLine: 1, EndLine: 3,
		},
		// Non-function — excluded.
		{
			Kind: parser.KindStruct, File: "/repo/types.go",
			Body: `type Config struct { Port int }`,
		},
	}

	ratio := computeMagicNumberRatio(symbols)
	// Eligible functions (non-test): Handle (2 magic), Add (0 magic), Defaults (2 magic)
	// 2 out of 3 have magic numbers.
	expected := 2.0 / 3.0
	if ratio < expected-0.01 || ratio > expected+0.01 {
		t.Errorf("computeMagicNumberRatio() = %f, want %f", ratio, expected)
	}
}

func TestComputeMagicNumberRatio_NoFunctions(t *testing.T) {
	symbols := []*parser.Symbol{
		{Kind: parser.KindStruct, Body: "type Foo struct{}"},
	}
	ratio := computeMagicNumberRatio(symbols)
	if ratio != 0 {
		t.Errorf("computeMagicNumberRatio() = %f, want 0", ratio)
	}
}

func TestComputeMagicNumberRatio_AllClean(t *testing.T) {
	symbols := []*parser.Symbol{
		{
			Kind: parser.KindFunction, File: "/repo/a.go",
			Body:      `func Foo() { return 0 }`,
			StartLine: 1, EndLine: 3,
		},
		{
			Kind: parser.KindFunction, File: "/repo/b.go",
			Body:      `func Bar() { return 1 }`,
			StartLine: 1, EndLine: 3,
		},
	}
	ratio := computeMagicNumberRatio(symbols)
	if ratio != 0 {
		t.Errorf("computeMagicNumberRatio() = %f, want 0", ratio)
	}
}

func TestMagicNumbersInMetrics(t *testing.T) {
	snap := &RepoSnapshot{
		FileCount:  1,
		TotalLines: 50,
		Symbols: []*parser.Symbol{
			{
				Kind: parser.KindFunction, File: "/repo/main.go",
				Body:      `func Run() { timeout := 30; port := 8080 }`,
				StartLine: 1, EndLine: 5,
			},
		},
	}
	m := ComputeMetrics(snap)
	if m.MagicNumberRatio == 0 {
		t.Error("MagicNumberRatio = 0, want > 0 for function with magic numbers")
	}
}

func TestMagicNumbersAffectScore(t *testing.T) {
	base := RepoMetrics{
		Files: 50, TotalLines: 5000,
		AvgFuncLines: 15, MaxFuncLines: 50,
		AvgComplexity: 4.0, MaxComplexity: 10,
		TestRatio: 0.3, DocRatio: 0.6,
		ErrorHandlingRatio: 0.6,
	}
	baseScore := GradeScore(base)

	withMagic := base
	withMagic.MagicNumberRatio = 0.8
	magicScore := GradeScore(withMagic)
	if magicScore >= baseScore {
		t.Errorf("high magic number ratio: score=%.0f >= base=%.0f", magicScore, baseScore)
	}
}

func TestMagicNumbersInOutliers(t *testing.T) {
	snap := &RepoSnapshot{
		Root:      "/repo",
		FileCount: 2,
		Symbols: []*parser.Symbol{
			{
				Kind: parser.KindFunction, File: "/repo/a.go", Name: "Clean",
				Body: `func Clean() { return 0 }`, StartLine: 1, EndLine: 3,
			},
			{
				Kind: parser.KindFunction, File: "/repo/b.go", Name: "Dirty",
				Body:      `func Dirty() { x := 42; y := 100; z := 3.14; w := 255 }`,
				StartLine: 10, EndLine: 15,
			},
		},
	}
	out := CollectOutliers(snap)
	if out.MaxMagicNumbers.Name != "Dirty" {
		t.Errorf("MaxMagicNumbers.Name = %q, want %q", out.MaxMagicNumbers.Name, "Dirty")
	}
	if out.MaxMagicNumbers.Value < 4 {
		t.Errorf("MaxMagicNumbers.Value = %d, want >= 4", out.MaxMagicNumbers.Value)
	}
}

func TestMagicNumbersRecommendation(t *testing.T) {
	m := RepoMetrics{
		Files: 50, TotalLines: 5000,
		AvgFuncLines: 10, MaxFuncLines: 30,
		AvgComplexity: 2.0, MaxComplexity: 5,
		TestRatio: 0.35, DocRatio: 0.8,
		ErrorHandlingRatio: 0.7,
		MagicNumberRatio:   0.6, // bad
	}
	out := Outliers{}
	recs := ComputeRecommendations(m, out, 0)

	found := false
	for _, r := range recs {
		if r.Area == "magic_numbers" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected recommendation for magic_numbers area")
	}
}

func TestSemanticDupRecommendation(t *testing.T) {
	m := RepoMetrics{
		Files: 50, TotalLines: 5000,
		AvgFuncLines: 10, MaxFuncLines: 30,
		AvgComplexity: 2.0, MaxComplexity: 5,
		TestRatio: 0.35, DocRatio: 0.8,
		ErrorHandlingRatio: 0.7,
		SemanticDupRatio:   0.5,
	}
	recs := ComputeRecommendations(m, Outliers{}, 0)

	found := false
	for _, r := range recs {
		if r.Area == "semantic_duplication" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected recommendation for semantic_duplication area")
	}
}
