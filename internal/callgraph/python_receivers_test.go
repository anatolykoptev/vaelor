package callgraph

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/anatolykoptev/vaelor/internal/parser"
)

// pyFixture is a synthetic Python tree shaped like an SDK wrapper used through
// instance attributes and locals. Layout is load-bearing: `transcribe` and
// `shared` are each defined on two classes, and every caller lives in a
// directory with no definition of the method it calls, so the pre-existing
// same-file / same-dir / unique-global name tiers cannot produce any of the
// asserted edges by accident.
var pyFixture = map[string]string{
	"providers/open_ai.py": `class OpenAi:
    def __init__(self, base_url=None):
        self.base_url = base_url

    def transcribe(self, filename):
        return filename
`,
	"providers/other.py": `class Other:
    def transcribe(self, filename):
        return None

    def shared(self):
        return None
`,
	"providers/base.py": `class Base:
    def shared(self):
        return 1
`,
	"providers/child.py": `from providers.base import Base


class Child(Base):
    def own(self):
        return 2
`,
	"app/locals.py": `from providers.open_ai import OpenAi


def make():
    return None


def run_local():
    c = OpenAi()
    c.transcribe("a")


def run_annotated():
    c: OpenAi = make()
    c.transcribe("b")
`,
	"app/holder.py": `from providers.open_ai import OpenAi


class Holder:
    e: OpenAi

    def __init__(self):
        self.c = OpenAi()
        self.d: OpenAi = make()
        self.n = None
        self.u: OpenAi | None = None

    def transcribe(self, f):
        return f

    def go(self):
        self.c.transcribe("c")

    def via_d(self):
        self.d.transcribe("d")

    def via_e(self):
        self.e.transcribe("e")

    def setup_u(self):
        self.u = OpenAi()

    def via_u(self):
        self.u.transcribe("u")


def make():
    return None
`,
	"app/aliased.py": `import providers.open_ai as oa


def f():
    x = oa.OpenAi()
    x.transcribe("f")
`,
	"app/inherit.py": `from providers.child import Child


def g():
    ch = Child()
    ch.shared()
`,
	"scripts/modlevel.py": `from providers.open_ai import OpenAi

m = OpenAi()
m.transcribe("g")
`,
	"neg/untyped.py": `from providers.open_ai import OpenAi


def make():
    return None


def untyped(p):
    p.transcribe("x")


def reassigned():
    c = OpenAi()
    c = make()
    c.transcribe("y")
`,
}

func writePyFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range pyFixture {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func buildPyFixtureGraph(t *testing.T) (*CallGraph, string) {
	t.Helper()
	root := writePyFixture(t)
	res, err := BuildAndEnrich(context.Background(), PipelineOpts{
		Root:         root,
		MaxFileBytes: 1 << 20,
	})
	if err != nil {
		t.Fatalf("BuildAndEnrich: %v", err)
	}
	return res.CG, root
}

// callersOf returns "relfile:caller" labels for every edge whose callee is the
// method class.name defined in defFile.
func callersOf(cg *CallGraph, root, defFile, class, name string) []string {
	var out []string
	for _, e := range cg.Edges {
		c := e.Callee
		if c == nil || c.Name != name || c.Receiver != class {
			continue
		}
		if rel, _ := filepath.Rel(root, c.File); filepath.ToSlash(rel) != defFile {
			continue
		}
		if e.Caller == nil {
			// A module-level call has no caller symbol and the edge carries no
			// file, so it is identified by its call-site line.
			out = append(out, "<module>@"+strconv.Itoa(int(e.Line)))
			continue
		}
		label := e.Caller.Name
		if e.Caller.Receiver != "" {
			label = e.Caller.Receiver + "." + label
		}
		rel, _ := filepath.Rel(root, e.Caller.File)
		out = append(out, filepath.ToSlash(rel)+":"+label)
	}
	sort.Strings(out)
	return out
}

func TestPythonReceiverCalls_ExactCallerSets(t *testing.T) {
	cg, root := buildPyFixtureGraph(t)

	cases := []struct {
		name               string
		defFile, cls, meth string
		want               []string
	}{
		{
			name: "OpenAi.transcribe: locals, self attrs, annotation, alias, module scope",
			// Holder.transcribe is defined next to the self.<attr>.transcribe
			// callers: they must resolve to OpenAi, not to the holder.
			defFile: "providers/open_ai.py", cls: "OpenAi", meth: "transcribe",
			want: []string{
				"<module>@4",
				"app/aliased.py:f",
				"app/holder.py:Holder.go",
				"app/holder.py:Holder.via_d",
				"app/holder.py:Holder.via_e",
				"app/holder.py:Holder.via_u",
				"app/locals.py:run_annotated",
				"app/locals.py:run_local",
			},
		},
		{
			name:    "Holder.transcribe has no callers",
			defFile: "app/holder.py", cls: "Holder", meth: "transcribe",
			want: nil,
		},
		{
			name:    "Other.transcribe has no callers (untyped param stays unresolved)",
			defFile: "providers/other.py", cls: "Other", meth: "transcribe",
			want: nil,
		},
		{
			name:    "inherited Base.shared reached through Child",
			defFile: "providers/base.py", cls: "Base", meth: "shared",
			want: []string{"app/inherit.py:g"},
		},
		{
			name:    "Other.shared is not Child's method",
			defFile: "providers/other.py", cls: "Other", meth: "shared",
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := callersOf(cg, root, tc.defFile, tc.cls, tc.meth)
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Fatalf("callers of %s.%s\n got: %q\nwant: %q", tc.cls, tc.meth, got, tc.want)
			}
		})
	}
}

func TestPythonReceiverCalls_MethodsCarryClassReceiver(t *testing.T) {
	cg, _ := buildPyFixtureGraph(t)
	got := FindSymbols(cg.Symbols, "OpenAi.transcribe")
	if len(got) != 1 {
		t.Fatalf("FindSymbols(OpenAi.transcribe) = %d matches, want exactly 1", len(got))
	}
	if got[0].Kind != parser.KindMethod || got[0].Receiver != "OpenAi" {
		t.Fatalf("unexpected symbol %+v", got[0])
	}
	if n := len(FindSymbols(cg.Symbols, "transcribe")); n != 3 {
		t.Fatalf("bare transcribe must still see all 3 definitions, got %d", n)
	}
}
