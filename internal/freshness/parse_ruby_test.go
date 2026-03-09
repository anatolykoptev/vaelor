package freshness

import (
	"testing"
)

func TestParseGemfile_Full(t *testing.T) {
	input := `source 'https://rubygems.org'

ruby '3.2.0'

# Web framework
gem 'rails', '~> 7.1'
gem 'puma', '>= 5.0'
gem "sidekiq", "7.2.0"
gem 'rake'
`
	info := ParseGemfile([]byte(input))

	if info.Language != "ruby" {
		t.Errorf("Language = %q, want %q", info.Language, "ruby")
	}
	if info.RuntimeVersion != "3.2.0" {
		t.Errorf("RuntimeVersion = %q, want %q", info.RuntimeVersion, "3.2.0")
	}

	wantDeps := 4
	if len(info.Dependencies) != wantDeps {
		t.Fatalf("Dependencies count = %d, want %d", len(info.Dependencies), wantDeps)
	}

	versions := map[string]string{
		"rails":   "~> 7.1",
		"puma":    ">= 5.0",
		"sidekiq": "7.2.0",
		"rake":    "",
	}

	for _, dep := range info.Dependencies {
		want, ok := versions[dep.Name]
		if !ok {
			t.Errorf("unexpected dep: %q", dep.Name)
			continue
		}
		if dep.Version != want {
			t.Errorf("%s version = %q, want %q", dep.Name, dep.Version, want)
		}
	}
}

func TestParseGemfile_Empty(t *testing.T) {
	info := ParseGemfile([]byte(""))
	if len(info.Dependencies) != 0 {
		t.Errorf("Dependencies count = %d, want 0", len(info.Dependencies))
	}
}

func TestParseGemfile_CommentsOnly(t *testing.T) {
	input := `# just a comment
source 'https://rubygems.org'
`
	info := ParseGemfile([]byte(input))
	if len(info.Dependencies) != 0 {
		t.Errorf("Dependencies count = %d, want 0", len(info.Dependencies))
	}
}
