package main

// Reusable structural-equivalence harness for the hand-rolled -> xml.Marshal
// formatter migration (failure class: manual XML string-concatenation).
//
// The invariant every migrated formatter must satisfy: its output is
// STRUCTURALLY EQUIVALENT to the pre-migration output -- same element names,
// nesting, attributes, and text/CDATA content -- differing ONLY by
//   (a) correct XML escaping, and
//   (b) serialization form (self-closing <x/> vs long-form <x></x>,
//       CDATA vs escaped text, formatter-injected whitespace).
//
// xml.Decoder normalizes (a) and (b) away: it unescapes attribute/text values
// and yields a CharData token for both CDATA and escaped text, and self-closing
// and long-form empty elements both decode to a start immediately followed by
// an end. So comparing the DECODED token trees proves the only remaining
// differences live at the escaping/serialization layer -- exactly the intended
// payload of the migration.
//
// Usage in a per-formatter test:
//
//	assertXMLEquivalent(t, capturedPreMigrationOutput, newFormatter(fixture))
//
// The captured pre-migration string is a golden recorded from the hand-rolled
// formatter on a BENIGN fixture (no <, &, or " in any attribute value, so the
// hand-rolled output is itself well-formed and therefore decodable). The
// escaping FIX is proven separately by assertAttrRoundTrips on a hostile
// fixture, where the hand-rolled output is malformed and does not round-trip.

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readGolden reads a recorded pre-migration baseline from testdata/. These
// files hold the exact bytes the hand-rolled formatter produced (captured once
// against the pre-migration tree), so equivalence tests can decode-and-compare
// against them.
func readGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("readGolden(%s): %v", name, err)
	}
	return string(b)
}

// xmlTreeNode is a normalized, decoder-derived view of an XML element used for
// structural comparison. Attribute order and serialization form are discarded;
// child order is preserved (XML child order is semantic).
type xmlTreeNode struct {
	name     string            // local element name
	attrs    map[string]string // local attr name -> unescaped value
	text     string            // concatenated direct CharData (text + CDATA), unescaped
	children []*xmlTreeNode
}

// parseXMLTree decodes s into a single root xmlTreeNode. It fails the test on a
// decode error, which is itself a signal: hand-rolled output that is not
// well-formed (e.g. an attribute value containing a raw ") will not decode.
func parseXMLTree(t *testing.T, label, s string) *xmlTreeNode {
	t.Helper()
	root, err := decodeXMLTree(s)
	if err != nil {
		t.Fatalf("parseXMLTree(%s): input is not well-formed XML: %v\ninput: %s", label, err, s)
	}
	return root
}

// decodeXMLTree is the non-fatal core: it returns an error instead of failing,
// so callers proving that malformed input does NOT decode can assert on err.
func decodeXMLTree(s string) (*xmlTreeNode, error) {
	dec := xml.NewDecoder(strings.NewReader(s))
	var stack []*xmlTreeNode
	var root *xmlTreeNode
	for {
		tok, err := dec.Token()
		if err != nil {
			// io.EOF ends the stream cleanly.
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
		switch e := tok.(type) {
		case xml.StartElement:
			node := &xmlTreeNode{
				name:  e.Name.Local,
				attrs: make(map[string]string, len(e.Attr)),
			}
			for _, a := range e.Attr {
				node.attrs[a.Name.Local] = a.Value
			}
			if len(stack) > 0 {
				parent := stack[len(stack)-1]
				parent.children = append(parent.children, node)
			} else if root == nil {
				root = node
			}
			stack = append(stack, node)
		case xml.EndElement:
			if len(stack) == 0 {
				return nil, fmt.Errorf("unbalanced end element </%s>", e.Name.Local)
			}
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				top := stack[len(stack)-1]
				top.text += string(e)
			}
		}
	}
	if root == nil {
		return nil, fmt.Errorf("no root element")
	}
	if len(stack) != 0 {
		return nil, fmt.Errorf("unterminated element(s): %d open", len(stack))
	}
	return root, nil
}

// assertXMLEquivalent fails the test unless a and b decode to the same tree.
func assertXMLEquivalent(t *testing.T, a, b string) {
	t.Helper()
	ta := parseXMLTree(t, "current", a)
	tb := parseXMLTree(t, "migrated", b)
	if diff := diffXMLTree(ta, tb, "/"+ta.name); diff != "" {
		t.Errorf("XML output not structurally equivalent:\n%s\n\ncurrent:  %s\nmigrated: %s", diff, a, b)
	}
}

