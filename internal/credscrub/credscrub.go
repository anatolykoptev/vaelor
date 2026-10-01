// Package credscrub masks credentials in free-form text (git output, tool
// errors) so they never reach logs or API responses.
package credscrub

import "regexp"

const mask = "***"

var (
	// scheme://userinfo@ — masks "user", "user:pass" and bare-token userinfo.
	userinfoRE = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.\-]*://)[^/\s@]+@`)
	// Well-known token shapes: GitHub (classic, App, OAuth, user/server/refresh),
	// GitHub fine-grained PATs, GitLab PATs.
	tokenRE = regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]+|github_pat_[A-Za-z0-9_]+|glpat-[A-Za-z0-9_\-]+`)
	// Authorization header values (Basic/Bearer).
	authHeaderRE = regexp.MustCompile(`(?i)(authorization:\s*(?:basic|bearer)\s+)\S+`)
)

// Scrub returns s with URL userinfo, token-shaped strings and Authorization
// header values replaced by "***".
func Scrub(s string) string {
	if s == "" {
		return s
	}
	s = userinfoRE.ReplaceAllString(s, "${1}"+mask+"@")
	s = authHeaderRE.ReplaceAllString(s, "${1}"+mask)
	return tokenRE.ReplaceAllString(s, mask)
}
