package policy

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Load reads .go-code.yaml from root. Returns (nil, nil) when the file is
// absent — policy is opt-in.
func Load(root string) (*Policy, error) {
	return loadFile(filepath.Join(root, ".go-code.yaml"))
}

// LoadWithDefaults reads a server-wide defaults file (may be empty path),
// then overlays the repo-local .go-code.yaml on top. Repo rules take
// precedence on scalar fields; list fields (forbidden_imports,
// required_when_touching, path_filters) are concatenated.
// Returns nil only when both files are missing.
func LoadWithDefaults(repoRoot, defaultsPath string) (*Policy, error) {
	var base *Policy
	if defaultsPath != "" {
		b, err := loadFile(defaultsPath)
		if err != nil {
			return nil, fmt.Errorf("load defaults %s: %w", defaultsPath, err)
		}
		base = b
	}
	local, err := Load(repoRoot)
	if err != nil {
		return nil, err
	}
	return Merge(base, local), nil
}

func loadFile(path string) (*Policy, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var p Policy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if p.Severity == "" {
		p.Severity = "warning"
	}
	return &p, nil
}

// Merge overlays local on top of base. Nil inputs are handled:
// Merge(nil, x) == x, Merge(x, nil) == x, Merge(nil, nil) == nil.
// Scalars: local wins when non-zero. Lists: concatenated (base first).
func Merge(base, local *Policy) *Policy {
	switch {
	case base == nil && local == nil:
		return nil
	case base == nil:
		return local
	case local == nil:
		return base
	}
	out := *base
	out.Rules.ForbiddenImports = append(append([]ForbiddenImport{}, base.Rules.ForbiddenImports...), local.Rules.ForbiddenImports...)
	out.Rules.RequiredWhen = append(append([]RequiredWhen{}, base.Rules.RequiredWhen...), local.Rules.RequiredWhen...)
	out.PathFilters.Include = append(append([]string{}, base.PathFilters.Include...), local.PathFilters.Include...)
	out.PathFilters.Exclude = append(append([]string{}, base.PathFilters.Exclude...), local.PathFilters.Exclude...)
	if local.Severity != "" {
		out.Severity = local.Severity
	}
	if local.Version != 0 {
		out.Version = local.Version
	}
	if local.Rules.MaxFuncLines != 0 {
		out.Rules.MaxFuncLines = local.Rules.MaxFuncLines
	}
	if local.Rules.NoMagicHTTPStatus {
		out.Rules.NoMagicHTTPStatus = true
	}
	return &out
}
