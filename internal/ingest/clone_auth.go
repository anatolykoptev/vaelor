package ingest

import (
	"encoding/base64"
	"net/url"
)

// defaultAuthUser is the HTTP Basic username GitHub expects for installation
// tokens and PATs.
const defaultAuthUser = "x-access-token"

// gitAuthEnv returns the environment that authenticates git HTTP(S) requests
// to remoteURL's host with token, or nil when there is nothing to inject
// (empty token, non-HTTP remote). The credential travels as a per-process
// http.extraheader (GIT_CONFIG_COUNT), scoped to the remote's scheme+host, so
// it is never part of a URL — git echoes URLs in its error output — and never
// written to .git/config.
//
// This is the single place that constructs git clone/fetch credentials.
func gitAuthEnv(remoteURL, user, token string) []string {
	if token == "" {
		return nil
	}
	u, err := url.Parse(remoteURL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil
	}
	if user == "" {
		user = defaultAuthUser
	}
	cred := base64.StdEncoding.EncodeToString([]byte(user + ":" + token))
	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http." + u.Scheme + "://" + u.Host + "/.extraheader",
		"GIT_CONFIG_VALUE_0=Authorization: Basic " + cred,
		// Defence-in-depth: suppress git trace channels that may echo the
		// header into stderr.
		"GIT_TRACE=0",
		"GIT_CURL_VERBOSE=0",
	}
}
