package freshness

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// DefaultGoDLURL is the Go download API endpoint.
const DefaultGoDLURL = "https://go.dev/dl/?mode=json"

// Runtime check timeout.
const runtimeCheckTimeout = 5 * time.Second

// goRelease represents a single Go release from the download API.
type goRelease struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

// CheckGoRuntime compares the go.mod Go version against the latest stable release.
// Returns a status string: "current", "outdated (have X, latest Y)", or empty on error.
func CheckGoRuntime(ctx context.Context, client *http.Client, goVersion string) string {
	if goVersion == "" {
		return ""
	}

	latest, err := fetchLatestGoVersion(ctx, client)
	if err != nil {
		return ""
	}

	return compareGoVersions(goVersion, latest)
}

// fetchLatestGoVersion fetches the latest stable Go version from go.dev.
func fetchLatestGoVersion(ctx context.Context, client *http.Client) (string, error) {
	checkCtx, cancel := context.WithTimeout(ctx, runtimeCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, DefaultGoDLURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating go dl request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching go versions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("go dl returned %d", resp.StatusCode)
	}

	var releases []goRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", fmt.Errorf("decoding go releases: %w", err)
	}

	for _, r := range releases {
		if r.Stable {
			return strings.TrimPrefix(r.Version, "go"), nil
		}
	}

	return "", errors.New("no stable Go releases found")
}

// compareGoVersions compares go.mod version against latest.
// Go versions use major.minor (e.g. "1.22") or major.minor.patch (e.g. "1.22.5").
func compareGoVersions(have, latest string) string {
	haveParts := parseSemver(have)
	latestParts := parseSemver(latest)

	if haveParts == nil || latestParts == nil {
		return ""
	}

	if haveParts[0] == latestParts[0] && haveParts[1] >= latestParts[1] {
		return verCurrent
	}

	return fmt.Sprintf("outdated (have %s, latest %s)", have, latest)
}
