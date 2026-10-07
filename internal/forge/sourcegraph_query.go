package forge

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// sgNativeQualifiers pass through to Sourcegraph unchanged.
var sgNativeQualifiers = map[string]struct{}{
	"repo": {}, "file": {}, "lang": {}, "content": {}, "type": {},
	"case": {}, "count": {}, "timeout": {}, "context": {}, "select": {},
	"patterntype": {}, "visibility": {}, "fork": {}, "archived": {},
	"rev": {}, "repo-deps": {}, "repohasfile": {}, "repohascommitafter": {},
}

// sgDroppedQualifiers are GitHub-only qualifiers Sourcegraph rejects —
// dropped rather than passed through (they would error the whole query).
var sgDroppedQualifiers = map[string]struct{}{
	"is": {}, "in": {}, "sort": {}, "license": {}, "created": {},
	"pushed": {}, "size": {}, "stars": {}, "followers": {}, "topics": {},
	"filename":  {}, // handled explicitly below — kept here as a guard
	"extension": {},
	"ext":       {},
	"language":  {},
	"path":      {},
	"symbol":    {},
	"org":       {},
	"user":      {},
	"owner":     {},
}

// sgRepoValueRe detects a plain owner/repo slug (no host, no regex).
var sgRepoValueRe = regexp.MustCompile(`^[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$`)

// buildSourcegraphQuery translates the GitHub-style query plus options into
// a Sourcegraph query. context:global scopes to the whole index.
func buildSourcegraphQuery(query string, repos []string, opt SearchCodeOptions, count int) (string, error) {
	q := translateCodeQueryToSG(query)

	for _, r := range repos {
		slug, err := NormalizeGitHubRepo(r)
		if err != nil {
			return "", fmt.Errorf("invalid repo %q: %w", r, err)
		}
		q = appendQualifier(q, "repo", "^github\\.com/"+regexp.QuoteMeta(slug)+"$")
	}
	for _, r := range opt.ExcludeRepos {
		slug, err := NormalizeGitHubRepo(r)
		if err != nil {
			return "", fmt.Errorf("invalid repo %q: %w", r, err)
		}
		q = appendQualifier(q, "-repo", "^github\\.com/"+regexp.QuoteMeta(slug)+"$")
	}
	if opt.Language != "" && !hasQualifier(q, "lang", opt.Language) {
		q = appendQualifier(q, "lang", opt.Language)
	}
	for _, ext := range opt.FileExtensions {
		ext = strings.TrimPrefix(strings.ToLower(ext), ".")
		if ext == "" {
			continue
		}
		q = appendQualifier(q, "file", `\.`+regexp.QuoteMeta(ext)+`$`)
	}
	for _, p := range opt.ExcludePaths {
		p = strings.Trim(strings.TrimSpace(p), `"`)
		if p == "" {
			continue
		}
		if !pathQualRe.MatchString(p) {
			return "", fmt.Errorf("invalid exclude path %q: only [A-Za-z0-9._*/-] allowed", p)
		}
		q = appendQualifier(q, "-file", globToSGRegex(p))
	}
	if count > 0 && !hasQualifier(q, "count", strconv.Itoa(count)) {
		q = appendQualifier(q, "count", strconv.Itoa(count))
	}
	return "context:global " + q, nil
}

// translateCodeQueryToSG rewrites GitHub code-search qualifiers into their
// Sourcegraph equivalents. GitHub-only qualifiers are dropped; bare terms,
// /regex/, boolean operators and quoted phrases pass through unchanged.
func translateCodeQueryToSG(query string) string {
	var out []string
	for _, tok := range queryTokenRe.FindAllString(query, -1) {
		if rewritten := translateQueryToken(tok); rewritten != "" {
			out = append(out, rewritten)
		}
	}
	return strings.Join(out, " ")
}

// translateQueryToken rewrites one query token; returns "" to drop it.
func translateQueryToken(tok string) string {
	neg := strings.HasPrefix(tok, "-")
	body := strings.TrimPrefix(tok, "-")

	colon := strings.Index(body, ":")
	if colon <= 0 {
		return tok // bare term, /regex/, quoted phrase, boolean op
	}
	qual := strings.ToLower(body[:colon])
	val := strings.Trim(body[colon+1:], `"`)

	if qual == "repo" {
		return emitSG(neg, "repo", sgRepoValue(val))
	}
	if _, ok := sgNativeQualifiers[qual]; ok {
		return tok
	}
	if _, ok := sgDroppedQualifiers[qual]; ok {
		return translateGHQualifier(qual, val, neg)
	}
	// Unknown qualifiers are dropped: Sourcegraph errors the whole query on
	// unrecognised filters.
	return ""
}

// translateGHQualifier maps a known GitHub qualifier to Sourcegraph syntax.
// Returns "" for qualifiers with no SG equivalent (is:, sort:, …).
func translateGHQualifier(qual, val string, neg bool) string {
	switch qual {
	case "language":
		return emitSG(neg, "lang", val)
	case "path":
		return emitSG(neg, "file", globToSGRegex(val))
	case "extension", "ext":
		return emitSG(neg, "file", `\.`+regexp.QuoteMeta(val)+`$`)
	case "filename":
		return emitSG(neg, "file", `(^|/)`+regexp.QuoteMeta(val)+`$`)
	case "symbol":
		return emitSG(neg, "type", "symbol") + " " + val
	case "org", "user", "owner":
		return emitSG(neg, "repo", "^github\\.com/"+regexp.QuoteMeta(val)+"/")
	}
	return "" // is:, in:, sort:, license:, created:, pushed:, size:, stars:, followers:, topics:
}

// emitSG renders a qualifier token, honouring negation.
func emitSG(neg bool, qual, val string) string {
	p := ""
	if neg {
		p = "-"
	}
	if strings.ContainsAny(val, " \t") {
		return p + qual + `:"` + val + `"`
	}
	return p + qual + ":" + val
}

// sgRepoValue maps a repo: value to Sourcegraph form: plain owner/repo slugs
// become an anchored github.com regex; host-qualified or regex values pass.
func sgRepoValue(val string) string {
	if sgRepoValueRe.MatchString(val) {
		return "^github\\.com/" + regexp.QuoteMeta(val) + "$"
	}
	return val
}

// globToSGRegex converts a GitHub path glob ("vendor", "src/*.js",
// "*.generated.go") into a Sourcegraph file: regex. Unanchored, matching
// GitHub's substring path semantics: `*` crosses path segments here since
// GitHub `path:` is substring-ish — `*.x` → `.*x` is equivalent unanchored.
func globToSGRegex(glob string) string {
	var b strings.Builder
	for _, r := range glob {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	return b.String()
}
