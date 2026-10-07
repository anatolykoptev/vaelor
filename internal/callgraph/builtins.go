package callgraph

// builtinCallNames lists, per language, unqualified call names that are
// language builtins or universal type conversions — never the target of a
// project symbol that happens to share the name. The global name-match
// tier of resolveCall skips these (issue #793): without the guard, every
// bare `len(x)` bound to whichever repo method was called `len`, and that
// symbol collected thousands of phantom CALLS edges — the observed
// PageRank/who_calls/surprises poison.
//
// Only UNQUALIFIED calls are filtered (cs.Receiver == ""): `x.len()` is a
// real method call and must still resolve. Same-file and same-package
// (directory) resolution run BEFORE this guard, so a repo that genuinely
// shadows a builtin in its own package still resolves correctly — the
// guard only removes the cross-package guess.
//
// Unknown/missing languages get no filter — dropping edges requires
// confidence, not a guess. Type-conversion names are included (archlint's
// resolver does the same) because `string(b)`, `int(x)` etc. are builtins
// in precisely the same sense.
var builtinCallNames = map[string]map[string]bool{
	"go": {
		// builtins
		"append": true, "cap": true, "clear": true, "close": true,
		"complex": true, "copy": true, "delete": true, "imag": true,
		"len": true, "make": true, "new": true, "panic": true,
		"print": true, "println": true, "real": true, "recover": true,
		// type conversions
		"any": true, "bool": true, "byte": true, "comparable": true,
		"complex64": true, "complex128": true, "error": true,
		"float32": true, "float64": true, "int": true, "int8": true,
		"int16": true, "int32": true, "int64": true, "rune": true,
		"string": true, "uint": true, "uint8": true, "uint16": true,
		"uint32": true, "uint64": true, "uintptr": true,
	},
	"python": {
		"abs": true, "all": true, "any": true, "bin": true, "bool": true,
		"bytearray": true, "bytes": true, "callable": true, "chr": true,
		"complex": true, "dict": true, "dir": true, "divmod": true,
		"enumerate": true, "eval": true, "exec": true, "filter": true,
		"float": true, "format": true, "frozenset": true, "getattr": true,
		"hasattr": true, "hash": true, "hex": true, "id": true,
		"input": true, "int": true, "isinstance": true, "issubclass": true,
		"iter": true, "len": true, "list": true, "map": true,
		"max": true, "memoryview": true, "min": true, "next": true,
		"object": true, "oct": true, "open": true, "ord": true,
		"pow": true, "print": true, "range": true, "repr": true,
		"reversed": true, "round": true, "set": true, "setattr": true,
		"slice": true, "sorted": true, "str": true, "sum": true,
		"super": true, "tuple": true, "type": true, "vars": true,
		"zip": true,
	},
	"javascript": jsBuiltinCallNames,
	"typescript": jsBuiltinCallNames,
}

// jsBuiltinCallNames covers the global-function/conversion namespace both
// JS grammars share (TypeScript files resolve via DetectLanguageFromPath to
// "typescript" but carry the same globals).
var jsBuiltinCallNames = map[string]bool{
	"Array": true, "BigInt": true, "Boolean": true, "Number": true,
	"Object": true, "String": true, "Symbol": true,
	"decodeURI": true, "decodeURIComponent": true,
	"encodeURI": true, "encodeURIComponent": true,
	"eval": true, "isFinite": true, "isNaN": true,
	"parseFloat": true, "parseInt": true,
	"setTimeout": true, "setInterval": true,
	"clearTimeout": true, "clearInterval": true,
	"require": true, "queueMicrotask": true,
}

// isBuiltinCallName reports whether an unqualified call `name(...)` in a
// file of language lang refers to a language builtin/type conversion
// rather than any project symbol.
func isBuiltinCallName(lang, name string) bool {
	set, ok := builtinCallNames[lang]
	return ok && set[name]
}
