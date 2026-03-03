package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestIsWordPressPlugin(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"wp:classic-editor", true},
		{"wp:akismet", true},
		{"wp:woo-commerce-plugin", true},
		{"wordpress.org/plugins/akismet", true},
		{"wordpress.org/plugins/akismet/", true},
		{"https://wordpress.org/plugins/wordfence/", true},
		{"http://wordpress.org/plugins/wordfence", true},
		{"https://www.wordpress.org/plugins/jetpack/", true},
		// Non-WP inputs.
		{"owner/repo", false},
		{"https://github.com/owner/repo", false},
		{"/local/path", false},
		{"wp:", false},
		{"wordpress.org/themes/flavor", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsWordPressPlugin(tt.input); got != tt.want {
			t.Errorf("IsWordPressPlugin(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeWPSlug(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"wp:classic-editor", "classic-editor", false},
		{"wordpress.org/plugins/akismet/", "akismet", false},
		{"https://wordpress.org/plugins/wordfence/", "wordfence", false},
		{"owner/repo", "", true},
		{"not-a-plugin", "", true},
	}
	for _, tt := range tests {
		got, err := NormalizeWPSlug(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("NormalizeWPSlug(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("NormalizeWPSlug(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFetchWPPluginMeta(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	ctx := context.Background()
	meta, err := FetchWPPluginMeta(ctx, "classic-editor")
	if err != nil {
		t.Fatalf("FetchWPPluginMeta(classic-editor): %v", err)
	}
	if meta.Slug != "classic-editor" {
		t.Errorf("slug = %q, want classic-editor", meta.Slug)
	}
	if meta.DownloadLink == "" {
		t.Error("download_link is empty")
	}
	if meta.Version == "" {
		t.Error("version is empty")
	}
}

func TestFetchWPPluginMeta_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	ctx := context.Background()
	_, err := FetchWPPluginMeta(ctx, "this-plugin-does-not-exist-99999")
	if err == nil {
		t.Error("expected error for non-existent plugin, got nil")
	}
}

func TestFetchWPPlugin(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	ctx := context.Background()
	destDir := t.TempDir()

	result, err := FetchWPPlugin(ctx, WPPluginOpts{
		Slug:    "classic-editor",
		DestDir: destDir,
	})
	if err != nil {
		t.Fatalf("FetchWPPlugin: %v", err)
	}

	expectedDir := filepath.Join(destDir, "wp_classic-editor")
	if result.LocalPath != expectedDir {
		t.Errorf("LocalPath = %q, want %q", result.LocalPath, expectedDir)
	}

	// Verify PHP files exist in extracted directory.
	entries, err := os.ReadDir(result.LocalPath)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	hasPHP := false
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".php" {
			hasPHP = true
			break
		}
	}
	if !hasPHP {
		t.Error("no .php files found in extracted plugin directory")
	}

	// Second call should be a cache hit.
	result2, err := FetchWPPlugin(ctx, WPPluginOpts{
		Slug:    "classic-editor",
		DestDir: destDir,
	})
	if err != nil {
		t.Fatalf("FetchWPPlugin (cache): %v", err)
	}
	if result2.LocalPath != result.LocalPath {
		t.Errorf("cache hit returned different path: %q vs %q", result2.LocalPath, result.LocalPath)
	}
}
