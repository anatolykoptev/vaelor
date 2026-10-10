package parser

import (
	"regexp"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// Python receiver typing.
//
// A Python call `x.m()` carries no type information in the syntax, and
// extractCallReceiver keeps only the last dotted segment, so `self.openai.m()`
// and a local `openai.m()` are indistinguishable and neither names a class.
// annotatePythonReceivers recovers the class from the three binding shapes that
// are decidable without flow analysis, and records it on the CallSite as
// RecvType/RecvModule for the call-graph layer to resolve against the symbol
// table:
//
//   - `self.<attr>.m()`: <attr> is bound to exactly one class across the whole
//     enclosing class (`self.<attr> = Cls(...)` in any method,
//     `self.<attr>: Cls = ...`, or a class-level `<attr>: Cls`);
//   - `<local>.m()`: the last binding of <local> before the call, in the same
//     function (or module) scope, is `<local> = Cls(...)` or
//     `<local>: Cls = ...`;
//   - `Cls(...).m()`: the receiver is itself a constructor call.
//
// Anything else — a parameter, a reassignment from an unknown call, two
// different classes on one attribute, a non-trivial annotation such as
// `Optional[Cls]` — leaves the CallSite untyped: precision over recall.

// pyDottedName matches a bare or dotted identifier path (`Cls`, `oa.Cls`).
var pyDottedName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)

// pyImport is what a local name was bound to by an import statement.
type pyImport struct {
	module string // dotted module path; leading dots mark a relative import
	name   string // imported member for `from module import name`; "" for `import module`
}

// pyBinding is one binding of a local name. typ is the class expression as
// written (`Cls`, `oa.Cls`), or "" when the bound value is not a recognisable
// constructor call or simple annotation (an unknown binding).
type pyBinding struct {
	name        string
	typ         string
	activeAfter uint32 // 1-based line after which the binding is visible
}

// pyAttrState accumulates the bindings of one `self.<attr>`.
type pyAttrState struct {
	types   map[string]struct{}
	unknown bool
}

type pyReceiverCtx struct {
	src      []byte
	imports  map[string]pyImport
	badNames map[string]bool // local names imported twice to different targets
	found    map[recvKey]recvVal
}

type recvKey struct {
	line uint32
	name string
}

type recvVal struct {
	typ, module string
	conflict    bool
}

// annotatePythonReceivers sets RecvType/RecvModule on the method-call sites of
// calls that it can type. It is the Capabilities.AnnotateCalls hook of the
// Python handler.
func annotatePythonReceivers(calls []CallSite, root *sitter.Node, src []byte) {
	if len(calls) == 0 {
		return
	}
	cx := &pyReceiverCtx{src: src, imports: map[string]pyImport{}, badNames: map[string]bool{}, found: map[recvKey]recvVal{}}
	cx.collectImports(root)
	cx.visit(root, cx.collectLocals(root), nil, "")

	for i := range calls {
		cs := &calls[i]
		if cs.Receiver == "" || cs.IsArgRef {
			continue
		}
		if v, ok := cx.found[recvKey{cs.Line, cs.Name}]; ok && !v.conflict {
			cs.RecvType, cs.RecvModule = v.typ, v.module
		}
	}
}

func (cx *pyReceiverCtx) text(n *sitter.Node) string { return n.Content(cx.src) }

// collectImports records every import binding in the file, at any depth.
func (cx *pyReceiverCtx) collectImports(n *sitter.Node) {
	switch n.Type() {
	case "import_statement":
		cx.importStatement(n)
	case "import_from_statement":
		cx.importFrom(n)
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		cx.collectImports(n.NamedChild(i))
	}
}

func (cx *pyReceiverCtx) bind(local string, imp pyImport) {
	if prev, ok := cx.imports[local]; ok && prev != imp {
		cx.badNames[local] = true
	}
	cx.imports[local] = imp
}

