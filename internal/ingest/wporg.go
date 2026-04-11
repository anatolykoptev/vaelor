package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// wpPluginRe matches WordPress plugin references:
//   - wp:classic-editor
//   - wordpress.org/plugins/akismet
//   - https://wordpress.org/plugins/wordfence/
var wpPluginRe = regexp.MustCompile(
	`^(?:wp:|(?:https?://)?(?:www\.)?wordpress\.org/plugins/)([a-z0-9-]+)/?$`,
)

const (
	wpAPIURL        = "https://api.wordpress.org/plugins/info/1.2/?action=plugin_information&slug=%s"
	wpDownloadTimeout = 60 * time.Second
	wpMetaTimeout     = 15 * time.Second
	wpDirPrefix       = "wp_"
)

// WPPluginMeta holds metadata returned by the WordPress.org Plugin API.
type WPPluginMeta struct {
	Name           string `json:"name"`
	Slug           string `json:"slug"`
	Version        string `json:"version"`
	DownloadLink   string `json:"download_link"`
	Author         string `json:"author"`
	ActiveInstalls int    `json:"active_installs"`
}

// WPPluginOpts controls how a WordPress plugin is fetched.
type WPPluginOpts struct {
	Slug    string
	Version string // optional; empty = latest
	DestDir string
}

// IsWordPressPlugin returns true if the input looks like a WP plugin reference.
func IsWordPressPlugin(input string) bool {
	return wpPluginRe.MatchString(input)
}

// NormalizeWPSlug extracts the plugin slug from a wp: prefix or wordpress.org URL.
func NormalizeWPSlug(input string) (string, error) {
	m := wpPluginRe.FindStringSubmatch(input)
	if m == nil {
		return "", fmt.Errorf("invalid wordpress plugin reference: %q", input)
	}
	return m[1], nil
}

// WPSearchResult holds one plugin from a search response.
type WPSearchResult struct {
	Name             string `json:"name"`
	Slug             string `json:"slug"`
	Version          string `json:"version"`
	Author           string `json:"author"`
	Rating           int    `json:"rating"`
	ActiveInstalls   int    `json:"active_installs"`
	ShortDescription string `json:"short_description"`
	DownloadLink     string `json:"download_link"`
}

// wpSearchInfo holds pagination metadata from the WP search API.
type wpSearchInfo struct {
	Page    int `json:"page"`
	Pages   int `json:"pages"`
	Results int `json:"results"`
}

// WPSearchResponse is the top-level response from the query_plugins API.
type WPSearchResponse struct {
	Info    wpSearchInfo     `json:"info"`
	Page    int              `json:"-"`
	Pages   int              `json:"-"`
	Plugins []WPSearchResult `json:"plugins"`
}

const wpSearchURL = "https://api.wordpress.org/plugins/info/1.2/?action=query_plugins&search=%s&per_page=%d&page=%d"

// SearchWPPlugins queries the WordPress.org plugin directory.
func SearchWPPlugins(ctx context.Context, query string, perPage, page int) (*WPSearchResponse, error) {
	if perPage <= 0 || perPage > 20 {
		perPage = 10
	}
	if page <= 0 {
		page = 1
	}

	ctx, cancel := context.WithTimeout(ctx, wpMetaTimeout)
	defer cancel()

	url := fmt.Sprintf(wpSearchURL, url.QueryEscape(query), perPage, page)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create search request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search wp plugins: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wordpress search API returned %d", resp.StatusCode)
	}

	var result WPSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode search response: %w", err)
	}
	result.Page = result.Info.Page
	result.Pages = result.Info.Pages
	return &result, nil
}

// FetchWPPluginMeta retrieves plugin metadata from the WordPress.org API.
func FetchWPPluginMeta(ctx context.Context, slug string) (*WPPluginMeta, error) {
	ctx, cancel := context.WithTimeout(ctx, wpMetaTimeout)
	defer cancel()

	url := fmt.Sprintf(wpAPIURL, slug)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch plugin meta: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wordpress API returned %d for slug %q", resp.StatusCode, slug)
	}

	var meta WPPluginMeta
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("decode plugin meta: %w", err)
	}
	if meta.DownloadLink == "" {
		return nil, fmt.Errorf("plugin %q has no download_link (may not exist)", slug)
	}
	return &meta, nil
}

// FetchWPPlugin downloads and extracts a WordPress plugin ZIP into DestDir.
// Returns a CloneResult with LocalPath pointing to the extracted directory.
func FetchWPPlugin(ctx context.Context, opts WPPluginOpts) (*CloneResult, error) {
	destDir := filepath.Join(opts.DestDir, wpDirPrefix+opts.Slug)

	// Cache hit: already downloaded.
	if _, err := os.Stat(destDir); err == nil {
		return &CloneResult{LocalPath: destDir}, nil
	}

	meta, err := FetchWPPluginMeta(ctx, opts.Slug)
	if err != nil {
		return nil, err
	}

	dlURL := meta.DownloadLink
	if opts.Version != "" && opts.Version != meta.Version {
		dlURL = fmt.Sprintf("https://downloads.wordpress.org/plugin/%s.%s.zip", opts.Slug, opts.Version)
	}

	zipPath, err := downloadZIP(ctx, dlURL, opts.DestDir, opts.Slug)
	if err != nil {
		return nil, err
	}
	defer os.Remove(zipPath)

	if err := extractZIP(zipPath, opts.DestDir); err != nil {
		return nil, fmt.Errorf("extract zip: %w", err)
	}

	// WordPress ZIPs extract into a directory named after the slug.
	// Rename to our wp_ prefixed name for consistency.
	rawDir := filepath.Join(opts.DestDir, opts.Slug)
	if _, err := os.Stat(rawDir); err == nil && rawDir != destDir {
		if err := os.Rename(rawDir, destDir); err != nil {
			return nil, fmt.Errorf("rename extracted dir: %w", err)
		}
	}

	return &CloneResult{LocalPath: destDir}, nil
}

