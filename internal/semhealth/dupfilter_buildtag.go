package semhealth

import (
	"bufio"
	"go/build/constraint"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/go-code/internal/embeddings"
)

// maxConstraintHeaderBytes bounds how far into a file we scan for the build
// constraint. Go requires //go:build / // +build to appear in the file's
// leading comment block (before the package clause), so a few KiB is ample and
// caps the cost of a malformed/huge file.
const maxConstraintHeaderBytes = 16 * 1024

// constraintScanBufBytes is the initial scanner buffer; it grows up to
// maxConstraintHeaderBytes for long leading comment blocks.
const constraintScanBufBytes = 4096

// filterBuildTagVariants drops pairs whose two .go files carry mutually-exclusive
// build constraints (e.g. //go:build linux vs //go:build !linux). Such files
// never compile together — at most one symbol exists per build target — so the
// pair is the platform-split idiom, not a refactor-worthy duplicate.
//
// root is the on-disk repo root used to read each file's leading build
// constraint. When root is empty (e.g. unit tests, or a graph-only invocation
// with no checkout), the filter is a no-op — same graceful-degradation contract
// as the graph filters: a missing input must not silently hide real duplicates.
//
// The filter is Go-specific: a pair with any non-.go endpoint is kept (other
// languages have no //go:build semantics). Files with no constraint, or with
// constraints that are NOT provably disjoint, are kept.
func filterBuildTagVariants(root string, pairs []embeddings.SimilarPair) (kept []embeddings.SimilarPair, dropped int) {
	if root == "" || len(pairs) == 0 {
		return pairs, 0
	}

	// Memoize per-file constraint parse — many pairs share a file.
	cache := make(map[string]constraint.Expr)
	exprFor := func(file string) constraint.Expr {
		if e, ok := cache[file]; ok {
			return e
		}
		e := readBuildConstraint(filepath.Join(root, file))
		cache[file] = e
		return e
	}

	for _, p := range pairs {
		if isGoFile(p.FileA) && isGoFile(p.FileB) {
			ea, eb := exprFor(p.FileA), exprFor(p.FileB)
			if ea != nil && eb != nil && constraintsDisjoint(ea, eb) {
				dropped++
				continue
			}
		}
		kept = append(kept, p)
	}
	return kept, dropped
}

// isGoFile reports whether path is a non-test Go source file. Test files are
// already removed by filterTests, but the guard keeps this filter self-contained.
func isGoFile(path string) bool {
	return strings.HasSuffix(path, ".go")
}

// readBuildConstraint reads the leading build-constraint expression from a Go
// file, or nil when the file has no constraint or cannot be read. Read errors
// return nil (treated as "unconstrained") so an unreadable file never causes a
// pair to be dropped.
//
// A failed open is logged at Debug (not silent): when the on-disk root is wrong
// for a remote/cloned repo every open fails, the filter degrades to zero drops,
// and the only signal that this happened is the absence of platform-split
// suppression. The keep-on-error semantics are unchanged — keep is the safe
// direction — but the Debug line makes the misconfiguration observable.
func readBuildConstraint(absPath string) constraint.Expr {
	f, err := os.Open(absPath) //nolint:gosec // path is repo-root-joined, operator-supplied repo
	if err != nil {
		slog.Debug("dupfilter buildtag: open failed, treating file as unconstrained",
			slog.String("path", absPath), slog.Any("error", err))
		return nil
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, constraintScanBufBytes), maxConstraintHeaderBytes)
	read := 0
	for sc.Scan() {
		line := sc.Text()
		read += len(line) + 1
		if read > maxConstraintHeaderBytes {
			break
		}
		trimmed := strings.TrimSpace(line)
		// The constraint lives in the leading comment block. Stop at the package
		// clause — a constraint after it is not a build constraint.
		if strings.HasPrefix(trimmed, "package ") {
			break
		}
		if constraint.IsGoBuild(line) || constraint.IsPlusBuild(line) {
			if expr, err := constraint.Parse(line); err == nil {
				return expr
			}
		}
	}
	return nil
}

// maxConstraintTags caps the combined tag-space enumeration in
// constraintsDisjoint. Platform splits reference 1-3 tags; a pathological file
// with many distinct tags falls back to "not provably disjoint" (pair kept)
// rather than enumerating 2^n assignments.
const maxConstraintTags = 12

