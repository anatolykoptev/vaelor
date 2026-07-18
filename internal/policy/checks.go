package policy

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/review"
)

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
		if strings.HasPrefix(ex, "**/") {
			suffix := ex[3:]
			if matched, _ := filepath.Match(suffix, base); matched {
				return false
			}
			if strings.HasSuffix(path, suffix[1:]) {
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
		if strings.HasPrefix(in, "**/") {
			suffix := in[3:]
			if matched, _ := filepath.Match(suffix, base); matched {
				return true
			}
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
				Message: "use http.Status* constants, not numeric literal",
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
				Message: fmt.Sprintf("touched %s without %s: %s", rw.Path, rw.MustAlsoTouch, rw.Reason),
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
