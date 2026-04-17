package parser

import (
	"context"
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// RelKind represents the kind of type relationship.
type RelKind string

const (
	// RelEmbeds represents Go struct embedding or interface composition.
	RelEmbeds RelKind = "embeds"

	// RelExtends represents class/interface inheritance (extends).
	RelExtends RelKind = "extends"

	// RelImplements represents interface implementation.
	RelImplements RelKind = "implements"
)

// TypeRelationship represents a relationship between two types (e.g. struct embedding,
// class inheritance, interface implementation).
type TypeRelationship struct {
	Subject string  // the type declaring the relationship
	Target  string  // the referenced type
	Kind    RelKind // relationship kind
	Line    uint32  // 1-based line number
	File    string  // absolute file path
}

// ExtractRelationships parses a source file and returns all type relationships.
// Returns empty slice (not error) for unsupported languages.
func ExtractRelationships(path string, source []byte, opts ParseOpts) ([]TypeRelationship, error) {
	ext := filepath.Ext(path)
	handler := HandlerForExt(ext)
	if handler == nil {
		return nil, nil
	}

	caps := handler.Capabilities()
	if caps.RelationshipsQuery == nil {
		return nil, nil
	}

	p := sitter.NewParser()
	defer p.Close()
	p.SetLanguage(caps.SitterLanguage)

	tree, err := p.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return nil, err
	}
	defer tree.Close()

	lang := handler.Language()
	return runRelQuery(caps.RelationshipsQuery, tree.RootNode(), source, path, lang), nil
}

// relMatchResult holds the extracted capture fields from a single query match.
type relMatchResult struct {
	subject    string
	target     string
	implTarget string
	line       uint32
}

func runRelQuery(q *sitter.Query, root *sitter.Node, source []byte, path, lang string) []TypeRelationship {
	qc := sitter.NewQueryCursor()
	defer qc.Close()
	qc.Exec(q, root)

	var rels []TypeRelationship
	seen := make(map[string]struct{})

	for {
		match, ok := qc.NextMatch()
		if !ok {
			break
		}

		m := extractRelMatch(q, match, source, lang)
		appendRelIfNew(&rels, seen, m.subject, m.target, relKindForLang(lang), m.line, path)
		appendRelIfNew(&rels, seen, m.subject, m.implTarget, RelImplements, m.line, path)
	}

	return rels
}

// extractRelMatch reads capture fields from a single query match.
func extractRelMatch(q *sitter.Query, match *sitter.QueryMatch, source []byte, lang string) relMatchResult {
	var m relMatchResult
	for _, capture := range match.Captures {
		capName := q.CaptureNameForId(capture.Index)
		text := capture.Node.Content(source)

		switch capName {
		case captureRelSubject:
			m.subject = text
		case captureRelTarget:
			if lang == "go" && isNamedField(capture.Node) {
				continue
			}
			m.target = stripPackageQualifier(text)
			m.line = capture.Node.StartPoint().Row + 1
		case captureRelImplTarget:
			m.implTarget = stripPackageQualifier(text)
			m.line = capture.Node.StartPoint().Row + 1
		}
	}
	return m
}

// relKindForLang returns the default relationship kind for a given language.
func relKindForLang(lang string) RelKind {
	if lang == "go" {
		return RelEmbeds
	}
	return RelExtends
}

// appendRelIfNew appends a TypeRelationship to rels if both subject and target
// are non-empty and the relationship has not been seen before.
func appendRelIfNew(rels *[]TypeRelationship, seen map[string]struct{}, subject, target string, kind RelKind, line uint32, path string) {
	if subject == "" || target == "" {
		return
	}
	rel := TypeRelationship{
		Subject: subject,
		Target:  target,
		Kind:    kind,
		Line:    line,
		File:    path,
	}
	key := dedupeKey(rel)
	if _, exists := seen[key]; !exists {
		seen[key] = struct{}{}
		*rels = append(*rels, rel)
	}
}

// stripPackageQualifier removes package/module prefix from a type name.
// e.g. "io.Reader" -> "Reader", "module.Bar" -> "Bar".
func stripPackageQualifier(name string) string {
	if idx := strings.LastIndexByte(name, '.'); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

func dedupeKey(r TypeRelationship) string {
	return r.Subject + ":" + string(r.Kind) + ":" + r.Target
}

// isNamedField checks whether a target node is inside a Go field_declaration
// that has an explicit field name (i.e. not an embedding). Embeddings have
// only a type child, while named fields have both name (field_identifier) and type.
func isNamedField(node *sitter.Node) bool {
	fd := findAncestor(node, "field_declaration")
	if fd == nil {
		return false
	}
	// If the field_declaration has a child of type "field_identifier", it's a named field.
	for i := range int(fd.NamedChildCount()) {
		child := fd.NamedChild(i)
		if child != nil && child.Type() == "field_identifier" {
			return true
		}
	}
	return false
}

// findAncestor walks up the tree to find the nearest ancestor with the given type.
func findAncestor(node *sitter.Node, nodeType string) *sitter.Node {
	for p := node.Parent(); p != nil; p = p.Parent() {
		if p.Type() == nodeType {
			return p
		}
	}
	return nil
}
