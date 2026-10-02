// Package credscrub masks credentials in free-form text (git output, tool
// results) so they never reach logs or API responses.
package credscrub

import "regexp"

const mask = "***"

var (
	// scheme://userinfo@ — masks "user", "user:pass" and bare-token userinfo.
	// Greedy up to the last '@' of the authority so an '@' inside a password is
	// covered; quotes, '<' '>' , whitespace and '/' end it, so it cannot span
	// JSON string boundaries or separate URLs.
	userinfoRE = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.\-]*://)[^/\s"'<>]+@`)
	// scp-like "user:secret@host:path" (a bare "git@host:path" has no colon
	// before the '@' and is left alone).
	scpRE = regexp.MustCompile(`(^|[\s'"(])[A-Za-z0-9._\-]+:[^\s@/'":]+@([A-Za-z0-9.\-]+:)`)
	// Well-known token shapes: GitHub (classic, App, OAuth, user/server/refresh),
	// GitHub fine-grained PATs, GitLab (PAT, runner, deploy, OAuth app secret,
	// pipeline trigger) tokens.
	tokenRE = regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]+|github_pat_[A-Za-z0-9_]+|gl(?:pat|rt|dt|oas|ptt)-[A-Za-z0-9_\-]+`)
	// JWTs (header always starts with {" → "eyJ").
	jwtRE = regexp.MustCompile(`eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]*`)
	// Authorization header values (Basic/Bearer/token).
	authHeaderRE = regexp.MustCompile(`(?i)(authorization:\s*(?:basic|bearer|token)\s+)\S+`)
	// Bare base64 Basic credentials as git builds them: base64 of
	// "x-access-token:" and "oauth2:" (prefix of the latter is alignment-safe).
	basicB64RE = regexp.MustCompile(`(?:eC1hY2Nlc3MtdG9rZW46|b2F1dGgyO)[A-Za-z0-9+/=_\-]*`)
	// Secrets passed to a credential helper or the git child's environment:
	// "password=<v>" (credential protocol) and "VAELOR_GIT_TOKEN=<v>".
	kvSecretRE = regexp.MustCompile(`(?i)\b((?:password|VAELOR_GIT_TOKEN)=)\S+`)
)

// Scrub returns s with URL userinfo, token-shaped strings, JWTs and
// Authorization credentials replaced by "***".
func Scrub(s string) string {
	if s == "" {
		return s
	}
	s = userinfoRE.ReplaceAllString(s, "${1}"+mask+"@")
	s = scpRE.ReplaceAllString(s, "${1}"+mask+"@${2}")
	s = authHeaderRE.ReplaceAllString(s, "${1}"+mask)
	s = basicB64RE.ReplaceAllString(s, mask)
	s = kvSecretRE.ReplaceAllString(s, "${1}"+mask)
	s = jwtRE.ReplaceAllString(s, mask)
	return tokenRE.ReplaceAllString(s, mask)
}
