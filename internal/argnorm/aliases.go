package argnorm

import "sort"

// toolNameAliases rewrites unambiguous tool-name aliases to their real
// registered name before dispatch. The agent-observed demand signal
// (issue #570): "github_repo_search" was called repeatedly but the real tool
// is "github_code_search".
var toolNameAliases = map[string]string{
	"github_repo_search": "github_code_search",
}

// ResolveToolName returns the canonical tool name for a (possibly aliased)
// requested name and whether a rewrite happened.
func ResolveToolName(name string) (canonical string, aliased bool) {
	if dst, ok := toolNameAliases[name]; ok {
		return dst, true
	}
	return name, false
}

// didYouMeanHints maps known demand-signal tool names that have NO real
// implementation to the nearest real capability. These are surfaced in the
// unknown-tool error so agents pivot to the right tool instead of retrying
// or treating the failure as a server connection drop (issue #570).
var didYouMeanHints = map[string]string{
	"find_bugs":        "debug_investigate",
	"flaky_tests":      "debug_investigate",
	"test_reliability": "code_health",
}

// aliasTargetsFor returns the per-tool alias renames that apply to a tool with
// the given accepted property set. Each entry is src→dst; a src is only
// applied when dst is an accepted property and src is NOT already accepted
// (so a tool that natively declares `limit` is left alone).
//
// Rules (issue #568):
//   - limit → max_results  (every tool that declares max_results)
//   - limit → top_k        (semantic_search declares top_k, not max_results)
//   - insights → repo      (remember_graph_insights: agents send `insights`
//     where the tool requires `repo`; 100% failure rate pre-fix)
func aliasTargetsFor(toolName string, accepted map[string]struct{}) map[string]string {
	out := map[string]string{}
	has := func(k string) bool { _, ok := accepted[k]; return ok }

	// limit → max_results (or top_k for semantic_search).
	if !has("limit") {
		switch {
		case has("max_results"):
			out["limit"] = "max_results"
		case toolName == "semantic_search" && has("top_k"):
			out["limit"] = "top_k"
		}
	}

	// remember_graph_insights: insights → repo.
	if toolName == "remember_graph_insights" && has("repo") && !has("insights") {
		out["insights"] = "repo"
	}

	return out
}

// DidYouMean returns up to maxSuggest closest registered tool names to the
// requested unknown name, plus any explicit hint for a known demand-signal
// name (find_bugs→debug_investigate, …). The hint, when present, is forced to
// the front of the result. Order is by ascending edit distance, ties broken
// by a prefix bonus then alphabetical.
func DidYouMean(name string, candidates []string, maxSuggest int) []string {
	if maxSuggest <= 0 {
		return nil
	}
	type cand struct {
		name   string
		score  int
		prefix bool
	}
	var ranked []cand
	for _, c := range candidates {
		d := levenshtein(name, c)
		prefix := false
		// Prefix or contained matches get a strong bonus so "github_repo_search"
		// surfaces "github_code_search" even though edit distance is moderate.
		if hasPrefixFold(name, c) || containsFold(c, name) {
			prefix = true
		}
		ranked = append(ranked, cand{name: c, score: d, prefix: prefix})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].prefix != ranked[j].prefix {
			return ranked[i].prefix
		}
		if ranked[i].score != ranked[j].score {
			return ranked[i].score < ranked[j].score
		}
		return ranked[i].name < ranked[j].name
	})

	// Force an explicit hint to the front when present.
	var hints []string
	if h, ok := didYouMeanHints[name]; ok {
		hints = append(hints, h)
	}

	out := append([]string{}, hints...)
	seen := map[string]bool{}
	for _, h := range hints {
		seen[h] = true
	}
	for _, r := range ranked {
		if seen[r.name] {
			continue
		}
		// Drop distant matches (distance > len(name)) to avoid noise — a
		// suggestion that needs more edits than the query has chars is not
		// "did you mean".
		if !r.prefix && r.score > len(name) {
			continue
		}
		out = append(out, r.name)
		seen[r.name] = true
		if len(out) >= maxSuggest {
			break
		}
	}
	return out
}

// hasPrefixFold reports whether a or b is a case-insensitive prefix of the
// other — useful for "repo_search" vs "Repo_Search".
func hasPrefixFold(a, b string) bool {
	la, lb := len(a), len(b)
	n := la
	if lb < n {
		n = lb
	}
	if n == 0 {
		return false
	}
	for i := 0; i < n; i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 32
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}

func containsFold(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	lh, ln := len(haystack), len(needle)
	if ln > lh {
		return false
	}
	for i := 0; i+ln <= lh; i++ {
		match := true
		for j := 0; j < ln; j++ {
			ch, cn := haystack[i+j], needle[j]
			if ch >= 'A' && ch <= 'Z' {
				ch += 32
			}
			if cn >= 'A' && cn <= 'Z' {
				cn += 32
			}
			if ch != cn {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// levenshtein computes the edit distance between a and b (case-insensitive).
// Bounded by the shorter length's trailing-char prune; sufficient for the
// small tool-name vocabulary (≈40 names).
func levenshtein(a, b string) int {
	a = foldASCII(a)
	b = foldASCII(b)
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	// Trim common prefix/suffix to shrink the matrix.
	for la > 0 && lb > 0 && a[0] == b[0] {
		a, b = a[1:], b[1:]
		la, lb = la-1, lb-1
	}
	for la > 0 && lb > 0 && a[la-1] == b[lb-1] {
		la, lb = la-1, lb-1
	}
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			m := del
			if ins < m {
				m = ins
			}
			if sub < m {
				m = sub
			}
			curr[j] = m
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func foldASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
