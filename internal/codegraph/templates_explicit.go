package codegraph

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Template parameter names referenced outside the template table.
const (
	paramLimit = "limit"
	paramName  = "name"
	paramFile  = "file"
	paramPath  = "path"
	paramPkg   = "pkg"
	paramFrom  = "from"
	paramTo    = "to"
)

// paramOptional reports whether an explicit call may omit param. Only
// Template.Optional entries and {limit} (which has a default) are optional —
// fail-closed, so a new template's params are required until marked. An
// omitted param silently changes the answer: an empty name matches nothing,
// an empty path matches every file.
func paramOptional(t *Template, param string) bool {
	return param == paramLimit || slices.Contains(t.Optional, param)
}

// maxTemplateLimit caps a {limit} parameter.
const maxTemplateLimit = 500

// ExplicitClassification validates a caller-chosen template and its params,
// letting code_graph run a template without asking the LLM to classify the
// query. It rejects freeform (which needs the LLM to write Cypher), unknown
// template IDs, params the template does not take (a typo would otherwise be
// dropped silently) and missing required params. Values are trimmed.
func ExplicitClassification(id string, params map[string]string) (*Classification, error) {
	if id == templateFreeform {
		return nil, errors.New(`template "freeform" needs the LLM to write Cypher; omit template to use it`)
	}
	t := GetTemplate(id)
	if t == nil {
		return nil, fmt.Errorf("unknown template %q; valid templates: %s", id, TemplateSignatures())
	}
	takes := make(map[string]bool, len(t.Params))
	for _, p := range t.Params {
		takes[p] = true
	}
	out := make(map[string]string, len(params))
	for k, v := range params {
		if !takes[k] {
			return nil, fmt.Errorf("template %s does not take param %q; it takes %s", id, k, signature(t))
		}
		out[k] = strings.TrimSpace(v)
	}
	for _, p := range t.Params {
		if !paramOptional(t, p) && out[p] == "" {
			return nil, fmt.Errorf("template %s requires param %q", id, p)
		}
	}
	return &Classification{Template: id, Params: out}, nil
}

// ExplicitQueryText renders an explicit classification as the question text
// shown in results and narratives, e.g. "who_calls name=ParseFile".
func ExplicitQueryText(c *Classification) string {
	keys := make([]string, 0, len(c.Params))
	for k := range c.Params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := []string{c.Template}
	for _, k := range keys {
		parts = append(parts, k+"="+c.Params[k])
	}
	return strings.Join(parts, " ")
}

// TemplateSignatures lists every template as id(params), sorted by ID —
// the compact form for tool descriptions and error hints.
func TemplateSignatures() string {
	ids := make([]string, 0, len(templates))
	for id := range templates {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	sigs := make([]string, len(ids))
	for i, id := range ids {
		sigs[i] = id + signature(templates[id])
	}
	return strings.Join(sigs, ", ")
}

func signature(t *Template) string {
	return "(" + strings.Join(t.Params, ", ") + ")"
}

// sanitizeLimit returns v when it is a positive integer (capped at
// maxTemplateLimit), else def. {limit} is rendered unquoted into
// `LIMIT {limit}`, so escaping alone does not keep a non-numeric value from
// changing the query.
func sanitizeLimit(v, def string) string {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return def
	}
	return strconv.Itoa(min(n, maxTemplateLimit))
}
