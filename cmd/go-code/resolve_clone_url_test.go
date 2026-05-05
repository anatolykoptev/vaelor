package main

import (
	"strings"
	"testing"

	"github.com/anatolykoptev/go-code/internal/forge"
)

// TestResolverCloneURL is an integration guard for the resolver pipeline.
// It verifies the chain forge.IsRemote → ExtractSlug → DetectForge → CloneURL
// for the bare-host form "github.com/owner/repo" that caused the double-host
// bug: CloneURL was producing "https://github.com/github.com/owner/repo.git"
// because ExtractSlug returned "github.com/owner/repo" verbatim instead of
// "owner/repo".
func TestResolverCloneURL(t *testing.T) {
	inputs := []struct {
		name  string
		input string
	}{
		{"bare host-prefix (primary regression)", "github.com/anatolykoptev/go-code"},
		{"bare slug", "anatolykoptev/go-code"},
		{"https URL", "https://github.com/anatolykoptev/go-code"},
		{"https URL with .git", "https://github.com/anatolykoptev/go-code.git"},
		{"ssh form", "git@github.com:anatolykoptev/go-code.git"},
	}

	for _, tc := range inputs {
		t.Run(tc.name, func(t *testing.T) {
			// Step 1: IsRemote must return true.
			if !forge.IsRemote(tc.input) {
				t.Fatalf("IsRemote(%q) = false, want true", tc.input)
			}

			// Step 2: ExtractSlug must return the clean "owner/repo" slug.
			slug, ok := forge.ExtractSlug(tc.input)
			if !ok {
				t.Fatalf("ExtractSlug(%q): ok = false, want true", tc.input)
			}
			if slug != "anatolykoptev/go-code" {
				t.Fatalf("ExtractSlug(%q) = %q, want %q", tc.input, slug, "anatolykoptev/go-code")
			}

			// Step 3: DetectForge must return GitHub.
			kind := forge.DetectForge(tc.input)
			if kind != forge.GitHub {
				t.Fatalf("DetectForge(%q) = %v, want GitHub", tc.input, kind)
			}

			// Step 4: CloneURL must produce the correct HTTPS URL.
			cloneURL := forge.CloneURL(kind, slug, "", "")
			if cloneURL != "https://github.com/anatolykoptev/go-code.git" {
				t.Fatalf("CloneURL = %q, want %q", cloneURL, "https://github.com/anatolykoptev/go-code.git")
			}

			// Step 5: Regression guard — must never contain the doubled host.
			if strings.Contains(cloneURL, "github.com/github.com/") {
				t.Fatalf("CloneURL contains doubled host: %q", cloneURL)
			}
		})
	}
}