func (cx *pyReceiverCtx) importStatement(n *sitter.Node) {
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		switch c.Type() {
		case "dotted_name":
			full := cx.text(c)
			head, _, _ := strings.Cut(full, ".")
			cx.bind(head, pyImport{module: head})
		case "aliased_import":
			name, alias := c.ChildByFieldName("name"), c.ChildByFieldName("alias")
			if name != nil && alias != nil {
				cx.bind(cx.text(alias), pyImport{module: cx.text(name)})
			}
		}
	}
}

func (cx *pyReceiverCtx) importFrom(n *sitter.Node) {
	modNode := n.ChildByFieldName("module_name")
	if modNode == nil {
		return
	}
	module := strings.ReplaceAll(cx.text(modNode), " ", "")
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		if c.StartByte() == modNode.StartByte() {
			continue
		}
		switch c.Type() {
		case "dotted_name":
			cx.bind(cx.text(c), pyImport{module: module, name: cx.text(c)})
		case "aliased_import":
			name, alias := c.ChildByFieldName("name"), c.ChildByFieldName("alias")
			if name != nil && alias != nil {
				cx.bind(cx.text(alias), pyImport{module: module, name: cx.text(name)})
			}
		}
	}
}

// resolveCtor turns a class expression as written into (class name, module
// the class is imported from). module == "" means "defined in the same file".
// ok is false for expressions that cannot be pinned to a class.
func (cx *pyReceiverCtx) resolveCtor(expr string) (typ, module string, ok bool) {
	parts := strings.Split(expr, ".")
	head, ok2 := cx.imports[parts[0]]
	if ok2 && cx.badNames[parts[0]] {
		return "", "", false
	}
	if len(parts) == 1 {
		if !ok2 {
			return parts[0], "", true
		}
		if head.name == "" {
			return "", "", false // a module called like a function
		}
		return head.name, head.module, true
	}
	if !ok2 {
		return "", "", false
	}
	mod := head.module
	if head.name != "" {
		mod = joinPyModule(mod, head.name)
	}
	for _, mid := range parts[1 : len(parts)-1] {
		mod = joinPyModule(mod, mid)
	}
	return parts[len(parts)-1], mod, true
}

func joinPyModule(base, part string) string {
	if strings.HasSuffix(base, ".") {
		return base + part
	}
	return base + "." + part
}

// ctorExpr returns the class expression of `Cls(...)` / `pkg.Cls(...)`, or "".
func (cx *pyReceiverCtx) ctorExpr(n *sitter.Node) string {
	if n == nil || n.Type() != "call" {
		return ""
	}
	fn := n.ChildByFieldName("function")
	if fn == nil || (fn.Type() != "identifier" && fn.Type() != "attribute") {
		return ""
	}
	if t := cx.text(fn); pyDottedName.MatchString(t) {
		return t
	}
	return ""
}

// annotationExpr returns the class expression of a `: T` annotation when it
// names exactly one class, ignoring None: `Cls`, `pkg.Cls`, `'Cls'`,
// `Optional[Cls]`, `Union[Cls, None]` and `Cls | None` all give "Cls". None is
// neutral for a binding (the idiomatic `self.x: Cls | None = None` shape).
// Two classes, generics (`list[Cls]`) and anything else give "" (unknown).
func (cx *pyReceiverCtx) annotationExpr(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	t := strings.Trim(strings.TrimSpace(cx.text(n)), `"'`)
	var members []string
	switch inner, kind := unwrapTypingForm(t); kind {
	case "Optional":
		members = []string{inner}
	case "Union":
		if strings.ContainsAny(inner, "[]") {
			return ""
		}
		members = strings.Split(inner, ",")
	default:
		members = strings.Split(t, "|")
	}
	out := ""
	for _, m := range members {
		m = strings.TrimSpace(m)
		switch {
		case m == "None":
		case !pyDottedName.MatchString(m) || out != "":
			return ""
		default:
			out = m
		}
	}
	return out
}

// unwrapTypingForm splits `Optional[X]` / `Union[X, Y]` (optionally qualified,
// e.g. `typing.Optional[X]`) into its bracket contents and the form's name.
func unwrapTypingForm(t string) (inner, kind string) {
	head, rest, ok := strings.Cut(t, "[")
	if !ok || !strings.HasSuffix(rest, "]") {
		return "", ""
	}
	head = head[strings.LastIndexByte(head, '.')+1:]
	if head != "Optional" && head != "Union" {
		return "", ""
	}
	return strings.TrimSuffix(rest, "]"), head
}

