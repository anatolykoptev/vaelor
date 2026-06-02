package freshness

import (
	"context"
	"net/http"
)

// Default base URLs for package registries.
const (
	DefaultGoProxyURL  = "https://proxy.golang.org"
	DefaultNpmURL      = "https://registry.npmjs.org"
	DefaultPyPIURL     = "https://pypi.org"
	DefaultCratesURL   = "https://crates.io"
	DefaultMavenURL    = "https://search.maven.org"
	DefaultRubyGemsURL = "https://rubygems.org"
	DefaultNuGetURL    = "https://api.nuget.org"
)

// Registry looks up the latest version of a package.
type Registry interface {
	Latest(ctx context.Context, name string) (string, error)
}

// MultiRegistry maps language names to their registries.
type MultiRegistry struct {
	registries map[string]Registry
}

// NewMultiRegistry creates a MultiRegistry with default registries for all languages.
func NewMultiRegistry(client *http.Client) *MultiRegistry {
	return &MultiRegistry{
		registries: map[string]Registry{
			"go":         &GoRegistry{BaseURL: DefaultGoProxyURL, Client: client},
			"npm":        &NpmRegistry{BaseURL: DefaultNpmURL, Client: client},
			"typescript": &NpmRegistry{BaseURL: DefaultNpmURL, Client: client},
			"python":     &PyPIRegistry{BaseURL: DefaultPyPIURL, Client: client},
			"rust":       &CratesRegistry{BaseURL: DefaultCratesURL, Client: client},
			"java":       &MavenRegistry{BaseURL: DefaultMavenURL, Client: client},
			"ruby":       &RubyGemsRegistry{BaseURL: DefaultRubyGemsURL, Client: client},
			"csharp":     &NuGetRegistry{BaseURL: DefaultNuGetURL, Client: client},
		},
	}
}

// ForLanguage returns the registry for the given language, or nil if unsupported.
func (mr *MultiRegistry) ForLanguage(lang string) Registry {
	return mr.registries[lang]
}