// goosTags and goarchTags are the mutually-exclusive GOOS/GOARCH build tags
// as reported by `go tool dist list`. A valid build target has at most one GOOS
// and one GOARCH tag true, so any truth assignment with two GOOS (or two GOARCH)
// tags set is impossible and is skipped during enumeration.
var (
	goosTags = map[string]bool{
		"aix": true, "android": true, "darwin": true, "dragonfly": true, "freebsd": true,
		"illumos": true, "ios": true, "js": true, "linux": true, "netbsd": true,
		"openbsd": true, "plan9": true, "solaris": true, "wasip1": true, "windows": true,
	}
	goarchTags = map[string]bool{
		"386": true, "amd64": true, "arm": true, "arm64": true, "loong64": true,
		"mips": true, "mips64": true, "mips64le": true, "mipsle": true, "ppc64": true,
		"ppc64le": true, "riscv64": true, "s390x": true, "wasm": true,
	}
)

// constraintsDisjoint reports whether two build-constraint expressions can never
// both be satisfied by the same build target — i.e. whether `a ∧ b` is
// unsatisfiable over the build-tag space.
//
// It enumerates every truth assignment over the UNION of tags referenced by both
// expressions and respects GOOS/GOARCH mutex: if no assignment satisfies both,
// the files never compile together and the pair is the platform-split idiom
// (linux vs !linux, windows vs darwin, etc.), not a duplicate. The tag union for
// real platform splits is tiny (≤3); the maxConstraintTags guard bounds the worst
// case to a "keep the pair" fallback.
func constraintsDisjoint(a, b constraint.Expr) bool {
	tags, unknown := unionTags(a, b)
	// An unrecognized constraint.Expr variant means unionTags could not collect
	// every tag, so the truth-assignment enumeration below would be unsound and
	// might wrongly declare the pair disjoint (over-suppression). Bail to "not
	// disjoint" — the safe direction is always to keep the pair.
	if unknown {
		return false
	}
	if len(tags) == 0 || len(tags) > maxConstraintTags {
		return false
	}

	n := len(tags)
	for mask := 0; mask < (1 << n); mask++ {
		assign := func(t string) bool {
			for i, tag := range tags {
				if tag == t {
					return mask&(1<<i) != 0
				}
			}
			return false
		}
		if !isMutexConsistent(assign, tags) {
			continue
		}
		if a.Eval(assign) && b.Eval(assign) {
			return false // a satisfying assignment for both → not disjoint
		}
	}
	return true
}

// isMutexConsistent reports whether a truth assignment respects the GOOS/GOARCH
// mutex: at most one GOOS tag and at most one GOARCH tag may be true.
func isMutexConsistent(assign func(string) bool, tags []string) bool {
	var goos, goarch int
	for _, tag := range tags {
		if !assign(tag) {
			continue
		}
		if goosTags[tag] {
			goos++
			if goos > 1 {
				return false
			}
		}
		if goarchTags[tag] {
			goarch++
			if goarch > 1 {
				return false
			}
		}
	}
	return true
}

// unionTags returns the distinct build tags referenced by either expression and
// whether an unrecognized constraint.Expr variant was encountered. The four
// concrete variants (TagExpr/NotExpr/AndExpr/OrExpr) are the complete set in
// go/build/constraint as of Go 1.x; the default arm future-proofs against a new
// variant by signalling unknown=true so the caller keeps the pair rather than
// reasoning over an incomplete tag set (over-suppression).
func unionTags(a, b constraint.Expr) (tags []string, unknown bool) {
	seen := make(map[string]bool)
	var out []string
	var walk func(constraint.Expr)
	walk = func(x constraint.Expr) {
		switch v := x.(type) {
		case *constraint.TagExpr:
			if !seen[v.Tag] {
				seen[v.Tag] = true
				out = append(out, v.Tag)
			}
		case *constraint.NotExpr:
			walk(v.X)
		case *constraint.AndExpr:
			walk(v.X)
			walk(v.Y)
		case *constraint.OrExpr:
			walk(v.X)
			walk(v.Y)
		default:
			unknown = true
		}
	}
	walk(a)
	walk(b)
	return out, unknown
}