// valueType classifies the type expression of an assignment: the annotation
// when present, else the constructor on the right-hand side. neutral reports a
// `= None` assignment, which never changes a binding's type.
func (cx *pyReceiverCtx) valueType(assign *sitter.Node) (typ string, neutral bool) {
	right := assign.ChildByFieldName("right")
	if ann := assign.ChildByFieldName("type"); ann != nil {
		return cx.annotationExpr(ann), false
	}
	if right == nil {
		return "", false
	}
	if right.Type() == "none" {
		return "", true
	}
	return cx.ctorExpr(right), false
}

// visit walks one scope (module or function body), typing every call in it.
// locals are the scope's own bindings; attrs/selfName describe the enclosing
// class for `self.<attr>` lookups. Nested scopes are entered separately.
func (cx *pyReceiverCtx) visit(n *sitter.Node, locals []pyBinding, attrs map[string]*pyAttrState, selfName string) {
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		switch c.Type() {
		case "function_definition":
			cx.enterFunction(c, attrs, selfName)
			continue
		case "class_definition":
			cx.enterClass(c)
			continue
		case "decorated_definition":
			cx.visit(c, locals, attrs, selfName)
			continue
		case "call":
			cx.typeCall(c, locals, attrs, selfName)
		}
		cx.visit(c, locals, attrs, selfName)
	}
}

func (cx *pyReceiverCtx) enterClass(class *sitter.Node) {
	body := class.ChildByFieldName("body")
	if body == nil {
		return
	}
	attrs := map[string]*pyAttrState{}
	cx.collectClassAttrs(body, attrs, "", true)
	// Statements directly in the class body are not typed; methods are.
	for i := 0; i < int(body.NamedChildCount()); i++ {
		c := body.NamedChild(i)
		if c.Type() == "decorated_definition" {
			c = c.ChildByFieldName("definition")
		}
		switch {
		case c == nil:
		case c.Type() == "function_definition":
			cx.enterFunction(c, attrs, "")
		case c.Type() == "class_definition":
			cx.enterClass(c)
		}
	}
}

func (cx *pyReceiverCtx) enterFunction(fn *sitter.Node, attrs map[string]*pyAttrState, selfName string) {
	body := fn.ChildByFieldName("body")
	if body == nil {
		return
	}
	if attrs != nil && selfName == "" {
		selfName = cx.firstParam(fn)
	}
	locals := cx.collectLocals(fn)
	cx.visit(body, locals, attrs, selfName)
}

// firstParam returns the name of a function's first positional parameter.
func (cx *pyReceiverCtx) firstParam(fn *sitter.Node) string {
	params := fn.ChildByFieldName("parameters")
	if params == nil || params.NamedChildCount() == 0 {
		return ""
	}
	if first := params.NamedChild(0); first.Type() == "identifier" {
		return cx.text(first)
	}
	return ""
}

// collectClassAttrs gathers the type bindings of every `self.<attr>` in the
// class body (method bodies included) and of class-level `<attr>: T`.
func (cx *pyReceiverCtx) collectClassAttrs(n *sitter.Node, attrs map[string]*pyAttrState, selfName string, classLevel bool) {
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		switch c.Type() {
		case "class_definition":
			continue // a nested class has its own attributes
		case "function_definition":
			cx.collectClassAttrs(c, attrs, cx.firstParam(c), false)
			continue
		case "assignment", "augmented_assignment":
			cx.noteAttrAssignment(c, attrs, selfName, classLevel)
		}
		cx.collectClassAttrs(c, attrs, selfName, classLevel)
	}
}

func (cx *pyReceiverCtx) attrState(attrs map[string]*pyAttrState, name string) *pyAttrState {
	st := attrs[name]
	if st == nil {
		st = &pyAttrState{types: map[string]struct{}{}}
		attrs[name] = st
	}
	return st
}

