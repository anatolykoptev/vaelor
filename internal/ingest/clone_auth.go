package ingest

import (
	"net/url"
	"regexp"
	"strings"
)

// safeHostRE bounds what may be interpolated into the helper script.
var safeHostRE = regexp.MustCompile(`^[A-Za-z0-9.\-:\[\]]+$`)

// defaultAuthUser is the HTTP Basic username GitHub expects for installation
// tokens and PATs.
const defaultAuthUser = "x-access-token"

// defaultAuthHost is the host the default (credential-free) clone URL points
// at, and therefore the host a token is assumed to be issued by when the
// caller does not say otherwise.
const defaultAuthHost = "github.com"

// Environment variables carrying the credential to the git credential helper.
// They hold the secret only for the lifetime of the git child process.
const (
	envAuthUser  = "VAELOR_GIT_USER"
	envAuthToken = "VAELOR_GIT_TOKEN" //nolint:gosec // env var name, not a credential
)

// credentialHelper builds the inline git credential helper for host (the
// "host[:port]" git passes in the credential request). It answers only a
// "get" for protocol=https and exactly that host, so a redirect to any other
// host receives nothing, and it never prints the secret anywhere but git's
// credential pipe.
func credentialHelper(host string) string {
	return `!f() { test "$1" = get || exit 0; p=; h=; ` +
		`while IFS== read -r k v; do case "$k" in protocol) p=$v;; host) h=$v;; esac; done; ` +
		`[ "$p" = https ] && [ "$h" = '` + host + `' ] || exit 0; ` +
		`printf 'username=%s\npassword=%s\n' "$` + envAuthUser + `" "$` + envAuthToken + `"; }; f`
}

// gitAuthEnv returns the environment that authenticates git HTTPS requests to
// remoteURL with token, or nil when nothing may be injected: empty token, a
// non-https remote, or a remote whose host is not issuerHost.
//
// Invariant: a credential is only ever sent to the host that issued it.
// issuerHost is the host the token belongs to; a mismatch is refused here so
// no caller wiring can send a token elsewhere.
//
// The credential is delivered through a host-scoped credential helper
// (credential.https://<host>.helper), not an http.extraheader and never a URL:
// git echoes URLs in its errors, and applies extra headers to every request in
// the process including those following a redirect to another host, whereas
// the helper is consulted per request host. Nothing is written to .git/config.
//
// This is the single place that constructs git clone/fetch credentials.
func gitAuthEnv(remoteURL, user, token, issuerHost string) []string {
	if token == "" || issuerHost == "" {
		return nil
	}
	u, err := url.Parse(remoteURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || !strings.EqualFold(u.Host, issuerHost) || !safeHostRE.MatchString(u.Host) {
		return nil
	}
	if user == "" {
		user = defaultAuthUser
	}
	return []string{
		"GIT_CONFIG_COUNT=2",
		// An empty value resets the inherited helper list so a host helper
		// (e.g. the macOS keychain) neither answers for nor stores this token.
		"GIT_CONFIG_KEY_0=credential.helper",
		"GIT_CONFIG_VALUE_0=",
		"GIT_CONFIG_KEY_1=credential.https://" + u.Host + ".helper",
		"GIT_CONFIG_VALUE_1=" + credentialHelper(u.Host),
		envAuthUser + "=" + user,
		envAuthToken + "=" + token,
		// Defence-in-depth: suppress git trace channels.
		"GIT_TRACE=0",
		"GIT_CURL_VERBOSE=0",
	}
}
