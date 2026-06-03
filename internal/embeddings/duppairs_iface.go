package embeddings

import (
	"strings"
)

// ifacePairCols is the number of columns returned by the interface-sibling
// Symbol-vertex query: a.name, a.file, a.kind, a.signature, b.name, b.file,
// b.kind, b.signature.
const ifacePairCols = 8

// sigInfo holds the discriminator fields extracted from a Go function/method
// signature: whether it is a method, its receiver type (bare, pointer/name
// stripped), and a normalized form with the receiver clause removed so two
// implementations of the same interface method compare equal.
type sigInfo struct {
	isMethod bool   // true when the signature has a receiver clause "func (recv T) ..."
	receiver string // bare receiver type, e.g. "GitHubForge" (no '*', no recv ident)
	norm     string // signature with the receiver clause stripped, e.g. "func FetchREADME(...) (...)"
}

// parseSignature classifies a Go signature string for the interface-sibling
// discriminator. A method signature looks like:
//
//	func (g *GitHubForge) FetchREADME(ctx context.Context, slug string) (string, error)
//
// parseSignature returns isMethod=true with receiver="GitHubForge" and a norm
// form "func FetchREADME(ctx context.Context, slug string) (string, error)".
// A free function ("func countSourceFiles(...) int") returns isMethod=false.
//
// Non-Go or unparseable signatures return the zero sigInfo (isMethod=false),
// which means the receiver-discriminator never fires for them — a safe default
// that keeps the pair (no over-suppression).
func parseSignature(sig string) sigInfo {
	const funcKw = "func "
	rest, ok := strings.CutPrefix(sig, funcKw)
	if !ok {
		return sigInfo{}
	}
	// Only a method has a receiver clause "(recv T)" immediately after "func ".
	if !strings.HasPrefix(rest, "(") {
		return sigInfo{}
	}
	close := strings.IndexByte(rest, ')')
	if close < 0 {
		return sigInfo{}
	}
	recvClause := rest[1:close] // e.g. "g *GitHubForge" or "*GitHubForge" or "GitHubForge"
	afterRecv := rest[close+1:] // e.g. " FetchREADME(ctx ...) (...)"
	recv := receiverTypeName(recvClause)
	if recv == "" {
		return sigInfo{}
	}
	return sigInfo{
		isMethod: true,
		receiver: recv,
		norm:     funcKw + strings.TrimSpace(afterRecv),
	}
}

// receiverTypeName extracts the bare receiver type from a Go receiver clause.
// Handles "g *GitHubForge" → "GitHubForge", "*GitHubForge" → "GitHubForge",
// "g GitHubForge" → "GitHubForge", "GitHubForge" → "GitHubForge", and generic
// receivers "g *Store[K, V]" → "Store" (instantiation args are dropped because
// the method name + normalized param list already pin the method identity).
func receiverTypeName(clause string) string {
	clause = strings.TrimSpace(clause)
	if clause == "" {
		return ""
	}
	// Strip generic instantiation FIRST so the "[K, V]" comma-space does not
	// confuse the identifier split below: "g *Cache[K, V]" → "g *Cache".
	if i := strings.IndexByte(clause, '['); i >= 0 {
		clause = strings.TrimSpace(clause[:i])
	}
	// Drop the optional receiver identifier: if there are two space-separated
	// fields, the type is the last one ("g *T" → "*T"); otherwise the single
	// field is the type ("*T").
	if fields := strings.Fields(clause); len(fields) >= 2 {
		clause = fields[len(fields)-1]
	}
	clause = strings.TrimPrefix(clause, "*")
	return strings.TrimSpace(clause)
}

// isInterfaceSiblingPair returns true when two symbols are interface-impl
// siblings rather than a refactor-worthy duplicate: both are methods, they
// share the same method name + identical receiver-stripped signature, and they
// sit on DISTINCT receiver types. This is the structural fingerprint of two
// concrete types satisfying the same interface (e.g. *GitHubForge.FetchREADME
// vs *GitLabForge.FetchREADME).
//
// Free functions (isMethod=false) never match, so genuine cross-package
// reinvention like two free countSourceFiles functions is preserved. Two
// methods on the SAME receiver type (overloaded-looking) also never match
// because the receiver types are not distinct.
func isInterfaceSiblingPair(aName string, aSig sigInfo, bName string, bSig sigInfo) bool {
	if !aSig.isMethod || !bSig.isMethod {
		return false
	}
	if aName != bName {
		return false
	}
	if aSig.receiver == "" || bSig.receiver == "" || aSig.receiver == bSig.receiver {
		return false
	}
	return aSig.norm == bSig.norm
}
