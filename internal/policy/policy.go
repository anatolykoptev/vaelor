// Package policy loads .go-code.yaml from a repo root and evaluates simple
// team rules against a review.DeltaResult.
package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/anatolykoptev/go-code/internal/review"
	"gopkg.in/yaml.v3"
)

// Policy is the parsed team policy.
type Policy struct {
	Version     int      `yaml:"version"`
	Severity    string   `yaml:"severity"` // warning | error
	PathFilters Filters  `yaml:"path_filters"`
	Rules       RuleSet  `yaml:"rules"`
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

// Load reads .go-code.yaml from root. Returns (nil, nil) when the file is
// absent — policy is opt-in.
func Load(root string) (*Policy, error) {
	data, err := os.ReadFile(filepath.Join(root, ".go-code.yaml"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read .go-code.yaml: %w", err)
	}
	var p Policy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse .go-code.yaml: %w", err)
	}
	if p.Severity == "" {
		p.Severity = "warning"
	}
	return &p, nil
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

func (p *Policy) match(path string) bool {
	base := filepath.Base(path)
	for _, ex := range p.PathFilters.Exclude {
		if matched, _ := filepath.Match(ex, path); matched {
			return false
		}
		if matched, _ := filepath.Match(ex, base); matched {
			return false
		}
		if strings.HasPrefix(path, strings.TrimSuffix(ex, "/**")+"/") {
			return false
		}
		// Handle **/ prefix for exclude patterns
		if strings.HasPrefix(ex, "**/") {
			suffix := ex[3:] // e.g., "*.go" from "**/*.go"
			if matched, _ := filepath.Match(suffix, base); matched {
				return false
			}
			if strings.HasSuffix(path, suffix[1:]) { // matches ".go"
				return false
			}
		}
	}
	if len(p.PathFilters.Include) == 0 {
		return true
	}
	for _, in := range p.PathFilters.Include {
		if matched, _ := filepath.Match(in, path); matched {
			return true
		}
		if matched, _ := filepath.Match(in, base); matched {
			return true
		}
		// Handle **/ prefix for include patterns (e.g., **/*.go matches any .go file)
		if strings.HasPrefix(in, "**/") {
			suffix := in[3:] // e.g., "*.go" from "**/*.go"
			if matched, _ := filepath.Match(suffix, base); matched {
				return true
			}
			// Also match extension directly
			if strings.HasPrefix(suffix, "*.") && strings.HasSuffix(path, suffix[1:]) {
				return true
			}
		}
	}
	return false
}

func (p *Policy) checkImports(path, body string) []Finding {
	var out []Finding
	for _, fi := range p.Rules.ForbiddenImports {
		if strings.Contains(body, `"`+fi.Pattern+`"`) {
			out = append(out, Finding{
				Path: path, Line: findImportLine(body, fi.Pattern),
				Severity: p.Severity, Rule: "forbidden_import",
				Message: fmt.Sprintf("forbidden import %q: %s", fi.Pattern, fi.Reason),
			})
		}
	}
	return out
}

var reStatusLit = regexp.MustCompile(`\b(200|201|204|301|302|400|401|403|404|409|422|500|502|503)\b`)

func (p *Policy) checkMagicStatus(path, body string) []Finding {
	if !p.Rules.NoMagicHTTPStatus {
		return nil
	}
	var out []Finding
	for i, line := range strings.Split(body, "\n") {
		if strings.Contains(line, "http.Status") {
			continue
		}
		if reStatusLit.MatchString(line) && (strings.Contains(line, "WriteHeader") || strings.Contains(line, "StatusCode")) {
			out = append(out, Finding{
				Path: path, Line: i + 1,
				Severity: p.Severity, Rule: "no_magic_http_status",
				Message:  "use http.Status* constants, not numeric literal",
			})
		}
	}
	return out
}

func (p *Policy) checkRequiredWhen(r *review.DeltaResult) []Finding {
	var out []Finding
	for _, rw := range p.Rules.RequiredWhen {
		var touched, paired bool
		for _, f := range r.ChangedFiles {
			if matched, _ := filepath.Match(rw.Path, f.Path); matched {
				touched = true
			}
			if strings.Contains(f.Path, strings.TrimPrefix(rw.Path, "internal/api/")) {
				// cheap heuristic; users expected to glob "internal/api/**"
				touched = touched || strings.HasPrefix(f.Path, strings.TrimSuffix(rw.Path, "/**"))
			}
			if matched, _ := filepath.Match(rw.MustAlsoTouch, filepath.Base(f.Path)); matched {
				paired = true
			}
		}
		if touched && !paired {
			out = append(out, Finding{
				Path: "", Line: 0,
				Severity: p.Severity, Rule: "required_when_touching",
				Message:  fmt.Sprintf("touched %s without %s: %s", rw.Path, rw.MustAlsoTouch, rw.Reason),
			})
		}
	}
	return out
}

func findImportLine(body, pattern string) int {
	for i, line := range strings.Split(body, "\n") {
		if strings.Contains(line, pattern) {
			return i + 1
		}
	}
	return 1
}