// diffXMLTree returns "" when a and b are structurally identical, else a
// human-readable description of the first difference and its path.
func diffXMLTree(a, b *xmlTreeNode, path string) string {
	if a.name != b.name {
		return fmt.Sprintf("at %s: element name %q vs %q", path, a.name, b.name)
	}
	if len(a.attrs) != len(b.attrs) {
		return fmt.Sprintf("at %s: attribute count %d vs %d (%v vs %v)", path, len(a.attrs), len(b.attrs), a.attrs, b.attrs)
	}
	for k, va := range a.attrs {
		vb, ok := b.attrs[k]
		if !ok {
			return fmt.Sprintf("at %s: attribute %q present in current, absent in migrated", path, k)
		}
		if va != vb {
			return fmt.Sprintf("at %s: attribute %q value %q vs %q", path, k, va, vb)
		}
	}
	if a.text != b.text {
		return fmt.Sprintf("at %s: text %q vs %q", path, a.text, b.text)
	}
	if len(a.children) != len(b.children) {
		return fmt.Sprintf("at %s: child count %d vs %d (%s vs %s)", path, len(a.children), len(b.children), childNames(a), childNames(b))
	}
	for i := range a.children {
		if d := diffXMLTree(a.children[i], b.children[i], fmt.Sprintf("%s/%s[%d]", path, a.children[i].name, i)); d != "" {
			return d
		}
	}
	return ""
}

func childNames(n *xmlTreeNode) string {
	names := make([]string, 0, len(n.children))
	for _, c := range n.children {
		names = append(names, c.name)
	}
	return "[" + strings.Join(names, ",") + "]"
}

// assertAttrRoundTrips proves the escaping FIX: the migrated output is
// well-formed and decoding it recovers wantValue verbatim at the given
// path/attr (a value that contains XML-hostile characters like <, &, ").
// path is a slash-separated chain of element local-names from the root, e.g.
// "response/site/meta"; attr is the local attribute name on the final element.
func assertAttrRoundTrips(t *testing.T, migrated, path, attr, wantValue string) {
	t.Helper()
	root, err := decodeXMLTree(migrated)
	if err != nil {
		t.Fatalf("migrated output is not well-formed XML: %v\ninput: %s", err, migrated)
	}
	node := findByPath(root, strings.Split(path, "/"))
	if node == nil {
		t.Fatalf("path %q not found in migrated output: %s", path, migrated)
	}
	got, ok := node.attrs[attr]
	if !ok {
		t.Fatalf("attr %q not found at %q in migrated output: %s", attr, path, migrated)
	}
	if got != wantValue {
		t.Errorf("attr %q at %q round-tripped to %q, want %q", attr, path, got, wantValue)
	}
}

// assertTextRoundTrips proves the escaping FIX for a text (chardata) node: the
// migrated output is well-formed and the concatenated direct text at the given
// element path recovers wantValue verbatim (a value carrying XML-hostile
// characters like <, & that the prior formatter emitted raw). path is a
// slash-separated chain of element local-names from the root, e.g.
// "response/map".
func assertTextRoundTrips(t *testing.T, migrated, path, wantValue string) {
	t.Helper()
	root, err := decodeXMLTree(migrated)
	if err != nil {
		t.Fatalf("migrated output is not well-formed XML: %v\ninput: %s", err, migrated)
	}
	node := findByPath(root, strings.Split(path, "/"))
	if node == nil {
		t.Fatalf("path %q not found in migrated output: %s", path, migrated)
	}
	if node.text != wantValue {
		t.Errorf("text at %q round-tripped to %q, want %q", path, node.text, wantValue)
	}
}

// assertNotWellFormed proves the BUG: the hand-rolled output does not decode
// (a hostile attribute value produced malformed XML). Recorded alongside the
// escaping-fix assertion so the regression guard documents both sides.
func assertNotWellFormed(t *testing.T, s string) {
	t.Helper()
	if _, err := decodeXMLTree(s); err == nil {
		t.Errorf("expected hand-rolled output to be malformed XML, but it decoded cleanly: %s", s)
	}
}

// findByPath walks children by local-name following names[1:] (names[0] must be
// the root's own name). Returns the first match at each level.
func findByPath(root *xmlTreeNode, names []string) *xmlTreeNode {
	if len(names) == 0 || root.name != names[0] {
		return nil
	}
	cur := root
	for _, want := range names[1:] {
		var next *xmlTreeNode
		for _, c := range cur.children {
			if c.name == want {
				next = c
				break
			}
		}
		if next == nil {
			return nil
		}
		cur = next
	}
	return cur
}