func (cx *pyReceiverCtx) noteAttrAssignment(a *sitter.Node, attrs map[string]*pyAttrState, selfName string, classLevel bool) {
	left := a.ChildByFieldName("left")
	if left == nil {
		return
	}
	targets := assignTargets(left)
	// Only a plain `target = value` / `target: T [= value]` is typed; a tuple
	// target or an augmented assignment makes the attribute unknown.
	single := a.Type() == "assignment" && len(targets) == 1 && targets[0].StartByte() == left.StartByte()
	for _, target := range targets {
		attr, ok := cx.attrName(target, selfName, classLevel)
		if !ok {
			continue
		}
		st := cx.attrState(attrs, attr)
		if !single {
			st.unknown = true
			continue
		}
		typ, neutral := cx.valueType(a)
		switch {
		case neutral:
		case typ == "":
			st.unknown = true
		default:
			st.types[typ] = struct{}{}
		}
	}
}

// attrName returns the attribute a target assigns: `<attr>` at class level,
// `<selfName>.<attr>` inside a method.
func (cx *pyReceiverCtx) attrName(target *sitter.Node, selfName string, classLevel bool) (string, bool) {
	if classLevel {
		if target.Type() == "identifier" {
			return cx.text(target), true
		}
		return "", false
	}
	if selfName == "" || target.Type() != "attribute" {
		return "", false
	}
	obj, at := target.ChildByFieldName("object"), target.ChildByFieldName("attribute")
	if obj == nil || at == nil || obj.Type() != "identifier" || cx.text(obj) != selfName {
		return "", false
	}
	return cx.text(at), true
}

// assignTargets flattens an assignment target into its leaf targets.
func assignTargets(left *sitter.Node) []*sitter.Node {
	switch left.Type() {
	case "pattern_list", "tuple_pattern", "list_pattern", "expression_list", "as_pattern_target":
		var out []*sitter.Node
		for i := 0; i < int(left.NamedChildCount()); i++ {
			out = append(out, assignTargets(left.NamedChild(i))...)
		}
		return out
	}
	return []*sitter.Node{left}
}

// collectLocals returns the bindings made directly in scope (a function or the
// module), not in nested functions, classes, lambdas or comprehensions.
func (cx *pyReceiverCtx) collectLocals(scope *sitter.Node) []pyBinding {
	var out []pyBinding
	if scope.Type() == "function_definition" {
		out = append(out, cx.paramBindings(scope)...)
	}
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		for i := 0; i < int(n.NamedChildCount()); i++ {
			c := n.NamedChild(i)
			switch c.Type() {
			case "function_definition", "class_definition":
				if nm := c.ChildByFieldName("name"); nm != nil {
					out = append(out, pyBinding{name: cx.text(nm), activeAfter: c.StartPoint().Row + 1})
				}
				continue
			case "lambda", "list_comprehension", "set_comprehension", "dictionary_comprehension", "generator_expression":
				continue
			}
			out = append(out, cx.bindingsOf(c)...)
			walk(c)
		}
	}
	if body := scope.ChildByFieldName("body"); body != nil {
		walk(body)
	} else {
		walk(scope)
	}
	return out
}

func (cx *pyReceiverCtx) paramBindings(fn *sitter.Node) []pyBinding {
	params := fn.ChildByFieldName("parameters")
	if params == nil {
		return nil
	}
	var out []pyBinding
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		for i := 0; i < int(n.NamedChildCount()); i++ {
			c := n.NamedChild(i)
			if c.Type() == "identifier" {
				out = append(out, pyBinding{name: cx.text(c), activeAfter: 0})
				continue
			}
			walk(c)
		}
	}
	walk(params)
	return out
}

// bindingsOf returns the names a single node binds in the current scope.
func (cx *pyReceiverCtx) bindingsOf(n *sitter.Node) []pyBinding {
	switch n.Type() {
	case "assignment":
		return cx.assignmentBindings(n)
	case "augmented_assignment":
		return cx.unknownBindings(n.ChildByFieldName("left"), n.EndPoint().Row+1)
	case "for_statement":
		left := n.ChildByFieldName("left")
		return cx.unknownBindings(left, left.EndPoint().Row+1)
	case "named_expression":
		return cx.unknownBindings(n.ChildByFieldName("name"), n.EndPoint().Row+1)
	case "as_pattern_target":
		return cx.unknownBindings(n, n.EndPoint().Row+1)
	case "import_statement", "import_from_statement":
		return cx.importBindings(n)
	}
	return nil
}

