package callgraph

import (
	"strings"

	"github.com/anatolykoptev/vaelor/internal/importresolve"
	"github.com/anatolykoptev/vaelor/internal/parser"
)

// maxPyBaseDepth bounds the base-class walk when looking for an inherited
// method (a Python hierarchy deeper than this is not worth an edge).
const maxPyBaseDepth = 8

// pyTypedResolver resolves Python method calls whose receiver class the parser
// already recovered (CallSite.RecvType/RecvModule) to the method symbol on that
// class, or on a base class in the repo.
//
// It runs BEFORE the same-file / same-dir / unique-global name tiers: those
// pick by method NAME, so a holder class that defines its own `transcribe`
// would otherwise win over the `self.client.transcribe()` it contains.
type pyTypedResolver struct {
	classesByName map[string][]*parser.Symbol
	methods       map[pyClassKey]map[string]*parser.Symbol
	bases         map[pyClassKey][]string
}

type pyClassKey struct{ file, class string }

// newPyTypedResolver indexes the Python classes and methods in symbols. It
// returns nil when no call site carries a receiver type, so repositories
// without typed Python receivers pay nothing.
func newPyTypedResolver(symbols []*parser.Symbol, calls []parser.CallSite, rels []parser.TypeRelationship) *pyTypedResolver {
	typed := false
	for i := range calls {
		if calls[i].RecvType != "" {
			typed = true
			break
		}
	}
	if !typed {
		return nil
	}
	r := &pyTypedResolver{
		classesByName: map[string][]*parser.Symbol{},
		methods:       map[pyClassKey]map[string]*parser.Symbol{},
		bases:         map[pyClassKey][]string{},
	}
	for _, s := range symbols {
		if s.Language != "python" {
			continue
		}
		switch {
		case s.Kind == parser.KindClass:
			r.classesByName[s.Name] = append(r.classesByName[s.Name], s)
		case s.Kind == parser.KindMethod && s.Receiver != "":
			k := pyClassKey{s.File, s.Receiver}
			if r.methods[k] == nil {
				r.methods[k] = map[string]*parser.Symbol{}
			}
			if _, dup := r.methods[k][s.Name]; !dup {
				r.methods[k][s.Name] = s
			}
		}
	}
	for _, rel := range rels {
		if rel.Kind == parser.RelExtends && strings.HasSuffix(rel.File, ".py") {
			k := pyClassKey{rel.File, rel.Subject}
			r.bases[k] = append(r.bases[k], rel.Target)
		}
	}
	return r
}

// resolve returns (method, true) when the receiver class is positively
// identified in the repo. The method is nil when that class (and its bases)
// has no such method: the receiver is known, so falling back to a name guess
// would only bind the call to an unrelated class. (nil, false) means the
// receiver could not be pinned to exactly one repo class, and the caller falls
// back to the existing name tiers.
func (r *pyTypedResolver) resolve(cs *parser.CallSite) (*parser.Symbol, bool) {
	class := r.receiverClass(cs)
	if class == nil {
		return nil, false
	}
	return r.lookupMethod(class, cs.Name, map[*parser.Symbol]bool{}, 0), true
}

// receiverClass finds the single repo class the call site's RecvType names.
func (r *pyTypedResolver) receiverClass(cs *parser.CallSite) *parser.Symbol {
	var hit *parser.Symbol
	for _, c := range r.classesByName[cs.RecvType] {
		if cs.RecvModule == "" {
			if c.File != cs.File {
				continue
			}
		} else if !importresolve.PythonModuleMatches(cs.RecvModule, cs.File, c.File) {
			continue
		}
		if hit != nil {
			return nil // two candidates: ambiguous, no guess
		}
		hit = c
	}
	return hit
}

// lookupMethod finds name on class, then on its bases depth-first in source
// order, to a bounded depth.
func (r *pyTypedResolver) lookupMethod(class *parser.Symbol, name string, seen map[*parser.Symbol]bool, depth int) *parser.Symbol {
	if seen[class] || depth > maxPyBaseDepth {
		return nil
	}
	seen[class] = true
	k := pyClassKey{class.File, class.Name}
	if m, ok := r.methods[k][name]; ok {
		return m
	}
	for _, base := range r.bases[k] {
		b := r.baseClass(class, base)
		if b == nil {
			continue
		}
		if m := r.lookupMethod(b, name, seen, depth+1); m != nil {
			return m
		}
	}
	return nil
}

// baseClass resolves a base-class expression as written in a `class X(Base)`
// header: a class in the same file, else the only class of that name in the
// repo. The last dotted segment names the class.
func (r *pyTypedResolver) baseClass(from *parser.Symbol, expr string) *parser.Symbol {
	name := expr[strings.LastIndexByte(expr, '.')+1:]
	cands := r.classesByName[name]
	for _, c := range cands {
		if c.File == from.File {
			return c
		}
	}
	if len(cands) == 1 {
		return cands[0]
	}
	return nil
}
