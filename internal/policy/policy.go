// Package policy loads .go-code.yaml from a repo root and evaluates simple
// team rules against a review.DeltaResult.
package policy

import "github.com/anatolykoptev/vaelor/internal/review"

// Policy is the parsed team policy.
type Policy struct {
	Version     int     `yaml:"version"`
	Severity    string  `yaml:"severity"` // warning | error
	PathFilters Filters `yaml:"path_filters"`
	Rules       RuleSet `yaml:"rules"`
}

// Filters controls which paths the policy considers.
type Filters struct {
	Include []string `yaml:"include"`
	Exclude []string `yaml:"exclude"`
}

// RuleSet is the set of supported rules. Keep this small and explicit —
// do not build a generic rule DSL.
type RuleSet struct {
	ForbiddenImports  []ForbiddenImport `yaml:"forbidden_imports"`
	RequiredWhen      []RequiredWhen    `yaml:"required_when_touching"`
	MaxFuncLines      int               `yaml:"max_func_lines"`
	NoMagicHTTPStatus bool              `yaml:"no_magic_http_status"`
}

// ForbiddenImport blocks a specific import path (substring match).
type ForbiddenImport struct {
	Pattern string `yaml:"pattern"`
	Reason  string `yaml:"reason"`
}

// RequiredWhen enforces that touching path X also touches path Y.
type RequiredWhen struct {
	Path          string `yaml:"path"`
	MustAlsoTouch string `yaml:"must_also_touch"`
	Reason        string `yaml:"reason"`
}

// Finding is one policy violation.
type Finding struct {
	Path     string
	Line     int
	Severity string
	Rule     string
	Message  string
}

// Apply runs rules against r. The readFile closure lets callers provide file
// bodies either from disk (local repo) or from a git blob (remote cloned repo).
func (p *Policy) Apply(r *review.DeltaResult, readFile func(path string) string) []Finding {
	if p == nil || r == nil {
		return nil
	}
	var out []Finding
	for _, f := range r.ChangedFiles {
		if !p.match(f.Path) {
			continue
		}
		body := readFile(f.Path)
		if body == "" {
			continue
		}
		out = append(out, p.checkImports(f.Path, body)...)
		out = append(out, p.checkMagicStatus(f.Path, body)...)
	}
	out = append(out, p.checkRequiredWhen(r)...)
	return out
}
