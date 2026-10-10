package impact

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/callgraph"
)

// TestAnalyze_PythonQualifiedMethod parses real Python files (a hand-built
// Symbol would bypass the handler that records the class as Receiver) and
// checks the "Class.method" query that impact_analysis used to answer with
// "symbol not found".
func TestAnalyze_PythonQualifiedMethod(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"providers/open_ai.py": "class OpenAi:\n    def transcribe(self, f):\n        return f\n",
		"providers/other.py":   "class Other:\n    def transcribe(self, f):\n        return f\n",
		"app/use.py":           "from providers.open_ai import OpenAi\n\n\ndef run():\n    c = OpenAi()\n    c.transcribe('a')\n",
	}
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	res, err := callgraph.BuildAndEnrich(context.Background(), callgraph.PipelineOpts{Root: root, MaxFileBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}

	got := Analyze(context.Background(), res.CG, "OpenAi.transcribe", Options{})
	if !got.Found {
		t.Fatal("OpenAi.transcribe not found")
	}
	if len(got.DirectCallers) != 1 || got.DirectCallers[0].Name != "run" {
		t.Fatalf("direct callers of OpenAi.transcribe = %+v, want exactly [run]", got.DirectCallers)
	}

	other := Analyze(context.Background(), res.CG, "Other.transcribe", Options{})
	if !other.Found || len(other.DirectCallers) != 0 {
		t.Fatalf("Other.transcribe: found=%v callers=%+v, want found with no callers", other.Found, other.DirectCallers)
	}

	if missing := Analyze(context.Background(), res.CG, "Nope.transcribe", Options{}); missing.Found {
		t.Fatal("Nope.transcribe must not be found")
	}
}