func (cx *pyReceiverCtx) assignmentBindings(a *sitter.Node) []pyBinding {
	left := a.ChildByFieldName("left")
	if left == nil {
		return nil
	}
	end := a.EndPoint().Row + 1
	if left.Type() != "identifier" {
		return cx.unknownBindings(left, end)
	}
	typ, neutral := cx.valueType(a)
	if neutral {
		return nil
	}
	return []pyBinding{{name: cx.text(left), typ: typ, activeAfter: end}}
}

func (cx *pyReceiverCtx) unknownBindings(target *sitter.Node, after uint32) []pyBinding {
	if target == nil {
		return nil
	}
	var out []pyBinding
	for _, t := range assignTargets(target) {
		if t.Type() == "identifier" {
			out = append(out, pyBinding{name: cx.text(t), activeAfter: after})
		}
	}
	return out
}

func (cx *pyReceiverCtx) importBindings(n *sitter.Node) []pyBinding {
	var out []pyBinding
	after := n.EndPoint().Row + 1
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		switch c.Type() {
		case "aliased_import":
			if alias := c.ChildByFieldName("alias"); alias != nil {
				out = append(out, pyBinding{name: cx.text(alias), activeAfter: after})
			}
		case "dotted_name":
			head, _, _ := strings.Cut(cx.text(c), ".")
			out = append(out, pyBinding{name: head, activeAfter: after})
		}
	}
	return out
}

// lastBinding returns the latest binding of name made strictly before line.
func lastBinding(locals []pyBinding, name string, line uint32) (pyBinding, bool) {
	var best pyBinding
	found := false
	for _, b := range locals {
		if b.name == name && b.activeAfter < line && (!found || b.activeAfter >= best.activeAfter) {
			best, found = b, true
		}
	}
	return best, found
}

// typeCall types the receiver of one call node and records the result.
func (cx *pyReceiverCtx) typeCall(call *sitter.Node, locals []pyBinding, attrs map[string]*pyAttrState, selfName string) {
	fn := call.ChildByFieldName("function")
	if fn == nil || fn.Type() != "attribute" {
		return
	}
	method, obj := fn.ChildByFieldName("attribute"), fn.ChildByFieldName("object")
	if method == nil || obj == nil {
		return
	}
	line := method.StartPoint().Row + 1
	expr := cx.receiverExpr(obj, line, locals, attrs, selfName)
	key := recvKey{line, cx.text(method)}
	typ, module, ok := "", "", false
	if expr != "" {
		typ, module, ok = cx.resolveCtor(expr)
	}
	v := recvVal{typ: typ, module: module, conflict: !ok}
	// Two calls of one name on one line cannot be told apart by (line, name):
	// any disagreement, or an untyped sibling, poisons the key.
	if prev, seen := cx.found[key]; seen && (prev.conflict || v.conflict || prev != v) {
		v = recvVal{conflict: true}
	}
	cx.found[key] = v
}

// receiverExpr returns the class expression of a call receiver, or "".
func (cx *pyReceiverCtx) receiverExpr(obj *sitter.Node, line uint32, locals []pyBinding, attrs map[string]*pyAttrState, selfName string) string {
	switch obj.Type() {
	case "identifier":
		b, ok := lastBinding(locals, cx.text(obj), line)
		if !ok {
			return ""
		}
		return b.typ
	case "call":
		return cx.ctorExpr(obj)
	case "attribute":
		base, at := obj.ChildByFieldName("object"), obj.ChildByFieldName("attribute")
		if base == nil || at == nil || selfName == "" || base.Type() != "identifier" || cx.text(base) != selfName {
			return ""
		}
		st := attrs[cx.text(at)]
		if st == nil || st.unknown || len(st.types) != 1 {
			return ""
		}
		for t := range st.types {
			return t
		}
	}
	return ""
}
