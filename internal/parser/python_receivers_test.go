package parser

import (
	"strconv"
	"testing"
)

// recvOf parses src as Python and returns "module|Type" for the method call
// named name on line, or "" when it is untyped.
func recvOf(t *testing.T, src, name string, line int) string {
	t.Helper()
	_, calls, err := ParseFileWithCalls("m.py", []byte(src), ParseOpts{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range calls {
		if c.Name == name && int(c.Line) == line && c.Receiver != "" {
			if c.RecvType == "" {
				return ""
			}
			return c.RecvModule + "|" + c.RecvType
		}
	}
	t.Fatalf("no call %s at line %d in %+v", name, line, calls)
	return ""
}

func TestPythonReceivers_Binding(t *testing.T) {
	t.Parallel()
	const imports = "from pkg.mod import Cls\nimport pkg.other as po\nfrom . import sib\nfrom .rel import RCls as R\n"
	cases := []struct {
		name string
		body string // appended after imports (4 lines); the call is on `line`
		line int
		want string
	}{
		{"ctor local", "def f():\n    x = Cls()\n    x.go()\n", 7, "pkg.mod|Cls"},
		{"annotated local", "def f():\n    x: Cls = make()\n    x.go()\n", 7, "pkg.mod|Cls"},
		{"aliased module ctor", "def f():\n    x = po.Thing()\n    x.go()\n", 7, "pkg.other|Thing"},
		{"relative from-import alias", "def f():\n    x = R()\n    x.go()\n", 7, ".rel|RCls"},
		{"sibling module via from . import", "def f():\n    x = sib.S()\n    x.go()\n", 7, ".sib|S"},
		{"in-file class has no module", "class K: pass\ndef f():\n    x = K()\n    x.go()\n", 8, "|K"},
		{"direct ctor receiver", "def f():\n    Cls().go()\n", 6, "pkg.mod|Cls"},
		// A call to a function is syntactically a constructor call too; the type is
		// then "make", which names no class, and the call-graph layer leaves it
		// unresolved. What matters is that the earlier Cls binding is superseded.
		{"reassigned from unknown call", "def f():\n    x = Cls()\n    x = make()\n    x.go()\n", 8, "|make"},
		{"reassigned back to ctor", "def f():\n    x = make()\n    x = Cls()\n    x.go()\n", 8, "pkg.mod|Cls"},
		{"parameter is unknown", "def f(x):\n    x.go()\n", 6, ""},
		{"parameter shadowing ctor", "def f(x):\n    x = Cls()\n    x.go()\n", 7, "pkg.mod|Cls"},
		{"tuple rebind is unknown", "def f():\n    x = Cls()\n    x, y = pair()\n    x.go()\n", 8, ""},
		{"with-as rebind is unknown", "def f():\n    x = Cls()\n    with ctx() as x:\n        x.go()\n", 8, ""},
		{"for rebind is unknown", "def f():\n    x = Cls()\n    for x in items():\n        x.go()\n", 8, ""},
		{"augmented rebind is unknown", "def f():\n    x = Cls()\n    x += 1\n    x.go()\n", 8, ""},
		{"call on the assignment's own line sees the old binding", "def f():\n    x = Cls()\n    x = x.go()\n", 7, "pkg.mod|Cls"},
		{"other function's local is invisible", "def g():\n    x = Cls()\ndef f():\n    x.go()\n", 8, ""},
		{"self attr from __init__", "class H:\n    def __init__(self):\n        self.c = Cls()\n    def run(self):\n        self.c.go()\n", 9, "pkg.mod|Cls"},
		{"self attr, None is neutral", "class H:\n    def __init__(self):\n        self.c = None\n    def setup(self):\n        self.c = Cls()\n    def run(self):\n        self.c.go()\n", 11, "pkg.mod|Cls"},
		{"self attr annotated", "class H:\n    def __init__(self):\n        self.c: Cls = make()\n    def run(self):\n        self.c.go()\n", 9, "pkg.mod|Cls"},
		{"class-level annotation", "class H:\n    c: Cls\n    def run(self):\n        self.c.go()\n", 8, "pkg.mod|Cls"},
		{"self attr with two classes is unknown", "class H:\n    def __init__(self):\n        self.c = Cls()\n    def other(self):\n        self.c = po.Thing()\n    def run(self):\n        self.c.go()\n", 11, ""},
		{"self attr with unknown assignment is unknown", "class H:\n    def __init__(self):\n        self.c = Cls()\n    def other(self):\n        self.c = make()\n    def run(self):\n        self.c.go()\n", 11, ""},
		{"Optional annotation counts, None is neutral", "from typing import Optional\nclass H:\n    c: Optional[Cls] = None\n    def run(self):\n        self.c.go()\n", 9, "pkg.mod|Cls"},
		{"PEP 604 union with None", "class H:\n    def __init__(self):\n        self.c: Cls | None = None\n    def run(self):\n        self.c.go()\n", 9, "pkg.mod|Cls"},
		{"Union[X, None]", "import typing\nclass H:\n    c: typing.Union[Cls, None] = None\n    def run(self):\n        self.c.go()\n", 9, "pkg.mod|Cls"},
		{"string forward ref", "class H:\n    c: 'Cls'\n    def run(self):\n        self.c.go()\n", 8, "pkg.mod|Cls"},
		{"union of two classes is unknown", "class H:\n    c: Cls | po.Thing\n    def run(self):\n        self.c.go()\n", 8, ""},
		{"generic annotation is unknown", "class H:\n    c: list[Cls]\n    def run(self):\n        self.c.go()\n", 8, ""},
		{"Optional annotation plus ctor assignment", "class H:\n    c: Cls | None = None\n    def setup(self):\n        self.c = Cls()\n    def run(self):\n        self.c.go()\n", 10, "pkg.mod|Cls"},
		{"Optional annotation plus a different class assignment is unknown", "class H:\n    c: Cls | None = None\n    def setup(self):\n        self.c = po.Thing()\n    def run(self):\n        self.c.go()\n", 10, ""},
		{"attr of another object is not self", "class H:\n    def __init__(self, o):\n        o.c = Cls()\n    def run(self, o):\n        o.c.go()\n", 9, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := recvOf(t, imports+tc.body, "go", tc.line); got != tc.want {
				t.Fatalf("line %d: recv = %q, want %q\n%s", tc.line, got, tc.want, imports+tc.body)
			}
		})
	}
}

// Two calls of one name on one line cannot be told apart by (line, name); a
// disagreement between them must leave both untyped, never type the wrong one.
func TestPythonReceivers_SameLineDisagreementIsUntyped(t *testing.T) {
	t.Parallel()
	src := "from pkg.mod import Cls\ndef f(p):\n    x = Cls()\n    x.go(); p.go()\n"
	if got := recvOf(t, src, "go", 4); got != "" {
		t.Fatalf("recv = %q, want untyped (line %s)", got, strconv.Itoa(4))
	}
}
