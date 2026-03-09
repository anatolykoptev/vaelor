package freshness

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// GoRegistry queries the Go module proxy.
type GoRegistry struct {
	BaseURL string
	Client  *http.Client
}

// Latest fetches the latest version from the Go proxy.
func (r *GoRegistry) Latest(ctx context.Context, name string) (string, error) {
	url := fmt.Sprintf("%s/%s/@latest", r.BaseURL, name)
	var resp struct{ Version string }
	if err := registryGet(ctx, r.Client, url, &resp); err != nil {
		return "", err
	}
	return resp.Version, nil
}

// NpmRegistry queries the npm registry.
type NpmRegistry struct {
	BaseURL string
	Client  *http.Client
}

// Latest fetches the latest version from npm.
func (r *NpmRegistry) Latest(ctx context.Context, name string) (string, error) {
	url := fmt.Sprintf("%s/%s/latest", r.BaseURL, name)
	var resp struct{ Version string }
	if err := registryGet(ctx, r.Client, url, &resp); err != nil {
		return "", err
	}
	return resp.Version, nil
}

// PyPIRegistry queries the Python Package Index.
type PyPIRegistry struct {
	BaseURL string
	Client  *http.Client
}

// Latest fetches the latest version from PyPI.
func (r *PyPIRegistry) Latest(ctx context.Context, name string) (string, error) {
	url := fmt.Sprintf("%s/pypi/%s/json", r.BaseURL, name)
	var resp struct {
		Info struct{ Version string } `json:"info"`
	}
	if err := registryGet(ctx, r.Client, url, &resp); err != nil {
		return "", err
	}
	return resp.Info.Version, nil
}

// CratesRegistry queries the crates.io registry.
type CratesRegistry struct {
	BaseURL string
	Client  *http.Client
}

// Latest fetches the latest stable version from crates.io.
func (r *CratesRegistry) Latest(ctx context.Context, name string) (string, error) {
	url := fmt.Sprintf("%s/api/v1/crates/%s", r.BaseURL, name)
	var resp struct {
		Crate struct {
			MaxStableVersion string `json:"max_stable_version"`
		} `json:"crate"`
	}
	if err := registryGet(ctx, r.Client, url, &resp); err != nil {
		return "", err
	}
	return resp.Crate.MaxStableVersion, nil
}

// MavenRegistry queries Maven Central.
type MavenRegistry struct {
	BaseURL string
	Client  *http.Client
}

// Latest fetches the latest version from Maven Central.
// Name must be in "groupId:artifactId" format.
func (r *MavenRegistry) Latest(ctx context.Context, name string) (string, error) {
	group, artifact, ok := strings.Cut(name, ":")
	if !ok {
		return "", fmt.Errorf("maven: invalid name %q, expected group:artifact", name)
	}
	url := fmt.Sprintf("%s/solrsearch/select?q=g:%s+AND+a:%s&rows=1&wt=json",
		r.BaseURL, group, artifact)
	var resp struct {
		Response struct {
			Docs []struct {
				LatestVersion string `json:"latestVersion"`
			} `json:"docs"`
		} `json:"response"`
	}
	if err := registryGet(ctx, r.Client, url, &resp); err != nil {
		return "", err
	}
	if len(resp.Response.Docs) == 0 {
		return "", fmt.Errorf("maven: no results for %q", name)
	}
	return resp.Response.Docs[0].LatestVersion, nil
}

// RubyGemsRegistry queries the RubyGems API.
type RubyGemsRegistry struct {
	BaseURL string
	Client  *http.Client
}

// Latest fetches the latest version from RubyGems.
func (r *RubyGemsRegistry) Latest(ctx context.Context, name string) (string, error) {
	url := fmt.Sprintf("%s/api/v1/gems/%s.json", r.BaseURL, name)
	var resp struct{ Version string }
	if err := registryGet(ctx, r.Client, url, &resp); err != nil {
		return "", err
	}
	return resp.Version, nil
}

// NuGetRegistry queries the NuGet v3 flat container API.
type NuGetRegistry struct {
	BaseURL string
	Client  *http.Client
}

// Latest fetches the latest version from NuGet.
func (r *NuGetRegistry) Latest(ctx context.Context, name string) (string, error) {
	lower := strings.ToLower(name)
	url := fmt.Sprintf("%s/v3-flatcontainer/%s/index.json", r.BaseURL, lower)
	var resp struct{ Versions []string }
	if err := registryGet(ctx, r.Client, url, &resp); err != nil {
		return "", err
	}
	if len(resp.Versions) == 0 {
		return "", fmt.Errorf("nuget: no versions for %q", name)
	}
	return resp.Versions[len(resp.Versions)-1], nil
}

// registryGet performs a GET request and decodes JSON into dest.
func registryGet(ctx context.Context, client *http.Client, url string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("registry returned %d for %s", resp.StatusCode, url)
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("decoding response from %s: %w", url, err)
	}
	return nil
}
